package busylight

import (
	"encoding/base64"
	"strings"
	"testing"
)

// The firmware parses DECKIMG by locating the first three colons and taking
// everything after the third as base64 (busylight_round.ino, handleLine).
func TestDeckImageLine(t *testing.T) {
	img := []byte{0x00, 0xFF, 0x12, 0x34}
	line := deckImageLine(7, img, true)

	parts := strings.SplitN(line, ":", 4)
	if len(parts) != 4 {
		t.Fatalf("line splits into %d fields, want 4: %q", len(parts), line)
	}
	if parts[0] != "DECKIMG" || parts[1] != "7" || parts[2] != "1" {
		t.Errorf("header = %q/%q/%q, want DECKIMG/7/1", parts[0], parts[1], parts[2])
	}
	if strings.ContainsAny(parts[3], ":") {
		t.Errorf("payload contains a colon, which would break the parse: %q", parts[3])
	}
	got, err := base64.StdEncoding.DecodeString(parts[3])
	if err != nil {
		t.Fatalf("payload not base64: %v", err)
	}
	if string(got) != string(img) {
		t.Errorf("payload decodes to %v, want %v", got, img)
	}

	// not the last image of a sync
	if f := strings.SplitN(deckImageLine(0, img, false), ":", 4)[2]; f != "0" {
		t.Errorf("last flag = %q, want 0", f)
	}
}
