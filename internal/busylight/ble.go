package busylight

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"os"
	"path/filepath"
)

// onIT BLE GATT layout — keep in sync with firmware/busylight_round_amoled
// ("6f6e4954" = "onIT", "0175" = 1.75").
//
//	Service  6f6e4954-0175-4b1e-8001-000000000001
//	  Command  6f6e4954-0175-4b1e-8001-000000000002  write   protocol lines: STATE:*, VERSION
//	  Emoji    6f6e4954-0175-4b1e-8001-000000000003  write   8-byte header (offset u32 LE,
//	                                                         total u16 LE, seq u16 LE) +
//	                                                         raw RGB565 chunk (≤504 B payload)
//	  Events   6f6e4954-0175-4b1e-8001-000000000004  notify  TOUCH:TAP, TOUCH:LONG,
//	                                                         VERSION:x.y.z:amoled175
//	                                                 read    the VERSION line; the encrypted
//	                                                         read triggers/verifies pairing
//
// All characteristics require an encrypted bonded link (LE Secure Connections).
const (
	bleServiceUUID = "6f6e4954-0175-4b1e-8001-000000000001"
	bleCommandUUID = "6f6e4954-0175-4b1e-8001-000000000002"
	bleEmojiUUID   = "6f6e4954-0175-4b1e-8001-000000000003"
	bleEventsUUID  = "6f6e4954-0175-4b1e-8001-000000000004"
)

// Emoji chunking: each Emoji write is an 8-byte header plus up to 504 bytes
// of raw RGB565 pixels, so a chunk fits a 512-byte ATT write.
const (
	bleChunkHeader  = 8
	bleChunkPayload = 504
)

// emojiChunks splits an RGB565 image into Emoji characteristic writes.
// Header fields are little-endian: offset u32 (byte offset of this chunk),
// total u16 (image size in bytes), seq u16 (chunk index from 0).
func emojiChunks(rgb565 []byte) [][]byte {
	var chunks [][]byte
	for seq, off := 0, 0; off < len(rgb565); seq++ {
		n := min(len(rgb565)-off, bleChunkPayload)
		c := make([]byte, bleChunkHeader+n)
		binary.LittleEndian.PutUint32(c[0:], uint32(off))
		binary.LittleEndian.PutUint16(c[4:], uint16(len(rgb565)))
		binary.LittleEndian.PutUint16(c[6:], uint16(seq))
		copy(c[bleChunkHeader:], rgb565[off:off+n])
		chunks = append(chunks, c)
		off += n
	}
	return chunks
}

// BLEDevice identifies a pairable/bonded busylight (ID is the OS peripheral
// identifier — a UUID on macOS).
type BLEDevice struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func bleConfigFile() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".onit_ble.json")
}

// loadBLEDevice restores the bonded device from disk (zero value if none).
func loadBLEDevice() BLEDevice {
	var d BLEDevice
	if b, err := os.ReadFile(bleConfigFile()); err == nil {
		json.Unmarshal(b, &d)
	}
	return d
}

func saveBLEDevice(d BLEDevice) {
	b, _ := json.Marshal(d)
	os.WriteFile(bleConfigFile(), b, 0o600)
}

func clearBLEDevice() {
	os.Remove(bleConfigFile())
}

// bleLink is a BLE transport that also exposes bond state. Platform files
// provide newBLELink (nil where BLE is unsupported) and blePair.
type bleLink interface {
	transport
	pairingLost() bool
}

// orderTransports is the selection policy: with no bonded device (or the bond
// lost) USB alone; BLE alone while it is connected (never double-drive USB);
// otherwise BLE first with USB fallback while BLE is down.
func orderTransports(ble bleLink, usb transport) []transport {
	switch {
	case ble == nil || ble.pairingLost():
		return []transport{usb}
	case ble.connected():
		return []transport{ble}
	default:
		return []transport{ble, usb}
	}
}

// PairBLE scans for busylights advertising the onIT service and calls choose
// for each one found; return true to pair with it. Pairing connects and reads
// the encrypted Events characteristic, which makes macOS show its passkey
// dialog (the OS owns pairing). On success the device is stored in config and
// becomes the preferred transport. choose runs on the scan callback; return
// promptly or block while the user picks — no further devices are reported
// meanwhile. Cancel via ctx.
func (l *Light) PairBLE(ctx context.Context, choose func(BLEDevice) bool) error {
	dev, err := blePair(ctx, choose)
	if err != nil {
		return err
	}
	saveBLEDevice(dev)
	l.mu.Lock()
	old := l.ble
	l.ble = newBLELink(dev.ID, l.handleBLEEvent)
	l.mu.Unlock()
	if old != nil {
		old.close()
	}
	return nil
}

// ForgetBLE drops the bonded device from config and closes the BLE link.
// The OS bond itself is removed in the system Bluetooth settings.
func (l *Light) ForgetBLE() {
	clearBLEDevice()
	l.mu.Lock()
	old := l.ble
	l.ble = nil
	l.mu.Unlock()
	if old != nil {
		old.close()
	}
}

// Transport reports the live link for the tray: "ble", "usb", or "" while
// disconnected.
func (l *Light) Transport() string {
	if ble := l.bleTr(); ble != nil && ble.connected() {
		return "ble"
	}
	if l.usb.connected() {
		return "usb"
	}
	return ""
}

// BLEBonded reports whether a BLE busylight is paired (bonded in config).
func (l *Light) BLEBonded() bool {
	return l.bleTr() != nil
}

// PairingLost reports that the bonded device refused the encrypted link
// (bond deleted or board reflashed). The link stops retrying; re-pair from
// the tray.
func (l *Light) PairingLost() bool {
	ble := l.bleTr()
	return ble != nil && ble.pairingLost()
}
