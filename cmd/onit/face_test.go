package main

import (
	"strings"
	"testing"

	"fyne.io/fyne/v2/test"
)

func customLines(f *deviceFace) []string {
	var out []string
	for _, l := range f.lines {
		if l.Visible() && l.Text != "" {
			out = append(out, l.Text)
		}
	}
	return out
}

func TestSetCustomFitsShortAndLong(t *testing.T) {
	test.NewApp()

	f := newDeviceFace()
	f.setCustom("Hi")
	if got := customLines(f); len(got) != 1 || got[0] != "Hi" {
		t.Fatalf("short custom lines = %q, want [Hi]", got)
	}
	if f.lines[0].TextSize != 19 {
		t.Errorf("short custom size = %v, want the biggest (19)", f.lines[0].TextSize)
	}

	msg := "Walking the dog around the block right now"
	f2 := newDeviceFace()
	f2.setCustom(msg)
	got := customLines(f2)
	if len(got) < 3 {
		t.Fatalf("long custom lines = %q, want 3+ wrapped lines", got)
	}
	if joined := strings.Join(got, " "); joined != msg {
		t.Errorf("wrapped custom = %q, want all words in order (%q)", joined, msg)
	}
}
