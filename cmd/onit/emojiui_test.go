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

func TestTopEmojiSlugs(t *testing.T) {
	// no usage yet: starters fill the list
	got := topEmojiSlugs(nil, 5)
	if len(got) != 5 || got[0] != starterEmojis[0] {
		t.Fatalf("topEmojiSlugs(nil, 5) = %q, want first starters", got)
	}

	// used emojis lead, starters fill the rest without duplicates
	usage := []string{"pizza 7", "fire 3"}
	got = topEmojiSlugs(usage, 5)
	if got[0] != "pizza" || got[1] != "fire" {
		t.Fatalf("topEmojiSlugs = %q, want usage first", got)
	}
	if len(got) != 5 {
		t.Fatalf("topEmojiSlugs = %d entries, want 5", len(got))
	}
	seen := map[string]bool{}
	for _, s := range got {
		if seen[s] {
			t.Fatalf("duplicate %q in %q", s, got)
		}
		seen[s] = true
	}
}

func TestCustomOptions(t *testing.T) {
	// no history, no pins: just the canned list
	if got := customOptions(nil, nil, nil); !slices.Equal(got, cannedTexts) {
		t.Fatalf("customOptions(nil, nil, nil) = %q, want canned list", got)
	}

	// history first, then pins, canned after, no duplicates anywhere
	got := customOptions([]string{"Dog walk", cannedTexts[1]}, []string{"Gym", "Dog walk"}, nil)
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

	// removed canned messages stay hidden
	got = customOptions(nil, nil, []string{cannedTexts[0]})
	if slices.Contains(got, cannedTexts[0]) || len(got) != len(cannedTexts)-1 {
		t.Fatalf("customOptions with removed = %q, want %q hidden", got, cannedTexts[0])
	}
}

func TestRemoveMessage(t *testing.T) {
	history := []string{"Dog walk", cannedTexts[0]}
	pinned := []string{"Gym", "Dog walk"}
	var removed []string

	// a history+pinned message disappears from both
	history, pinned, removed = removeMessage(history, pinned, removed, "Dog walk")
	if slices.Contains(history, "Dog walk") || slices.Contains(pinned, "Dog walk") {
		t.Fatalf("Dog walk still present: history=%q pinned=%q", history, pinned)
	}
	if len(removed) != 0 {
		t.Fatalf("removed = %q, want empty (not a canned message)", removed)
	}

	// a canned message is suppressed via the removed list (and dropped from
	// history it may appear in)
	history, pinned, removed = removeMessage(history, pinned, removed, cannedTexts[0])
	if !slices.Contains(removed, cannedTexts[0]) {
		t.Fatalf("removed = %q, want %q in it", removed, cannedTexts[0])
	}
	if slices.Contains(history, cannedTexts[0]) {
		t.Fatalf("history still has %q", cannedTexts[0])
	}
	if got := customOptions(history, pinned, removed); slices.Contains(got, cannedTexts[0]) {
		t.Fatalf("options still show removed canned: %q", got)
	}

	// removing twice stays stable
	_, _, removed2 := removeMessage(history, pinned, removed, cannedTexts[0])
	if len(removed2) != len(removed) {
		t.Fatalf("double remove grew the list: %q", removed2)
	}
}
