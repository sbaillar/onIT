package emoji

import (
	"bytes"
	"embed"
	"fmt"
	"image"
	"image/draw"
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

var allOnce = sync.OnceValue(func() []Entry {
	var out []Entry
	seen := map[string]bool{}
	for _, g := range gomoji.AllEmojis() {
		file := notoFile(g.CodePoint)
		if seen[g.Slug] {
			continue
		}
		if _, err := allFiles.Open("allimg/" + file); err != nil {
			continue // no Noto artwork for this one
		}
		seen[g.Slug] = true
		out = append(out, Entry{
			Slug: g.Slug,
			Name: strings.ToLower(g.UnicodeName),
			file: file,
		})
	}
	return out
})

// All lists every emoji with artwork, in gomoji's group order.
func All() []Entry { return allOnce() }
