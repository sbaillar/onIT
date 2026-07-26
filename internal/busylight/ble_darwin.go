package busylight

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"tinygo.org/x/bluetooth"
)

// The adapter is enabled lazily so onIT never touches Bluetooth (or prompts
// for permission) unless a device is bonded or pairing starts.
var (
	bleOnce sync.Once
	bleErr  error
)

func bleAdapter() (*bluetooth.Adapter, error) {
	bleOnce.Do(func() { bleErr = bluetooth.DefaultAdapter.Enable() })
	return bluetooth.DefaultAdapter, bleErr
}

func bleUUID(s string) bluetooth.UUID {
	u, err := bluetooth.ParseUUID(s)
	if err != nil {
		panic(err) // constants in ble.go; cannot fail
	}
	return u
}

// bleTransport drives the bonded AMOLED board over Bluetooth LE.
// Reconnects lazily on send, like serialTransport.
type bleTransport struct {
	mu        sync.Mutex
	deviceID  string // OS peripheral identifier from pairing
	dev       *bluetooth.Device
	cmd       bluetooth.DeviceCharacteristic
	emoji     bluetooth.DeviceCharacteristic
	nextScan  time.Time
	conn      atomic.Bool
	lost      atomic.Bool       // bond refused; stop retrying until re-pair
	onEvent   func(line string) // device lines (VERSION:, TOUCH:, ROULETTE:) from Events
	onConnect func()            // post-connect pushes (timezone, deck sync); may be nil
	deckAck   chan byte         // DECKOK:<slot> ack after the device persists a deck image
}

var _ bleLink = (*bleTransport)(nil)

func newBLELink(deviceID string, onEvent func(line string), onConnect func()) bleLink {
	return &bleTransport{deviceID: deviceID, onEvent: onEvent, onConnect: onConnect,
		deckAck: make(chan byte, 1)}
}

// bleConnect connects to a peripheral by its stored identifier.
func bleConnect(deviceID string) (bluetooth.Device, error) {
	adapter, err := bleAdapter()
	if err != nil {
		return bluetooth.Device{}, err
	}
	uu, err := bluetooth.ParseUUID(deviceID)
	if err != nil {
		return bluetooth.Device{}, err
	}
	return adapter.Connect(bluetooth.Address{UUID: uu}, bluetooth.ConnectionParams{
		ConnectionTimeout: bluetooth.NewDuration(5 * time.Second),
	})
}

// bleChars discovers the onIT service and returns its three characteristics.
func bleChars(dev bluetooth.Device) (cmd, emoji, events bluetooth.DeviceCharacteristic, err error) {
	svcs, err := dev.DiscoverServices([]bluetooth.UUID{bleUUID(bleServiceUUID)})
	if err != nil {
		return
	}
	if len(svcs) == 0 {
		err = errors.New("onIT service not found")
		return
	}
	chars, err := svcs[0].DiscoverCharacteristics([]bluetooth.UUID{
		bleUUID(bleCommandUUID), bleUUID(bleEmojiUUID), bleUUID(bleEventsUUID),
	})
	if err != nil {
		return
	}
	found := 0
	for _, c := range chars {
		switch c.UUID() {
		case bleUUID(bleCommandUUID):
			cmd, found = c, found+1
		case bleUUID(bleEmojiUUID):
			emoji, found = c, found+1
		case bleUUID(bleEventsUUID):
			events, found = c, found+1
		}
	}
	if found != 3 {
		err = fmt.Errorf("onIT characteristics incomplete (%d of 3)", found)
	}
	return
}

// ensureLocked connects to the bonded device. Caller holds mu.
func (t *bleTransport) ensureLocked() bool {
	if t.dev != nil {
		return true
	}
	if t.lost.Load() || time.Now().Before(t.nextScan) {
		return false
	}
	t.nextScan = time.Now().Add(scanBackoff)
	dev, err := bleConnect(t.deviceID)
	if err != nil {
		return false
	}
	cmd, emoji, events, err := bleChars(dev)
	if err != nil {
		log.Printf("BLE discovery failed: %v", err)
		dev.Disconnect()
		return false
	}
	// The encrypted read verifies the bond. If the board was reflashed or the
	// OS bond deleted it fails: surface "pairing lost" and stop retrying —
	// no silent loop that would pop OS pairing dialogs.
	if _, err := events.Read(make([]byte, 64)); err != nil {
		log.Printf("BLE bond check failed: %v — re-pair from the tray", err)
		t.lost.Store(true)
		dev.Disconnect()
		return false
	}
	if err := events.EnableNotifications(func(buf []byte) {
		line := strings.TrimSpace(string(buf))
		// DECKOK acks are transport-internal flow control (see sendDeckImage)
		if s, ok := strings.CutPrefix(line, "DECKOK:"); ok {
			if slot, err := strconv.Atoi(s); err == nil {
				select {
				case t.deckAck <- byte(slot):
				default:
				}
			}
			return
		}
		t.onEvent(line)
	}); err != nil {
		log.Printf("BLE notifications failed: %v", err)
		dev.Disconnect()
		return false
	}
	t.dev = &dev
	t.cmd = cmd
	t.emoji = emoji
	t.conn.Store(true)
	log.Printf("BLE connected: %s", t.deviceID)
	go t.watch(t.dev)
	// Ask for the version banner; the reply arrives as an Events notification.
	go t.sendLine("VERSION")
	if t.onConnect != nil {
		go t.onConnect()
	}
	return true
}

// watch polls the connection state so an idle disconnect (out of range,
// radio off) drops the link and lets sends fall back to USB.
func (t *bleTransport) watch(dev *bluetooth.Device) {
	for {
		time.Sleep(2 * time.Second)
		t.mu.Lock()
		if t.dev != dev {
			t.mu.Unlock()
			return
		}
		up, _ := dev.Connected()
		if !up {
			t.dropLocked()
			t.mu.Unlock()
			return
		}
		t.mu.Unlock()
	}
}

// dropLocked releases the connection. Caller holds mu.
func (t *bleTransport) dropLocked() {
	if t.dev != nil {
		t.dev.Disconnect()
		t.dev = nil
		t.conn.Store(false)
		log.Print("BLE disconnected")
	}
}

// sendLine writes a protocol line to the Command characteristic, connecting
// first if needed. EMOJI: lines (the serial wire format the agent still
// speaks) are decoded and routed to the binary chunked path.
func (t *bleTransport) sendLine(line string) bool {
	if b64, ok := strings.CutPrefix(line, "EMOJI:"); ok {
		rgb565, err := base64.StdEncoding.DecodeString(b64)
		if err != nil {
			return false
		}
		return t.sendEmoji(rgb565)
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.ensureLocked() {
		return false
	}
	if _, err := t.cmd.Write([]byte(line)); err != nil {
		t.dropLocked()
		return false
	}
	return true
}

// sendEmoji streams an RGB565 image to the live display (slot 0xFF).
func (t *bleTransport) sendEmoji(rgb565 []byte) bool {
	return t.writeEmoji(rgb565, bleSlotLive, false)
}

// sendDeckImage stores an RGB565 image into a roulette deck slot; lastOfSync
// flags the final chunk of the whole deck sync so the device may persist its
// deck index. It waits for the device's DECKOK:<slot> ack — the ATT response
// only means the last chunk was received; the image is consumed and written
// to LittleFS later in the device's loop, and streaming the next slot before
// that would overwrite the rx buffer with a torn mix of two images.
func (t *bleTransport) sendDeckImage(slot int, rgb565 []byte, lastOfSync bool) bool {
	select { // drop a stale ack from an aborted earlier sync
	case <-t.deckAck:
	default:
	}
	if !t.writeEmoji(rgb565, byte(slot), lastOfSync) {
		return false
	}
	select {
	case got := <-t.deckAck:
		return got == byte(slot) // a mismatched slot ack is a failure
	case <-time.After(5 * time.Second):
		return false
	}
}

// writeEmoji streams an RGB565 image over the Emoji characteristic in
// header-prefixed chunks. The last chunk is written with response so a
// mid-transfer disconnect surfaces as failure.
func (t *bleTransport) writeEmoji(rgb565 []byte, slot byte, lastOfSync bool) bool {
	if len(rgb565) > 0xFFFF {
		log.Printf("BLE emoji too large: %d bytes", len(rgb565))
		return false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.ensureLocked() {
		return false
	}
	chunks := emojiChunks(rgb565, slot, lastOfSync)
	for i, c := range chunks {
		var err error
		if i == len(chunks)-1 {
			_, err = t.emoji.Write(c)
		} else {
			_, err = t.emoji.WriteWithoutResponse(c)
		}
		if err != nil {
			t.dropLocked()
			return false
		}
	}
	return true
}

// connected reports whether the BLE link is currently up (lock-free).
func (t *bleTransport) connected() bool {
	return t.conn.Load()
}

// pairingLost reports that the bond was refused (see ensureLocked).
func (t *bleTransport) pairingLost() bool {
	return t.lost.Load()
}

// close releases the connection.
func (t *bleTransport) close() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.dropLocked()
}

// blePair scans for busylights advertising the onIT service, lets choose pick
// one, then connects and reads the encrypted Events characteristic so macOS
// runs its pairing dialog against the passkey shown on the device.
func blePair(ctx context.Context, choose func(BLEDevice) bool) (BLEDevice, error) {
	adapter, err := bleAdapter()
	if err != nil {
		return BLEDevice{}, err
	}
	var picked BLEDevice
	var found bool
	seen := map[string]bool{}
	scanDone := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			adapter.StopScan()
		case <-scanDone:
		}
	}()
	err = adapter.Scan(func(a *bluetooth.Adapter, r bluetooth.ScanResult) {
		if !r.HasServiceUUID(bleUUID(bleServiceUUID)) {
			return
		}
		id := r.Address.String()
		if seen[id] {
			return
		}
		seen[id] = true
		d := BLEDevice{ID: id, Name: r.LocalName()}
		if choose(d) {
			picked, found = d, true
			a.StopScan()
		}
	})
	close(scanDone)
	if err != nil {
		return BLEDevice{}, err
	}
	if !found {
		if err := ctx.Err(); err != nil {
			return BLEDevice{}, err
		}
		return BLEDevice{}, errors.New("pairing cancelled")
	}

	dev, err := bleConnect(picked.ID)
	if err != nil {
		return BLEDevice{}, err
	}
	defer dev.Disconnect()
	_, _, events, err := bleChars(dev)
	if err != nil {
		return BLEDevice{}, err
	}
	// The read blocks while the user types the passkey; each attempt times
	// out after 10s inside the library, so retry until pairing completes,
	// fails for good, or ctx expires. Only the library's read timeout means
	// "still typing" — any other error (e.g. the user cancelled the macOS
	// passkey dialog) is final, so the retry can never re-pop the OS dialog
	// in a loop.
	deadline := time.Now().Add(90 * time.Second)
	for {
		if _, err = events.Read(make([]byte, 64)); err == nil {
			return picked, nil
		}
		if ctx.Err() != nil {
			return BLEDevice{}, ctx.Err()
		}
		if err.Error() != "timeout on Read()" {
			return BLEDevice{}, fmt.Errorf("pairing failed: %w", err)
		}
		if time.Now().After(deadline) {
			return BLEDevice{}, fmt.Errorf("pairing failed: %w", err)
		}
		up, _ := dev.Connected()
		if !up {
			return BLEDevice{}, fmt.Errorf("pairing failed: %w", err)
		}
	}
}
