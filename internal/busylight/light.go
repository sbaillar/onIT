package busylight

import (
	"bufio"
	"log"
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
)

// USB VID:PID pairs to match: Espressif native USB, WCH CH343 bridge.
var usbIDs = map[[2]string]bool{
	{"303A", "1001"}: true,
	{"1A86", "55D3"}: true,
}

// Light owns the serial port. Reconnects lazily on every send.
type Light struct {
	mu        sync.Mutex
	port      serial.Port
	portName  string // last successfully opened port; survives Close
	nextScan  time.Time
	connected atomic.Bool
	version   atomic.Value // string: firmware version from VERSION: banner
	onTouch   atomic.Value // func(string): TOUCH: event callback
}

// SetOnTouch registers a callback for TOUCH: events from the device.
func (l *Light) SetOnTouch(f func(kind string)) { l.onTouch.Store(f) }

func NewLight() *Light {
	return &Light{}
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

// ensureLocked opens the port and starts a reader goroutine. Caller holds mu.
func (l *Light) ensureLocked() bool {
	if l.port != nil {
		return true
	}
	if time.Now().Before(l.nextScan) {
		return false
	}
	name := findPort()
	if name == "" {
		l.nextScan = time.Now().Add(scanBackoff)
		return false
	}
	port, err := serial.Open(name, &serial.Mode{BaudRate: baud})
	if err != nil {
		log.Printf("open %s failed: %v", name, err)
		l.nextScan = time.Now().Add(scanBackoff)
		return false
	}
	time.Sleep(500 * time.Millisecond) // board may reset on open
	l.port = port
	l.portName = name
	l.connected.Store(true)
	log.Printf("Serial connected: %s", name)
	go l.reader(port)
	// The boot banner is easy to miss and the first query can be eaten by
	// the open-triggered reset, so keep asking until the device answers.
	go func() {
		for range 5 {
			l.mu.Lock()
			open := l.port == port
			if open {
				port.Write([]byte("VERSION\n"))
			}
			l.mu.Unlock()
			if !open {
				return
			}
			time.Sleep(2 * time.Second)
			if v, _ := l.version.Load().(string); v != "" {
				return
			}
		}
	}()
	return true
}

// reader watches device output (VERSION banners, TOUCH events) until the
// port dies.
func (l *Light) reader(port serial.Port) {
	sc := bufio.NewScanner(port)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if v, ok := strings.CutPrefix(line, "VERSION:"); ok {
			l.version.Store(v)
		}
		if kind, ok := strings.CutPrefix(line, "TOUCH:"); ok {
			if f, _ := l.onTouch.Load().(func(string)); f != nil {
				go f(kind)
			}
		}
	}
	l.drop(port)
}

// drop closes port if it is still the active one.
func (l *Light) drop(port serial.Port) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.port == port {
		l.port.Close()
		l.port = nil
		l.connected.Store(false)
	}
}

// Send writes a state to the device, connecting first if needed.
// Only the agent's push goroutine calls this; the UI never blocks on it.
func (l *Light) Send(state string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.ensureLocked() {
		return
	}
	if _, err := l.port.Write([]byte("STATE:" + state + "\n")); err != nil {
		l.port.Close()
		l.port = nil
		l.connected.Store(false)
	}
}

// SendLine writes an arbitrary protocol line (e.g. an EMOJI payload).
// Blocks until transmitted; large payloads take a couple of seconds.
func (l *Light) SendLine(line string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.ensureLocked() {
		return false
	}
	if _, err := l.port.Write([]byte(line + "\n")); err != nil {
		l.port.Close()
		l.port = nil
		l.connected.Store(false)
		return false
	}
	return true
}

// Connected reports whether a serial port is currently open (lock-free,
// safe to call from UI threads while a reconnect is in progress).
func (l *Light) Connected() bool {
	return l.connected.Load()
}

// Version returns the firmware version the device last reported ("" if the
// firmware predates the VERSION banner).
func (l *Light) Version() string {
	v, _ := l.version.Load().(string)
	return v
}

// ClearVersion forgets the cached firmware version (called before a flash
// so a lost banner can never leave a stale pre-flash version on display).
func (l *Light) ClearVersion() {
	l.version.Store("")
}

// PortName returns the last successfully opened port path, even after Close.
func (l *Light) PortName() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.portName
}

// Close releases the serial port (e.g. so esptool can use it).
func (l *Light) Close() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.port != nil {
		l.port.Close()
		l.port = nil
		l.connected.Store(false)
	}
}
