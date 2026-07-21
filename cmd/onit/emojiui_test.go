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
