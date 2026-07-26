package busylight

import (
	"bytes"
	"encoding/binary"
	"slices"
	"testing"
	"time"
)

// fakeBLE is a fakeTransport that also carries bond state and records the
// deck-slot uploads (protocol lines land in the embedded fakeTransport.lines).
type fakeBLE struct {
	fakeTransport
	lost         bool
	deck         []deckWrite
	failDeckFrom int // fail sendDeckImage at slots >= this (0 disables)
}

type deckWrite struct {
	slot int
	img  []byte
	last bool
}

func (f *fakeBLE) pairingLost() bool { return f.lost }

func (f *fakeBLE) sendDeckImage(slot int, rgb565 []byte, lastOfSync bool) bool {
	f.deck = append(f.deck, deckWrite{slot, rgb565, lastOfSync})
	if f.failDeckFrom > 0 && slot >= f.failDeckFrom {
		return false
	}
	return f.ok
}

func TestOrderTransports(t *testing.T) {
	usb := &fakeTransport{}
	tests := []struct {
		name string
		ble  *fakeBLE // nil = no bonded device
		want func(ble *fakeBLE) []transport
	}{
		{
			name: "no bonded device: USB only",
			ble:  nil,
			want: func(*fakeBLE) []transport { return []transport{usb} },
		},
		{
			name: "BLE connected: USB ignored",
			ble:  &fakeBLE{fakeTransport: fakeTransport{up: true}},
			want: func(b *fakeBLE) []transport { return []transport{b} },
		},
		{
			name: "BLE down: BLE first, USB fallback",
			ble:  &fakeBLE{},
			want: func(b *fakeBLE) []transport { return []transport{b, usb} },
		},
		{
			name: "pairing lost: USB only, no BLE retries",
			ble:  &fakeBLE{lost: true},
			want: func(*fakeBLE) []transport { return []transport{usb} },
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var ble bleLink
			if tt.ble != nil {
				ble = tt.ble
			}
			got := orderTransports(ble, usb)
			want := tt.want(tt.ble)
			if len(got) != len(want) {
				t.Fatalf("got %d transports, want %d", len(got), len(want))
			}
			for i := range got {
				if got[i] != want[i] {
					t.Errorf("transport[%d] = %T(%p), want %T(%p)", i, got[i], got[i], want[i], want[i])
				}
			}
		})
	}
}

func TestLightSendFallsBackToUSB(t *testing.T) {
	usb := &fakeTransport{ok: true}
	ble := &fakeBLE{} // down: sendLine fails
	l := &Light{usb: usb, ble: ble}

	l.Send("meeting")
	if len(ble.lines) != 1 || ble.lines[0] != "STATE:meeting" {
		t.Errorf("BLE tried %q, want [STATE:meeting]", ble.lines)
	}
	if len(usb.lines) != 1 || usb.lines[0] != "STATE:meeting" {
		t.Errorf("USB fallback got %q, want [STATE:meeting]", usb.lines)
	}
}

func TestLightSendSkipsUSBWhileBLEConnected(t *testing.T) {
	usb := &fakeTransport{ok: true}
	ble := &fakeBLE{fakeTransport: fakeTransport{up: true, ok: true}}
	l := &Light{usb: usb, ble: ble}

	l.Send("available")
	if len(ble.lines) != 1 {
		t.Errorf("BLE got %q, want one line", ble.lines)
	}
	if len(usb.lines) != 0 {
		t.Errorf("USB got %q, want none while BLE is connected", usb.lines)
	}
}

func TestLightTransport(t *testing.T) {
	usb := &fakeTransport{}
	l := &Light{usb: usb}

	if got := l.Transport(); got != "" {
		t.Errorf("Transport = %q, want empty while disconnected", got)
	}
	usb.up = true
	if got := l.Transport(); got != "usb" {
		t.Errorf("Transport = %q, want usb", got)
	}
	ble := &fakeBLE{fakeTransport: fakeTransport{up: true}}
	l.ble = ble
	if got := l.Transport(); got != "ble" {
		t.Errorf("Transport = %q, want ble", got)
	}
	ble.up = false
	if got := l.Transport(); got != "usb" {
		t.Errorf("Transport = %q, want usb while BLE is down", got)
	}
}

func TestLightBLEBonded(t *testing.T) {
	l := &Light{usb: &fakeTransport{}}
	if l.BLEBonded() {
		t.Error("BLEBonded = true with no bonded device")
	}
	l.ble = &fakeBLE{}
	if !l.BLEBonded() {
		t.Error("BLEBonded = false with a bonded device")
	}
}

func TestLightPairingLost(t *testing.T) {
	l := &Light{usb: &fakeTransport{}}
	if l.PairingLost() {
		t.Error("PairingLost = true with no bonded device")
	}
	ble := &fakeBLE{}
	l.ble = ble
	if l.PairingLost() {
		t.Error("PairingLost = true with a healthy bond")
	}
	ble.lost = true
	if !l.PairingLost() {
		t.Error("PairingLost = false after the bond was refused")
	}
}

func TestEmojiChunksRoundTrip(t *testing.T) {
	for _, size := range []int{1, 501, 502, 503, 28800} { // 28800 = 120x120 RGB565
		img := make([]byte, size)
		for i := range img {
			img[i] = byte(i * 7)
		}
		chunks := emojiChunks(img, 7, true)

		wantChunks := (size + bleChunkPayload - 1) / bleChunkPayload
		if len(chunks) != wantChunks {
			t.Errorf("size %d: %d chunks, want %d", size, len(chunks), wantChunks)
		}
		out := make([]byte, size)
		for i, c := range chunks {
			if len(c) > bleChunkHeader+bleChunkPayload {
				t.Errorf("size %d: chunk %d is %d bytes, exceeds %d", size, i, len(c), bleChunkHeader+bleChunkPayload)
			}
			off := binary.LittleEndian.Uint32(c[0:])
			total := binary.LittleEndian.Uint16(c[4:])
			seq := binary.LittleEndian.Uint16(c[6:])
			if int(total) != size {
				t.Errorf("size %d: chunk %d total = %d", size, i, total)
			}
			if int(seq) != i {
				t.Errorf("size %d: chunk %d seq = %d", size, i, seq)
			}
			if c[8] != 7 {
				t.Errorf("size %d: chunk %d slot = %d, want 7", size, i, c[8])
			}
			wantFlags := byte(0)
			if i == len(chunks)-1 {
				wantFlags = bleFlagDeckLast // lastOfSync marks only the final chunk
			}
			if c[9] != wantFlags {
				t.Errorf("size %d: chunk %d flags = %#x, want %#x", size, i, c[9], wantFlags)
			}
			copy(out[off:], c[bleChunkHeader:])
		}
		if !bytes.Equal(out, img) {
			t.Errorf("size %d: reassembled image differs from original", size)
		}
	}
}

func TestEmojiChunksLiveSlot(t *testing.T) {
	// the live display path: slot 0xFF, no deck-sync flag anywhere
	for i, c := range emojiChunks(make([]byte, 1200), bleSlotLive, false) {
		if c[8] != bleSlotLive {
			t.Errorf("chunk %d slot = %#x, want %#x", i, c[8], bleSlotLive)
		}
		if c[9] != 0 {
			t.Errorf("chunk %d flags = %#x, want 0", i, c[9])
		}
	}
}

func TestEmojiChunksFullChunkSize(t *testing.T) {
	// 28,800-byte emoji: every chunk but the last is exactly 512 B on the wire.
	chunks := emojiChunks(make([]byte, 28800), bleSlotLive, false)
	for i, c := range chunks[:len(chunks)-1] {
		if len(c) != 512 {
			t.Errorf("chunk %d = %d bytes, want 512", i, len(c))
		}
	}
	if last := chunks[len(chunks)-1]; len(last) != bleChunkHeader+28800%bleChunkPayload {
		t.Errorf("last chunk = %d bytes, want %d", len(last), bleChunkHeader+28800%bleChunkPayload)
	}
}

func TestDeckChangedSlots(t *testing.T) {
	a, b, c := deckHashes([][]byte{{1}})[0], deckHashes([][]byte{{2}})[0], deckHashes([][]byte{{3}})[0]
	tests := []struct {
		name           string
		cached, hashes []string
		want           []int
	}{
		{"empty cache: everything changed", nil, []string{a, b}, []int{0, 1}},
		{"unchanged", []string{a, b}, []string{a, b}, nil},
		{"one slot changed", []string{a, b, c}, []string{a, c, c}, []int{1}},
		{"deck grew", []string{a}, []string{a, b}, []int{1}},
		{"deck shrank: nothing to upload", []string{a, b}, []string{a}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := deckChangedSlots(tt.cached, tt.hashes); !slices.Equal(got, tt.want) {
				t.Errorf("deckChangedSlots = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSyncDeckSkipsUnchangedSlots(t *testing.T) {
	t.Setenv("HOME", t.TempDir()) // isolate the ~/.onit_ble.json cache
	ble := &fakeBLE{fakeTransport: fakeTransport{up: true, ok: true}}
	l := &Light{usb: &fakeTransport{}, ble: ble, dev: &BLEDevice{ID: "test"}}
	images := [][]byte{{1}, {2}, {3}}
	const sig = "deck" // deck identity held constant; per-slot hashes drive skips

	// first sync: every slot uploaded, sync flag on the last, DECK announced
	if err := l.SyncDeck(images, sig); err != nil {
		t.Fatalf("first SyncDeck: %v", err)
	}
	if len(ble.deck) != 3 {
		t.Fatalf("first sync uploaded %d slots, want 3", len(ble.deck))
	}
	for i, d := range ble.deck {
		if d.slot != i || !bytes.Equal(d.img, images[i]) {
			t.Errorf("upload %d = slot %d img %v", i, d.slot, d.img)
		}
		if d.last != (i == 2) {
			t.Errorf("upload %d lastOfSync = %v", i, d.last)
		}
	}
	if len(ble.lines) != 1 || ble.lines[0] != "DECK:3" {
		t.Errorf("protocol lines = %v, want [DECK:3]", ble.lines)
	}

	// unchanged deck: nothing at all is sent
	ble.deck, ble.lines = nil, nil
	if err := l.SyncDeck(images, sig); err != nil {
		t.Fatalf("unchanged SyncDeck: %v", err)
	}
	if len(ble.deck) != 0 || len(ble.lines) != 0 {
		t.Errorf("unchanged deck sent %d uploads, %d lines, want none", len(ble.deck), len(ble.lines))
	}

	// one slot changed: only that slot uploads, flagged as last of the sync
	images[1] = []byte{9}
	if err := l.SyncDeck(images, sig); err != nil {
		t.Fatalf("changed SyncDeck: %v", err)
	}
	if len(ble.deck) != 1 || ble.deck[0].slot != 1 || !ble.deck[0].last {
		t.Errorf("changed sync uploads = %+v, want one final upload of slot 1", ble.deck)
	}
	if len(ble.lines) != 1 || ble.lines[0] != "DECK:3" {
		t.Errorf("protocol lines = %v, want [DECK:3]", ble.lines)
	}

	// deck shrank: no uploads, but the new count is announced
	ble.deck, ble.lines = nil, nil
	if err := l.SyncDeck(images[:2], sig); err != nil {
		t.Fatalf("shrunk SyncDeck: %v", err)
	}
	if len(ble.deck) != 0 {
		t.Errorf("shrunk deck uploaded %d slots, want none", len(ble.deck))
	}
	if len(ble.lines) != 1 || ble.lines[0] != "DECK:2" {
		t.Errorf("protocol lines = %v, want [DECK:2]", ble.lines)
	}
}

func TestSyncDeckResumesPerSlot(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ble := &fakeBLE{fakeTransport: fakeTransport{up: true, ok: true}}
	l := &Light{usb: &fakeTransport{}, ble: ble, dev: &BLEDevice{ID: "test"}}
	images := [][]byte{{1}, {2}, {3}}

	// first upload acks slots 0 and 1, then fails on slot 2
	ble.failDeckFrom = 2
	if err := l.SyncDeck(images, "deck"); err == nil {
		t.Fatal("SyncDeck succeeded despite a slot upload failure")
	}
	if len(ble.deck) != 3 { // slot 2 was attempted (and failed)
		t.Fatalf("interrupted sync attempted %d uploads, want 3", len(ble.deck))
	}

	// resume: only the un-acked slot 2 is re-uploaded, then DECK announced
	ble.deck, ble.lines, ble.failDeckFrom = nil, nil, -1
	if err := l.SyncDeck(images, "deck"); err != nil {
		t.Fatalf("resume SyncDeck: %v", err)
	}
	if len(ble.deck) != 1 || ble.deck[0].slot != 2 {
		t.Errorf("resume uploads = %+v, want only slot 2", ble.deck)
	}
	if len(ble.lines) != 1 || ble.lines[0] != "DECK:3" {
		t.Errorf("protocol lines = %v, want [DECK:3]", ble.lines)
	}
}

func TestSyncDeckWithoutBLE(t *testing.T) {
	l := &Light{usb: &fakeTransport{}}
	if err := l.SyncDeck([][]byte{{1}}, "deck"); err == nil {
		t.Error("SyncDeck succeeded with no bonded BLE device")
	}
}

func TestLightSpinAndRoulette(t *testing.T) {
	ble := &fakeBLE{fakeTransport: fakeTransport{up: true, ok: true}}
	l := &Light{usb: &fakeTransport{}, ble: ble}
	if err := l.Spin(); err != nil {
		t.Fatalf("Spin: %v", err)
	}
	if len(ble.lines) != 1 || ble.lines[0] != "SPIN" {
		t.Errorf("BLE got %q, want [SPIN]", ble.lines)
	}

	got := make(chan int, 1)
	l.SetOnRoulette(func(slot int) { got <- slot })
	l.handleBLEEvent("ROULETTE:7")
	select {
	case slot := <-got:
		if slot != 7 {
			t.Errorf("roulette slot = %d, want 7", slot)
		}
	case <-time.After(time.Second):
		t.Error("roulette callback never fired")
	}
}
