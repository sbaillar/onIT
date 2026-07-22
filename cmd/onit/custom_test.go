package main

import (
	"image/color"
	"testing"
)

func TestSplitCustom(t *testing.T) {
	// plain text: default yellow/black
	bg, fg, text := splitCustom("Back in 5")
	if bg != defaultCustomBg || fg != defaultCustomFg || text != "Back in 5" {
		t.Fatalf("plain = %q %q %q", bg, fg, text)
	}

	// colored payload
	bg, fg, text = splitCustom("112233,AABBCC:Hello there")
	if bg != "112233" || fg != "AABBCC" || text != "Hello there" {
		t.Fatalf("colored = %q %q %q", bg, fg, text)
	}

	// almost-a-color-prefix stays literal text
	for _, s := range []string{"12345,AABBCC:x", "11223G,AABBCC:x", "112233,AABBCC x"} {
		if _, _, text := splitCustom(s); text != s {
			t.Errorf("splitCustom(%q) text = %q, want unchanged", s, text)
		}
	}
}

func TestCustomPayloadRoundTrip(t *testing.T) {
	// defaults produce a bare message (old firmware keeps working)
	if got := customPayload(defaultCustomBg, defaultCustomFg, "Hi"); got != "Hi" {
		t.Fatalf("default payload = %q, want Hi", got)
	}
	if got := customPayload("", "", "Hi"); got != "Hi" {
		t.Fatalf("empty-color payload = %q, want Hi", got)
	}

	p := customPayload("112233", "AABBCC", "Hi")
	bg, fg, text := splitCustom(p)
	if bg != "112233" || fg != "AABBCC" || text != "Hi" {
		t.Fatalf("round trip = %q %q %q (payload %q)", bg, fg, text, p)
	}
}

func TestHexColor(t *testing.T) {
	if c := hexColor("FF8000"); c != (color.NRGBA{0xFF, 0x80, 0x00, 0xFF}) {
		t.Errorf("hexColor(FF8000) = %v", c)
	}
	// garbage falls back to the default background
	if c := hexColor("nope"); c != hexColor(defaultCustomBg) {
		t.Errorf("hexColor(nope) = %v, want default", c)
	}
}
