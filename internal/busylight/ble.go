package busylight

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// onIT BLE GATT layout — keep in sync with both firmware sketches (the UUIDs
// are shared; "6f6e4954" = "onIT"). Three characteristics.
//
//	Service  6f6e4954-0175-4b1e-8001-000000000001
//	  Command  6f6e4954-0175-4b1e-8001-000000000002  write   protocol lines: STATE:*, VERSION,
//	                                                         SPIN, TIME:<unix>,
//	                                                         TZ:<posix-tz>, DECK:<count>
//	  Emoji    6f6e4954-0175-4b1e-8001-000000000003  write   10-byte v2 header (offset u32 LE,
//	                                                         total u16 LE, seq u16 LE, slot u8,
//	                                                         flags u8) + raw RGB565 chunk
//	                                                         (≤502 B payload); slot 0xFF displays
//	                                                         now, slots 0..19 store into the
//	                                                         roulette deck in LittleFS
//	  Events   6f6e4954-0175-4b1e-8001-000000000004  notify  TOUCH:TAP, TOUCH:LONG,
//	                                                         ROULETTE:<slot>,
//	                                                         DECKOK:<slot> (deck image
//	                                                         persisted; paces the sync),
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

// Emoji chunking: each Emoji write is a 10-byte v2 header plus up to 502
// bytes of raw RGB565 pixels, so a chunk fits a 512-byte ATT write.
const (
	bleChunkHeader  = 10
	bleChunkPayload = 502
	bleSlotLive     = 0xFF // slot value for "display now" (not a deck slot)
	bleFlagDeckLast = 0x01 // flags bit0: last chunk of a whole deck sync
	// DeckSlots is the roulette deck capacity (slots 0..19 in the device's
	// LittleFS); the deck source and SyncDeck both cap at it.
	DeckSlots = 20
)

// emojiChunks splits an RGB565 image into Emoji characteristic writes.
// Header fields are little-endian: offset u32 (byte offset of this chunk),
// total u16 (image size in bytes), seq u16 (chunk index from 0), slot u8
// (deck slot, or bleSlotLive to display now), flags u8 (bleFlagDeckLast on
// the final chunk when lastOfSync).
func emojiChunks(rgb565 []byte, slot byte, lastOfSync bool) [][]byte {
	var chunks [][]byte
	for seq, off := 0, 0; off < len(rgb565); seq++ {
		n := min(len(rgb565)-off, bleChunkPayload)
		c := make([]byte, bleChunkHeader+n)
		binary.LittleEndian.PutUint32(c[0:], uint32(off))
		binary.LittleEndian.PutUint16(c[4:], uint16(len(rgb565)))
		binary.LittleEndian.PutUint16(c[6:], uint16(seq))
		c[8] = slot
		if lastOfSync && off+n == len(rgb565) {
			c[9] = bleFlagDeckLast
		}
		copy(c[bleChunkHeader:], rgb565[off:off+n])
		chunks = append(chunks, c)
		off += n
	}
	return chunks
}

// BLEDevice identifies a pairable/bonded busylight (ID is the OS peripheral
// identifier — a UUID on macOS). Deck caches the per-slot content hashes of
// the roulette deck last synced to it, so unchanged slots are skipped; Sig is
// the signature of the last fully synced deck, so an unchanged deck skips
// rendering entirely.
type BLEDevice struct {
	ID   string   `json:"id"`
	Name string   `json:"name"`
	Deck []string `json:"deck,omitempty"`
	Sig  string   `json:"sig,omitempty"`
}

func bleConfigFile() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".onit_ble.json")
}

// loadBLEDevice restores the bonded device from disk (nil if none is stored).
func loadBLEDevice() *BLEDevice {
	b, err := os.ReadFile(bleConfigFile())
	if err != nil {
		return nil
	}
	var d BLEDevice
	if json.Unmarshal(b, &d) != nil || d.ID == "" {
		return nil
	}
	return &d
}

// saveDev persists the in-memory bonded record (removing the file when there
// is none). Caller holds l.mu.
func (l *Light) saveDev() {
	if l.dev == nil {
		os.Remove(bleConfigFile())
		return
	}
	if b, err := json.Marshal(l.dev); err == nil {
		os.WriteFile(bleConfigFile(), b, 0o600)
	}
}

// bleLink is a BLE transport that also exposes bond state and the deck-slot
// upload path. Platform files provide newBLELink (nil where BLE is
// unsupported) and blePair.
type bleLink interface {
	transport
	pairingLost() bool
	sendDeckImage(slot int, rgb565 []byte, lastOfSync bool) bool
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
	l.mu.Lock()
	old := l.ble
	l.dev = &dev
	l.saveDev()
	l.ble = newBLELink(dev.ID, l.handleBLEEvent, l.handleBLEConnect)
	l.mu.Unlock()
	if old != nil {
		old.close()
	}
	return nil
}

// ForgetBLE drops the bonded device from config and closes the BLE link.
// The OS bond itself is removed in the system Bluetooth settings.
func (l *Light) ForgetBLE() {
	l.mu.Lock()
	old := l.ble
	l.ble = nil
	l.dev = nil
	l.saveDev() // removes the config file
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

// PushClock sets the device's standalone clock: the Mac's timezone as a POSIX
// TZ string (e.g. EST5EDT,M3.2.0,M11.1.0) so it tracks DST on its own, then
// the current UTC epoch. This is the only way the device learns the time —
// it has no RTC battery and no network of its own — so it goes over whichever
// transport is live, not BLE alone: a USB-only board still needs a clock for
// when the app quits. Order matters; TZ first means the first TIME: push
// already renders in local time.
func (l *Light) PushClock() error {
	now := time.Now() // one reading, so the zone and the time can't straddle a DST change
	for _, line := range []string{
		"TZ:" + posixTZ(now.Location(), now.Year()),
		"TIME:" + strconv.FormatInt(now.Unix(), 10),
	} {
		if !l.sendLine(line) {
			return errors.New("busylight not connected")
		}
	}
	return nil
}

// Spin starts the emoji roulette on the device; the winner arrives as a
// ROULETTE:<slot> event (see SetOnRoulette).
func (l *Light) Spin() error {
	if !l.sendLine("SPIN") {
		return errors.New("busylight not connected")
	}
	return nil
}

// deckHashes returns the content hash of each deck image.
func deckHashes(images [][]byte) []string {
	hashes := make([]string, len(images))
	for i, img := range images {
		sum := sha256.Sum256(img)
		hashes[i] = hex.EncodeToString(sum[:])
	}
	return hashes
}

// deckChangedSlots lists the slots whose hash differs from the cached one.
func deckChangedSlots(cached, hashes []string) []int {
	var changed []int
	for i, h := range hashes {
		if i >= len(cached) || cached[i] != h {
			changed = append(changed, i)
		}
	}
	return changed
}

// SyncDeck uploads the roulette deck (≤20 images, 120x120 RGB565 each) into
// the device's LittleFS slots, then announces the deck size with DECK:<n> and
// records sig as the fully-synced signature. Slots matching the per-slot
// content-hash cache are skipped; a fully unchanged deck sends nothing at all.
// Each slot's hash is persisted as soon as the device acks it, so an
// interrupted sync resumes from where it left off.
func (l *Light) SyncDeck(images [][]byte, sig string) error {
	if len(images) > DeckSlots {
		images = images[:DeckSlots]
	}
	ble := l.bleTr()
	if ble == nil {
		return errors.New("no BLE busylight bonded")
	}
	hashes := deckHashes(images)

	// snapshot the bonded identity and per-slot cache under l.mu
	l.mu.Lock()
	if l.dev == nil {
		l.mu.Unlock()
		return errors.New("no BLE busylight bonded")
	}
	id := l.dev.ID
	changed := deckChangedSlots(l.dev.Deck, hashes)
	unchanged := len(changed) == 0 && len(l.dev.Deck) == len(hashes) && l.dev.Sig == sig
	l.mu.Unlock()
	if unchanged {
		return nil
	}
	// a first upload takes tens of seconds; a reconnect must not start a
	// second sync writing the same slots and cache file concurrently
	if !l.deckSyncing.CompareAndSwap(false, true) {
		return errors.New("deck sync already in flight")
	}
	defer l.deckSyncing.Store(false)
	if len(images) == 0 {
		return nil // never announce DECK:0 — the device divides by the count
	}
	for i, slot := range changed {
		if !ble.sendDeckImage(slot, images[slot], i == len(changed)-1) {
			return errors.New("BLE deck upload failed")
		}
		// persist this slot's hash the moment the device acks it, but only
		// while the bonded device is still the one this sync started with —
		// a mid-sync Forget/re-pair must not be resurrected or reverted
		if !l.recordDeckSlot(id, slot, hashes[slot]) {
			return errors.New("BLE busylight changed mid-sync")
		}
	}
	if !ble.sendLine(fmt.Sprintf("DECK:%d", len(images))) {
		return errors.New("BLE deck count write failed")
	}
	l.recordDeckSynced(id, hashes, sig)
	return nil
}

// recordDeckSlot writes one slot's cached hash to the in-memory bonded record
// and persists it, but only while the bonded device is still id. Returns
// false (without touching state) if the device changed mid-sync.
func (l *Light) recordDeckSlot(id string, slot int, hash string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.dev == nil || l.dev.ID != id {
		return false
	}
	for len(l.dev.Deck) <= slot {
		l.dev.Deck = append(l.dev.Deck, "")
	}
	l.dev.Deck[slot] = hash
	l.saveDev()
	return true
}

// recordDeckSynced marks the whole deck synced (full hash set + signature) on
// the in-memory record if the bonded device is still id.
func (l *Light) recordDeckSynced(id string, hashes []string, sig string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.dev == nil || l.dev.ID != id {
		return
	}
	l.dev.Deck = hashes
	l.dev.Sig = sig
	l.saveDev()
}

// DeckSyncing reports whether a deck upload is in flight (for the tray).
func (l *Light) DeckSyncing() bool {
	return l.deckSyncing.Load()
}

// handleBLEConnect runs the post-connect pushes on the BLE link's goroutine:
// the clock on every connect, then a deck sync. The deck source reports a
// cheap signature (no rendering) alongside the render closure; if it matches
// the last fully-synced deck, rendering and SyncDeck are skipped entirely.
func (l *Light) handleBLEConnect() {
	if err := l.PushClock(); err != nil {
		log.Printf("BLE clock push failed: %v", err)
	}
	src := l.deckSrc()
	if src == nil || l.deckSyncing.Load() {
		return
	}
	sig, render := src()
	l.mu.Lock()
	unchanged := l.dev != nil && l.dev.Sig == sig
	l.mu.Unlock()
	if unchanged {
		return
	}
	if err := l.SyncDeck(render(), sig); err != nil {
		log.Printf("BLE deck sync failed: %v", err)
	}
}
