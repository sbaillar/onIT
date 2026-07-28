package busylight

import (
	"bufio"
	"encoding/base64"
	"log"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"go.bug.st/serial"
	"go.bug.st/serial/enumerator"
)

const (
	baud        = 115200
	scanBackoff = 10 * time.Second // don't re-enumerate USB on every heartbeat

	// pacing for long lines — see writePaced. 1KB per 8ms is ~128KB/s, far
	// quicker than any payload needs and well inside the firmware's buffer
	// even when a repaint stalls its loop for tens of milliseconds.
	serialBurst    = 1024
	serialBurstGap = 8 * time.Millisecond
)

// USB VID:PID pairs to match: Espressif native USB, WCH CH343 bridge.
var usbIDs = map[[2]string]bool{
	{"303A", "1001"}: true,
	{"1A86", "55D3"}: true,
}

// transport is the link to the device (serial today, BLE later).
type transport interface {
	sendLine(line string) bool    // STATE:*, VERSION
	sendEmoji(rgb565 []byte) bool // binary path (BLE); serial keeps base64 line
	connected() bool
	close()
}

// Light drives the device through a transport. Reconnects lazily on send.
// The VERSION banner (version + board) is tracked per transport — each side
// may be a different physical device (e.g. a BLE-bonded AMOLED plus another
// board on USB), so one must never answer for the other.
type Light struct {
	usb         transport
	serial      *serialTransport // concrete handle for serial-only PortName
	bleVersion  atomic.Value     // string: version from the BLE device's banner
	bleBoard    atomic.Value     // string: board from the BLE device's banner
	onTouch     atomic.Value     // func(string): TOUCH: event callback
	onRoulette  atomic.Value     // func(int): ROULETTE: winner-slot callback
	deckSource  atomic.Value     // func() (sig string, render func() [][]byte)
	deckSyncing atomic.Bool      // a deck upload is in flight (see SyncDeck)

	// replies to serial deck traffic, routed out of the reader goroutine.
	// Buffered by one and written non-blocking: a reply nobody is waiting
	// for is dropped rather than stalling the reader.
	serialDeckAck chan byte
	serialDeckSig chan string

	mu  sync.Mutex
	ble bleLink    // nil until a device is bonded (see ble.go)
	dev *BLEDevice // in-memory bonded record (Deck/Sig cache); nil when none
}

// SetOnTouch registers a callback for TOUCH: events from the device.
func (l *Light) SetOnTouch(f func(kind string)) { l.onTouch.Store(f) }

// SetOnRoulette registers a callback for ROULETTE:<slot> events — the deck
// slot the emoji roulette settled on (see Spin).
func (l *Light) SetOnRoulette(f func(slot int)) { l.onRoulette.Store(f) }

// DeckSource reports a cheap signature of the current roulette deck plus a
// closure that renders it. Named so that changing the shape breaks callers at
// compile time — as an inline type it was asserted in three places, and a
// mismatch would have silently yielded nil and stopped deck sync on both
// transports with no error.
type DeckSource func() (sig string, render func() [][]byte)

// SetDeckSource registers the roulette-deck source: it returns a cheap
// signature of the current deck plus a render closure. On every BLE connect
// the signature gates whether rendering and a sync are needed (see SyncDeck
// and handleBLEConnect).
func (l *Light) SetDeckSource(f DeckSource) {
	l.deckSource.Store(f)
}

// deckSrc returns the registered deck source, nil if none.
func (l *Light) deckSrc() DeckSource {
	f, _ := l.deckSource.Load().(DeckSource)
	return f
}

func NewLight() *Light {
	l := &Light{
		serialDeckAck: make(chan byte, 1),
		serialDeckSig: make(chan string, 1),
	}
	l.serial = newSerialTransport(l.handleSerialEvent)
	l.usb = l.serial
	if dev := loadBLEDevice(); dev != nil {
		l.dev = dev
		l.ble = newBLELink(dev.ID, l.handleBLEEvent, l.handleBLEConnect)
	}
	return l
}

// bleTr returns the current BLE link (nil when no device is bonded).
func (l *Light) bleTr() bleLink {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.ble
}

// transports returns the links to try in order per the selection policy.
func (l *Light) transports() []transport {
	return orderTransports(l.bleTr(), l.usb)
}

// parseVersion splits a VERSION banner body into version and board type:
// "x.y.z:amoled175" is the 1.75" AMOLED board, "x.y.z:lcd128" the 1.28" LCD,
// and a missing third field is legacy 1.28" firmware.
func parseVersion(v string) (version, board string) {
	if ver, b, ok := strings.Cut(v, ":"); ok {
		return ver, b
	}
	return v, "lcd128"
}

// handleBLEEvent parses a device line from the BLE transport: the VERSION
// banner is stored as the BLE device's own state (the serial transport keeps
// its own — see serialTransport.handleLine), TOUCH and ROULETTE events are
// dispatched.
func (l *Light) handleBLEEvent(line string) {
	if v, ok := strings.CutPrefix(line, "VERSION:"); ok {
		ver, board := parseVersion(v)
		l.bleVersion.Store(ver)
		l.bleBoard.Store(board)
	}
	l.handleDeviceEvent(line)
}

// handleDeviceEvent dispatches a line the device sent — over either link,
// since the serial transport and handleBLEEvent both feed it here.
func (l *Light) handleDeviceEvent(line string) {
	if kind, ok := strings.CutPrefix(line, "TOUCH:"); ok {
		if f, _ := l.onTouch.Load().(func(string)); f != nil {
			go f(kind)
		}
		return
	}
	// A roulette winner arrives over whichever link the device is on; this
	// used to be dispatched from the BLE path alone, so a spin over USB
	// never reached the app.
	if s, ok := strings.CutPrefix(line, "ROULETTE:"); ok {
		if slot, err := strconv.Atoi(s); err == nil {
			if f, _ := l.onRoulette.Load().(func(int)); f != nil {
				go f(slot)
			}
		}
		return
	}
	// deck-upload replies belong to whoever is running a serial sync
	if s, ok := strings.CutPrefix(line, "DECKOK:"); ok {
		if slot, err := strconv.Atoi(s); err == nil {
			select {
			case l.serialDeckAck <- byte(slot):
			default:
			}
		}
		return
	}
	if s, ok := strings.CutPrefix(line, "DECKSIG:"); ok {
		select {
		case l.serialDeckSig <- s:
		default:
		}
		return
	}
}

// handleSerialEvent is the serial transport's event hook: shared dispatch,
// plus the connect-time work BLE does in handleBLEConnect. A VERSION banner
// is serial's "just connected" signal — the board either booted or was reset
// by the port opening, so its clock is unset. Off this goroutine: it is the
// reader, and both of these write.
func (l *Light) handleSerialEvent(line string) {
	l.handleDeviceEvent(line)
	if strings.HasPrefix(line, "VERSION:") {
		go func() {
			if err := l.PushClock(); err != nil {
				log.Printf("clock push failed: %v", err)
			}
			l.syncDeckSerial()
		}()
	}
}

func findPort() string {
	ports, err := enumerator.GetDetailedPortsList()
	if err != nil {
		log.Printf("port enumeration failed: %v", err)
		return ""
	}
	for _, p := range ports {
		if p.IsUSB && usbIDs[[2]string{strings.ToUpper(p.VID), strings.ToUpper(p.PID)}] {
			return p.Name
		}
	}
	return ""
}

// ListPorts logs all serial ports (the -ports flag).
func ListPorts() {
	ports, err := enumerator.GetDetailedPortsList()
	if err != nil {
		log.Fatal(err)
	}
	for _, p := range ports {
		if p.IsUSB {
			log.Printf("%s  VID:%s PID:%s  %s", p.Name, p.VID, p.PID, p.Product)
		} else {
			log.Print(p.Name)
		}
	}
}

// Send writes a state to the device, connecting first if needed.
// Only the agent's push goroutine calls this; the UI never blocks on it.
func (l *Light) Send(state string) {
	l.sendLine("STATE:" + state)
}

// SendLine writes an arbitrary protocol line (e.g. an EMOJI payload).
// Blocks until transmitted; large payloads take a couple of seconds.
func (l *Light) SendLine(line string) bool {
	return l.sendLine(line)
}

// sendLine tries each transport in policy order until one accepts the line.
func (l *Light) sendLine(line string) bool {
	for _, t := range l.transports() {
		if t.sendLine(line) {
			return true
		}
	}
	return false
}

// Connected reports whether any transport is currently up (lock-free,
// safe to call from UI threads while a reconnect is in progress).
func (l *Light) Connected() bool {
	for _, t := range l.transports() {
		if t.connected() {
			return true
		}
	}
	return false
}

// Version returns the firmware version of the device the light is driving:
// the BLE device's banner while BLE is connected, the serial device's
// otherwise ("" if none reported yet, e.g. firmware predating the banner).
func (l *Light) Version() string {
	if ble := l.bleTr(); ble != nil && ble.connected() {
		v, _ := l.bleVersion.Load().(string)
		return v
	}
	return l.serial.bannerVersion()
}

// Board returns the board type of the same device Version reports
// ("lcd128", "amoled175", or "" before any VERSION banner).
func (l *Light) Board() string {
	if ble := l.bleTr(); ble != nil && ble.connected() {
		b, _ := l.bleBoard.Load().(string)
		return b
	}
	return l.serial.bannerBoard()
}

// ClearVersion forgets the serial device's cached banner (called before a
// flash — which only ever writes over USB — so a lost banner can never leave
// a stale pre-flash version on display).
func (l *Light) ClearVersion() {
	l.serial.clearBanner()
}

// PortName returns the last successfully opened serial port path, even after
// Close.
func (l *Light) PortName() string {
	return l.serial.lastPortName()
}

// Close releases the transports (e.g. so esptool can use the serial port).
func (l *Light) Close() {
	if ble := l.bleTr(); ble != nil {
		ble.close()
	}
	l.usb.close()
}

// serialTransport owns the serial port. Reconnects lazily on every send.
// It keeps its own VERSION banner state so the serial device's identity is
// never confused with a BLE-connected one (FlashFirmware senses from here).
type serialTransport struct {
	mu       sync.Mutex
	port     serial.Port
	portName string // last successfully opened port; survives close
	nextScan time.Time
	conn     atomic.Bool
	version  atomic.Value      // string: version from this port's banner
	board    atomic.Value      // string: board from this port's banner
	onEvent  func(line string) // device output lines (TOUCH:)
}

var _ transport = (*serialTransport)(nil)

func newSerialTransport(onEvent func(line string)) *serialTransport {
	return &serialTransport{onEvent: onEvent}
}

// ensureLocked opens the port and starts a reader goroutine. Caller holds mu.
func (t *serialTransport) ensureLocked() bool {
	if t.port != nil {
		return true
	}
	if time.Now().Before(t.nextScan) {
		return false
	}
	name := findPort()
	if name == "" {
		t.nextScan = time.Now().Add(scanBackoff)
		return false
	}
	port, err := serial.Open(name, &serial.Mode{BaudRate: baud})
	if err != nil {
		log.Printf("open %s failed: %v", name, err)
		t.nextScan = time.Now().Add(scanBackoff)
		return false
	}
	time.Sleep(500 * time.Millisecond) // board may reset on open
	t.port = port
	t.portName = name
	t.conn.Store(true)
	log.Printf("Serial connected: %s", name)
	go t.reader(port)
	// The boot banner is easy to miss and the first query can be eaten by
	// the open-triggered reset, so keep asking until the device answers.
	go func() {
		for range 5 {
			t.mu.Lock()
			open := t.port == port
			if open {
				port.Write([]byte("VERSION\n"))
			}
			t.mu.Unlock()
			if !open {
				return
			}
			time.Sleep(2 * time.Second)
			if t.bannerVersion() != "" {
				return
			}
		}
	}()
	return true
}

// reader watches device output (VERSION banners, TOUCH events) until the
// port dies.
func (t *serialTransport) reader(port serial.Port) {
	sc := bufio.NewScanner(port)
	for sc.Scan() {
		t.handleLine(strings.TrimSpace(sc.Text()))
	}
	t.drop(port)
}

// handleLine parses a device output line: VERSION banners update this port's
// own banner state, everything else (TOUCH events) goes to onEvent.
func (t *serialTransport) handleLine(line string) {
	if v, ok := strings.CutPrefix(line, "VERSION:"); ok {
		ver, board := parseVersion(v)
		t.version.Store(ver)
		t.board.Store(board)
	}
	t.onEvent(line)
}

// bannerVersion returns the firmware version this port's device reported
// ("" before any banner).
func (t *serialTransport) bannerVersion() string {
	v, _ := t.version.Load().(string)
	return v
}

// bannerBoard returns the board type this port's device reported
// ("" before any banner).
func (t *serialTransport) bannerBoard() string {
	b, _ := t.board.Load().(string)
	return b
}

// clearBanner forgets the cached banner (pre-flash).
func (t *serialTransport) clearBanner() {
	t.version.Store("")
	t.board.Store("")
}

// drop closes port if it is still the active one.
func (t *serialTransport) drop(port serial.Port) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.port == port {
		t.port.Close()
		t.port = nil
		t.conn.Store(false)
	}
}

// sendLine writes a protocol line, connecting first if needed.
func (t *serialTransport) sendLine(line string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.ensureLocked() {
		return false
	}
	if err := t.writePaced([]byte(line + "\n")); err != nil {
		t.port.Close()
		t.port = nil
		t.conn.Store(false)
		return false
	}
	return true
}

// writePaced writes in bursts with a pause between them. "115200" is a
// fiction on the AMOLED board: it has no UART bridge, so a write crosses USB
// at USB speed — 38KB of EMOJI: or DECKIMG: lands in ~60ms, far faster than
// the firmware's loop drains it, and the overflow silently truncates the
// line. (The 1.28" board's CH343 really does run at 115200, which is slow
// enough to keep up on its own — which is why this only ever broke on the
// AMOLED.) Caller holds mu, so a line is still written atomically.
func (t *serialTransport) writePaced(b []byte) error {
	for len(b) > serialBurst {
		if _, err := t.port.Write(b[:serialBurst]); err != nil {
			return err
		}
		b = b[serialBurst:]
		time.Sleep(serialBurstGap)
	}
	_, err := t.port.Write(b)
	return err
}

// sendEmoji sends raw RGB565 pixels as the base64 EMOJI: line the serial
// protocol expects.
func (t *serialTransport) sendEmoji(rgb565 []byte) bool {
	return t.sendLine("EMOJI:" + base64.StdEncoding.EncodeToString(rgb565))
}

// connected reports whether a serial port is currently open (lock-free).
func (t *serialTransport) connected() bool {
	return t.conn.Load()
}

// lastPortName returns the last successfully opened port path, even after
// close.
func (t *serialTransport) lastPortName() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.portName
}

// close releases the serial port (e.g. so esptool can use it).
func (t *serialTransport) close() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.port != nil {
		t.port.Close()
		t.port = nil
		t.conn.Store(false)
	}
}
