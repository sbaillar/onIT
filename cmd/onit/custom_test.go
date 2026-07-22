package main

import (
	"fmt"
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

func TestRememberRecallForgetColors(t *testing.T) {
	var list []string

	// remember and recall a message's colors
	list = rememberColors(list, "112233", "AABBCC", "Back in 5")
	bg, fg, ok := recallColors(list, "Back in 5")
	if !ok || bg != "112233" || fg != "AABBCC" {
		t.Fatalf("recall = %q %q %v", bg, fg, ok)
	}
	if _, _, ok := recallColors(list, "On lunch"); ok {
		t.Fatal("recalled colors for a message never remembered")
	}

	// re-remembering replaces, not duplicates
	list = rememberColors(list, "000080", "FFFFFF", "Back in 5")
	if len(list) != 1 {
		t.Fatalf("list = %q, want one entry", list)
	}
	if bg, _, _ := recallColors(list, "Back in 5"); bg != "000080" {
		t.Fatalf("recall after update = %q", bg)
	}

	// remembering the defaults just forgets the entry
	list = rememberColors(list, defaultCustomBg, defaultCustomFg, "Back in 5")
	if len(list) != 0 {
		t.Fatalf("default colors kept an entry: %q", list)
	}

	// forget removes only the named message
	list = rememberColors(list, "112233", "AABBCC", "A")
	list = rememberColors(list, "445566", "DDEEFF", "B")
	list = forgetColors(list, "A")
	if _, _, ok := recallColors(list, "A"); ok {
		t.Fatal("A still remembered after forget")
	}
	if _, _, ok := recallColors(list, "B"); !ok {
		t.Fatal("forget(A) also dropped B")
	}

	// capped so preferences don't grow forever
	list = nil
	for i := 0; i < 60; i++ {
		list = rememberColors(list, "112233", "AABBCC", fmt.Sprintf("msg %d", i))
	}
	if len(list) > 50 {
		t.Fatalf("list grew to %d entries, want <= 50", len(list))
	}
	if _, _, ok := recallColors(list, "msg 59"); !ok {
		t.Fatal("newest entry was evicted")
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
