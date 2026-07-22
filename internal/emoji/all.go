package emoji

import (
	"bytes"
	"embed"
	"fmt"
	"image"
	"image/draw"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/forPelevin/gomoji"
	xdraw "golang.org/x/image/draw"
)

// The full standard emoji set: Google's Noto emoji art (Apache-2.0, see
// allimg/LICENSE), one 128px PNG per emoji, matched to names via gomoji.

//go:embed allimg/*.png
var allFiles embed.FS

// Entry is one emoji in the full set.
type Entry struct {
	Slug string // stable id, e.g. "thumbs-up"
	Name string // searchable name, e.g. "thumbs up"
	file string
}

// PNG returns the 128px artwork, nil if missing.
func (e Entry) PNG() []byte {
	b, err := allFiles.ReadFile("allimg/" + e.file)
	if err != nil {
		return nil
	}
	return b
}

// Payload returns the EMOJI: wire payload, resized to Size x Size on black.
func (e Entry) Payload() (string, error) {
	b := e.PNG()
	if b == nil {
		return "", fmt.Errorf("no artwork for %s", e.Slug)
	}
	src, _, err := image.Decode(bytes.NewReader(b))
	if err != nil {
		return "", err
	}
	dst := image.NewRGBA(image.Rect(0, 0, Size, Size))
	draw.Draw(dst, dst.Bounds(), image.Black, image.Point{}, draw.Src)
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), src, src.Bounds(), xdraw.Over, nil)
	return rgb565Base64(dst), nil
}

// notoFile converts a gomoji codepoint list ("1F468 200D 1F469") to the
// Noto asset filename. Noto omits the FE0F variation selector.
func notoFile(codepoint string) string {
	var parts []string
	for _, cp := range strings.Fields(codepoint) {
		if cp == "FE0F" {
			continue
		}
		parts = append(parts, strings.ToLower(cp))
	}
	return "emoji_u" + strings.Join(parts, "_") + ".png"
}

// groupRank orders categories like the iPhone keyboard: smileys & people,
// animals, food, activities, travel, objects, symbols, flags.
var groupRank = map[string]int{
	"Smileys & Emotion": 0, "People & Body": 1, "Animals & Nature": 2,
	"Food & Drink": 3, "Activities": 4, "Travel & Places": 5,
	"Objects": 6, "Symbols": 7, "Flags": 8,
}

// subgroupRank orders subgroups within each category per CLDR's
// emoji-test.txt — the sequence every keyboard follows. Unknown subgroups
// sink to the end of their category.
var subgroupRank = func() map[string]int {
	order := []string{
		// Smileys & Emotion
		"face-smiling", "face-affection", "face-tongue", "face-hand",
		"face-neutral-skeptical", "face-sleepy", "face-unwell", "face-hat",
		"face-glasses", "face-concerned", "face-negative", "face-costume",
		"cat-face", "monkey-face", "heart", "emotion",
		// People & Body
		"hand-fingers-open", "hand-fingers-partial", "hand-single-finger",
		"hand-fingers-closed", "hands", "hand-prop", "body-parts", "person",
		"person-gesture", "person-role", "person-fantasy", "person-activity",
		"person-sport", "person-resting", "family", "person-symbol",
		// Animals & Nature
		"animal-mammal", "animal-bird", "animal-amphibian", "animal-reptile",
		"animal-marine", "animal-bug", "plant-flower", "plant-other",
		// Food & Drink
		"food-fruit", "food-vegetable", "food-prepared", "food-asian",
		"food-marine", "food-sweet", "drink", "dishware",
		// Travel & Places
		"place-map", "place-geographic", "place-building", "place-religious",
		"place-other", "transport-ground", "transport-water", "transport-air",
		"hotel", "time", "sky & weather",
		// Activities
		"event", "award-medal", "sport", "game", "arts & crafts",
		// Objects
		"clothing", "sound", "music", "musical-instrument", "phone",
		"computer", "light & video", "book-paper", "money", "mail",
		"writing", "office", "lock", "tool", "science", "medical",
		"household", "other-object",
		// Symbols
		"transport-sign", "warning", "arrow", "religion", "zodiac",
		"av-symbol", "gender", "math", "punctuation", "currency",
		"other-symbol", "keycap", "alphanum", "geometric",
		// Flags
		"flag", "country-flag", "subdivision-flag",
	}
	m := make(map[string]int, len(order))
	for i, s := range order {
		m[s] = i
	}
	return m
}()

// codepointKey turns "1F600" / "1F468 200D ..." into a sortable value of
// its leading codepoint (CLDR orders roughly by codepoint within subgroups).
func codepointKey(cp string) uint64 {
	first, _, _ := strings.Cut(cp, " ")
	v, err := strconv.ParseUint(first, 16, 64)
	if err != nil {
		return 1 << 62
	}
	return v
}

// order.txt is Unicode's emoji-test.txt sequence (the exact keyboard
// order), one normalized codepoint key per line.
//
//go:embed order.txt
var orderTxt string

// cldrIndex maps a normalized codepoint key ("1f468_200d_1f469") to its
// position in the official keyboard order.
var cldrIndex = func() map[string]int {
	m := map[string]int{}
	for i, line := range strings.Split(strings.TrimSpace(orderTxt), "\n") {
		m[strings.TrimSpace(line)] = i
	}
	return m
}()

var allOnce = sync.OnceValue(func() []Entry {
	type ranked struct {
		Entry
		group, cldr, sub int
		cp               uint64
	}
	var out []ranked
	seen := map[string]bool{}
	for _, g := range gomoji.AllEmojis() {
		rank, ok := groupRank[g.Group]
		if !ok {
			continue // skin-tone components etc.
		}
		file := notoFile(g.CodePoint)
		if seen[g.Slug] {
			continue
		}
		if _, err := allFiles.Open("allimg/" + file); err != nil {
			continue // no Noto artwork for this one
		}
		seen[g.Slug] = true
		sub, ok := subgroupRank[g.SubGroup]
		if !ok {
			sub = len(subgroupRank) // unknown subgroup: end of its category
		}
		key := strings.TrimSuffix(strings.TrimPrefix(file, "emoji_u"), ".png")
		cldr, ok := cldrIndex[key]
		if !ok {
			cldr = len(cldrIndex) // not in emoji-test.txt: after the known ones
		}
		out = append(out, ranked{Entry{
			Slug: g.Slug,
			Name: strings.ToLower(g.UnicodeName),
			file: file,
		}, rank, cldr, sub, codepointKey(g.CodePoint)})
	}
	// iPhone category order first, then the exact CLDR keyboard sequence;
	// subgroup/codepoint only break ties for emojis missing from order.txt
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.group != b.group {
			return a.group < b.group
		}
		if a.cldr != b.cldr {
			return a.cldr < b.cldr
		}
		if a.sub != b.sub {
			return a.sub < b.sub
		}
		return a.cp < b.cp
	})
	entries := make([]Entry, len(out))
	for i, r := range out {
		entries[i] = r.Entry
	}
	return entries
})

// All lists every emoji with artwork, in iPhone keyboard category order.
func All() []Entry { return allOnce() }
