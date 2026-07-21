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

func TestUsageTracking(t *testing.T) {
	var u []string
	for i := 0; i < 3; i++ {
		u = bumpUsage(u, "fire")
	}
	u = bumpUsage(u, "pizza")
	u = bumpUsage(u, "pizza")
	u = bumpUsage(u, "cat")

	if got := topUsed(u, 2); !slices.Equal(got, []string{"fire", "pizza"}) {
		t.Fatalf("topUsed(2) = %q, want [fire pizza]", got)
	}
	// n larger than distinct slugs: all of them, most used first
	if got := topUsed(u, 10); !slices.Equal(got, []string{"fire", "pizza", "cat"}) {
		t.Fatalf("topUsed(10) = %q, want [fire pizza cat]", got)
	}
	// malformed entries are ignored
	if got := topUsed(append(u, "garbage"), 1); !slices.Equal(got, []string{"fire"}) {
		t.Fatalf("topUsed with garbage = %q, want [fire]", got)
	}
}

func TestCustomOptions(t *testing.T) {
	// no history, no pins: just the canned list
	if got := customOptions(nil, nil); !slices.Equal(got, cannedTexts) {
		t.Fatalf("customOptions(nil, nil) = %q, want canned list", got)
	}

	// history first, then pins, canned after, no duplicates anywhere
	got := customOptions([]string{"Dog walk", cannedTexts[1]}, []string{"Gym", "Dog walk"})
	if got[0] != "Dog walk" || got[1] != cannedTexts[1] || got[2] != "Gym" {
		t.Fatalf("customOptions = %q, want history then pins", got)
	}
	seen := map[string]bool{}
	for _, o := range got {
		if seen[o] {
			t.Fatalf("customOptions has duplicate %q", o)
		}
		seen[o] = true
	}
	// 2 history + 1 new pin ("Dog walk" deduped) + canned minus the one in history
	if want := 3 + len(cannedTexts) - 1; len(got) != want {
		t.Fatalf("customOptions has %d entries, want %d", len(got), want)
	}
}
