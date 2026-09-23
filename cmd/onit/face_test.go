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

	f := newDeviceFace(faceSize)
	f.setCustom("Hi", faceBlack)
	if got := customLines(f); len(got) != 1 || got[0] != "Hi" {
		t.Fatalf("short custom lines = %q, want [Hi]", got)
	}
	if f.lines[0].TextSize != 51 {
		t.Errorf("short custom size = %v, want the biggest (51)", f.lines[0].TextSize)
	}

	// a few short words spread over lines at a big size rather than
	// squeezing onto fewer lines at a small one
	f3 := newDeviceFace(faceSize)
	f3.setCustom("Back in 5", faceBlack)
	if got := customLines(f3); len(got) < 2 {
		t.Errorf("Back in 5 lines = %q, want spread over 2+", got)
	}
	if f3.lines[0].TextSize < 38 {
		t.Errorf("Back in 5 size = %v, want a doubled font (>= 38)", f3.lines[0].TextSize)
	}

	msg := "Walking the dog around the block right now"
	f2 := newDeviceFace(faceSize)
	f2.setCustom(msg, faceBlack)
	got := customLines(f2)
	if len(got) < 3 {
		t.Fatalf("long custom lines = %q, want 3+ wrapped lines", got)
	}
	if joined := strings.Join(got, " "); joined != msg {
		t.Errorf("wrapped custom = %q, want all words in order (%q)", joined, msg)
	}
}

// The compact face draws the same screens at a smaller size: the fitted
// message keeps all its words and its text shrinks with the face.
func TestCompactFaceScales(t *testing.T) {
	test.NewApp()

	big, small := newDeviceFace(faceSize), newDeviceFace(compactFaceSize)
	msg := "Walking the dog around the block right now"
	big.setCustom(msg, faceBlack)
	small.setCustom(msg, faceBlack)
	if joined := strings.Join(customLines(small), " "); joined != msg {
		t.Errorf("compact wrapped custom = %q, want all words in order", joined)
	}
	if small.lines[0].TextSize >= big.lines[0].TextSize {
		t.Errorf("compact text %v, want smaller than full-size %v",
			small.lines[0].TextSize, big.lines[0].TextSize)
	}

	small.Set("available", nil)
	if got, want := small.lines[0].TextSize, float32(19)*compactFaceSize/faceSize; got != want {
		t.Errorf("compact caption size = %v, want %v", got, want)
	}
	if small.root.MinSize().Width != compactFaceSize {
		t.Errorf("compact face width = %v, want %v", small.root.MinSize().Width, compactFaceSize)
	}
}
