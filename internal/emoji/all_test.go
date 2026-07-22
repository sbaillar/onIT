package emoji

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestNotoFile(t *testing.T) {
	cases := []struct{ codepoint, want string }{
		{"1F600", "emoji_u1f600.png"},
		{"1F44D 1F3FB", "emoji_u1f44d_1f3fb.png"},
		{"2764 FE0F", "emoji_u2764.png"}, // FE0F dropped
		{"1F468 200D 1F469 200D 1F467", "emoji_u1f468_200d_1f469_200d_1f467.png"},
	}
	for _, c := range cases {
		if got := notoFile(c.codepoint); got != c.want {
			t.Errorf("notoFile(%q) = %q, want %q", c.codepoint, got, c.want)
		}
	}
}

func TestAllCoversTheStandardSet(t *testing.T) {
	all := All()
	if len(all) < 3000 {
		t.Fatalf("All() = %d entries, want the full standard set (3000+)", len(all))
	}
	seen := map[string]bool{}
	for _, e := range all {
		if e.Slug == "" || e.Name == "" {
			t.Fatalf("entry with empty slug/name: %+v", e)
		}
		if seen[e.Slug] {
			t.Fatalf("duplicate slug %q", e.Slug)
		}
		seen[e.Slug] = true
		if e.PNG() == nil {
			t.Fatalf("%s: no PNG bytes", e.Slug)
		}
	}
	if !seen["thumbs-up"] {
		t.Error("All() is missing thumbs-up")
	}
}

func TestAllOrderedLikeAnIPhone(t *testing.T) {
	// one representative per category, in the iOS keyboard order:
	// smileys/people, animals, food, activities, travel, objects,
	// symbols, flags
	reps := []string{"grinning-face", "thumbs-up", "dog-face", "pizza",
		"soccer-ball", "rocket", "light-bulb", "check-mark-button", "chequered-flag"}
	idx := map[string]int{}
	for i, e := range All() {
		idx[e.Slug] = i
	}
	last := -1
	for _, s := range reps {
		i, ok := idx[s]
		if !ok {
			t.Fatalf("missing representative emoji %q", s)
		}
		if i <= last {
			t.Errorf("%s out of order (index %d, previous rep at %d)", s, i, last)
		}
		last = i
	}

	// within categories the CLDR keyboard order must hold too
	if All()[0].Slug != "grinning-face" {
		t.Errorf("All()[0] = %s, want grinning-face (the keyboard starts with it)", All()[0].Slug)
	}
	within := [][2]string{
		{"grinning-face", "face-with-tears-of-joy"}, // both face-smiling, by codepoint
		{"face-with-tears-of-joy", "red-heart"},     // smiling faces before hearts
		{"waving-hand", "thumbs-up"},                // open hands before closed
		{"thumbs-up", "folded-hands"},
		{"dog-face", "cat-face"}, // mammals by codepoint
	}
	for _, p := range within {
		if idx[p[0]] == 0 && p[0] != "grinning-face" {
			t.Fatalf("missing %q", p[0])
		}
		if idx[p[0]] >= idx[p[1]] {
			t.Errorf("%s (%d) should come before %s (%d)", p[0], idx[p[0]], p[1], idx[p[1]])
		}
	}
}

func TestEntryPayload(t *testing.T) {
	var thumb Entry
	for _, e := range All() {
		if e.Slug == "thumbs-up" {
			thumb = e
			break
		}
	}
	b64, err := thumb.Payload()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) != Size*Size*2 {
		t.Fatalf("payload = %d bytes, want %d (resized to %dx%d)", len(raw), Size*Size*2, Size, Size)
	}
	if strings.Count(b64, "A") == len(b64) {
		t.Error("payload is all zeroes")
	}
}
