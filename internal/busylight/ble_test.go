package busylight

import (
	"bytes"
	"encoding/binary"
	"testing"
)

// fakeBLE is a fakeTransport that also carries bond state.
type fakeBLE struct {
	fakeTransport
	lost bool
}

func (f *fakeBLE) pairingLost() bool { return f.lost }

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
	for _, size := range []int{1, 503, 504, 505, 28800} { // 28800 = 120x120 RGB565
		img := make([]byte, size)
		for i := range img {
			img[i] = byte(i * 7)
		}
		chunks := emojiChunks(img)

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
			copy(out[off:], c[bleChunkHeader:])
		}
		if !bytes.Equal(out, img) {
			t.Errorf("size %d: reassembled image differs from original", size)
		}
	}
}

func TestEmojiChunksFullChunkSize(t *testing.T) {
	// 28,800-byte emoji: every chunk but the last is exactly 512 B on the wire.
	chunks := emojiChunks(make([]byte, 28800))
	for i, c := range chunks[:len(chunks)-1] {
		if len(c) != 512 {
			t.Errorf("chunk %d = %d bytes, want 512", i, len(c))
		}
	}
	if last := chunks[len(chunks)-1]; len(last) != bleChunkHeader+28800%bleChunkPayload {
		t.Errorf("last chunk = %d bytes, want %d", len(last), bleChunkHeader+28800%bleChunkPayload)
	}
}
