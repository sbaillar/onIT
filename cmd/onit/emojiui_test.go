package main

import (
	"slices"
	"testing"
)

func TestPushHistory(t *testing.T) {
	var h []string
	h = pushHistory(h, "Back in 5")
	h = pushHistory(h, "On lunch")
	h = pushHistory(h, "BRB")
	if want := []string{"BRB", "On lunch", "Back in 5"}; !slices.Equal(h, want) {
		t.Fatalf("history = %q, want %q (newest first)", h, want)
	}

	// resending an old message moves it to the front, no duplicate
	h = pushHistory(h, "On lunch")
	if want := []string{"On lunch", "BRB", "Back in 5"}; !slices.Equal(h, want) {
		t.Fatalf("history after resend = %q, want %q", h, want)
	}

	// capped at 3, oldest dropped
	h = pushHistory(h, "Gone home")
	if want := []string{"Gone home", "On lunch", "BRB"}; !slices.Equal(h, want) {
		t.Fatalf("history after 4th = %q, want %q", h, want)
	}
}

func TestCustomOptions(t *testing.T) {
	// no history: just the canned list
	if got := customOptions(nil); !slices.Equal(got, cannedTexts) {
		t.Fatalf("customOptions(nil) = %q, want canned list", got)
	}

	// history first, canned after, no duplicates
	got := customOptions([]string{"Dog walk", cannedTexts[1]})
	if got[0] != "Dog walk" || got[1] != cannedTexts[1] {
		t.Fatalf("customOptions = %q, want history first", got)
	}
	seen := map[string]bool{}
	for _, o := range got {
		if seen[o] {
			t.Fatalf("customOptions has duplicate %q", o)
		}
		seen[o] = true
	}
	if len(got) != len(cannedTexts)+1 {
		t.Fatalf("customOptions has %d entries, want %d", len(got), len(cannedTexts)+1)
	}
}
