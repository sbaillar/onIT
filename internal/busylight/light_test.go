package busylight

import (
	"testing"
	"time"
)

// fakeTransport records sent lines and lets tests script the link state.
type fakeTransport struct {
	lines []string
	emoji [][]byte
	up    bool
	ok    bool
	shut  bool
}

func (f *fakeTransport) sendLine(line string) bool {
	f.lines = append(f.lines, line)
	return f.ok
}
func (f *fakeTransport) sendEmoji(rgb565 []byte) bool {
	f.emoji = append(f.emoji, rgb565)
	return f.ok
}
func (f *fakeTransport) connected() bool { return f.up }
func (f *fakeTransport) close()          { f.shut = true }

func TestLightTransportDelegation(t *testing.T) {
	f := &fakeTransport{ok: true}
	l := NewLight()
	l.usb = f
	l.ble = nil // ignore any bonded device on the host running the tests

	l.Send("meeting")
	if len(f.lines) != 1 || f.lines[0] != "STATE:meeting" {
		t.Errorf("Send lines = %q, want [STATE:meeting]", f.lines)
	}

	if !l.SendLine("EMOJI:abc") {
		t.Error("SendLine = false, want true")
	}
	if got := f.lines[len(f.lines)-1]; got != "EMOJI:abc" {
		t.Errorf("SendLine sent %q, want EMOJI:abc", got)
	}

	f.ok = false
	if l.SendLine("VERSION") {
		t.Error("SendLine = true on transport failure, want false")
	}

	if l.Connected() {
		t.Error("Connected = true, want false")
	}
	f.up = true
	if !l.Connected() {
		t.Error("Connected = false, want true")
	}

	l.Close()
	if !f.shut {
		t.Error("Close did not reach the transport")
	}
}

func TestParseVersion(t *testing.T) {
	tests := []struct {
		in         string
		ver, board string
	}{
		{"1.10.0", "1.10.0", "lcd128"}, // legacy 2-field banner = 1.28"
		{"1.11.0:lcd128", "1.11.0", "lcd128"},
		{"1.0.0:amoled175", "1.0.0", "amoled175"},
	}
	for _, tc := range tests {
		ver, board := parseVersion(tc.in)
		if ver != tc.ver || board != tc.board {
			t.Errorf("parseVersion(%q) = %q, %q; want %q, %q", tc.in, ver, board, tc.ver, tc.board)
		}
	}
}

func TestVersionPerTransport(t *testing.T) {
	l := NewLight()
	l.ble = nil // ignore any bonded device on the host running the tests

	if got := l.Board(); got != "" {
		t.Errorf("Board before any banner = %q, want empty", got)
	}

	// legacy 2-field banner: version only, board defaults to the 1.28"
	l.serial.handleLine("VERSION:1.10.0")
	if got := l.Version(); got != "1.10.0" {
		t.Errorf("Version = %q, want 1.10.0", got)
	}
	if got := l.Board(); got != "lcd128" {
		t.Errorf("Board = %q, want lcd128", got)
	}

	// a BLE banner is the BLE device's own state: while BLE is down the
	// serial device still answers
	l.handleBLEEvent("VERSION:1.0.0:amoled175")
	if got := l.Version(); got != "1.10.0" {
		t.Errorf("Version with BLE down = %q, want serial's 1.10.0", got)
	}

	// while BLE is connected its device answers instead
	l.ble = &fakeBLE{fakeTransport: fakeTransport{up: true}}
	if got := l.Version(); got != "1.0.0" {
		t.Errorf("Version over BLE = %q, want 1.0.0", got)
	}
	if got := l.Board(); got != "amoled175" {
		t.Errorf("Board over BLE = %q, want amoled175", got)
	}

	// ClearVersion (pre-flash) forgets the serial banner only
	l.ble = nil
	l.ClearVersion()
	if got := l.Version(); got != "" {
		t.Errorf("Version after ClearVersion = %q, want empty", got)
	}
	if got := l.Board(); got != "" {
		t.Errorf("Board after ClearVersion = %q, want empty", got)
	}

	// unrelated lines leave the version alone
	l.serial.handleLine("hello")
	if got := l.Version(); got != "" {
		t.Errorf("Version after junk line = %q, want empty", got)
	}
}

func TestLightHandleTouch(t *testing.T) {
	l := NewLight()

	// no callback registered: must not panic
	l.handleTouch("TOUCH:TAP")

	got := make(chan string, 1)
	l.SetOnTouch(func(kind string) { got <- kind })

	l.handleTouch("TOUCH:LONG")
	select {
	case kind := <-got:
		if kind != "LONG" {
			t.Errorf("touch kind = %q, want LONG", kind)
		}
	case <-time.After(time.Second):
		t.Fatal("touch callback never fired")
	}

	// non-touch lines do not fire the callback (BLE path dispatches too)
	l.handleBLEEvent("VERSION:1.0.0")
	select {
	case kind := <-got:
		t.Errorf("unexpected touch callback %q", kind)
	case <-time.After(50 * time.Millisecond):
	}
}
