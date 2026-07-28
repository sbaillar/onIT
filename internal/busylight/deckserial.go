package busylight

import (
	"encoding/base64"
	"errors"
	"fmt"
	"log"
	"time"
)

// Deck sync over USB. BLE streams raw chunks through its Emoji
// characteristic; serial has only text lines, so each image travels as one
// DECKIMG:<slot>:<last>:<base64> line and the device acks it with DECKOK.
//
// The BLE path caches per-slot hashes against the bonded device's record. A
// serial device has no such identity — swap boards and any host-side cache
// would be silently wrong — so instead the device stores the signature of
// the deck it holds and reports it on request. Nothing is uploaded when it
// already matches; when it doesn't, the whole deck goes, which is rare (only
// when the emoji list itself changes) and costs a few seconds — writePaced,
// not the nominal baud, sets the rate.
const (
	deckAckTimeout = 15 * time.Second // one image: transfer + LittleFS write
	deckSigTimeout = 3 * time.Second  // firmware predating DECKSIG never answers
)

// deviceDeckSig asks the connected device which deck it is holding. An empty
// string means "none or unknown", which forces a full upload.
func (l *Light) deviceDeckSig() (string, error) {
	select { // drop a stale reply from an earlier query
	case <-l.serialDeckSig:
	default:
	}
	if !l.serial.sendLine("DECKSIG") {
		return "", errors.New("busylight not connected")
	}
	select {
	case sig := <-l.serialDeckSig:
		return sig, nil
	case <-time.After(deckSigTimeout):
		return "", errors.New("no DECKSIG reply (firmware too old?)")
	}
}

// deckImageLine builds the wire line the firmware parses: it splits on the
// first three colons, so the base64 payload (which has none) is the rest.
func deckImageLine(slot int, rgb565 []byte, lastOfSync bool) string {
	last := 0
	if lastOfSync {
		last = 1
	}
	return fmt.Sprintf("DECKIMG:%d:%d:%s", slot, last, base64.StdEncoding.EncodeToString(rgb565))
}

// sendDeckImageSerial writes one image and waits for the device to ack it,
// which paces the upload: unpaced, a deck outruns both the serial RX buffer
// and the LittleFS write behind it.
func (l *Light) sendDeckImageSerial(slot int, rgb565 []byte, lastOfSync bool) error {
	select { // an ack for an earlier slot must not satisfy this one
	case <-l.serialDeckAck:
	default:
	}
	if !l.serial.sendLine(deckImageLine(slot, rgb565, lastOfSync)) {
		return errors.New("deck image write failed")
	}
	// The channel was drained just above and holds one entry, so the only ack
	// that can arrive is this slot's; anything else is the device disagreeing
	// with us and is worth failing on rather than waiting out.
	select {
	case got := <-l.serialDeckAck:
		if int(got) != slot {
			return fmt.Errorf("deck slot %d acked as %d", slot, got)
		}
		return nil
	case <-time.After(deckAckTimeout):
		return fmt.Errorf("no ack for deck slot %d", slot)
	}
}

// syncDeckSerial uploads the roulette deck over USB when the device isn't
// already holding it. Called after a serial VERSION banner; a no-op when BLE
// owns the deck for this device, when no deck source is registered, or when
// the signatures already agree.
func (l *Light) syncDeckSerial() {
	// Only when BLE is actually carrying the device: bleTr() is non-nil for
	// any *bonded* device, so testing it alone skipped the USB sync entirely
	// for anyone who had ever paired, even with the device out of range.
	if ble := l.bleTr(); ble != nil && ble.connected() {
		return // BLE has its own incremental sync
	}
	src := l.deckSrc()
	if src == nil {
		return
	}
	sig, render := src()
	have, err := l.deviceDeckSig()
	if err != nil {
		log.Printf("deck sync over USB skipped: %v", err)
		return
	}
	if have == sig {
		return
	}
	if !l.deckSyncing.CompareAndSwap(false, true) {
		return // a sync is already in flight
	}
	defer l.deckSyncing.Store(false)
	images := render()
	if len(images) > DeckSlots {
		images = images[:DeckSlots]
	}
	if len(images) == 0 {
		return
	}
	log.Printf("syncing %d emoji(s) to the device over USB", len(images))
	for i, img := range images {
		if err := l.sendDeckImageSerial(i, img, i == len(images)-1); err != nil {
			log.Printf("deck sync over USB failed: %v", err)
			return
		}
	}
	if !l.serial.sendLine(fmt.Sprintf("DECK:%d", len(images))) {
		return
	}
	// record the signature only after every image landed, so an interrupted
	// sync is retried rather than remembered as complete
	l.serial.sendLine("DECKSIG:" + sig)
	log.Print("deck sync over USB complete")
}
