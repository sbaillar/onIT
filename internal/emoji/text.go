package emoji

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
	"strings"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

// The display is a circle: text must fit inside the usable radius, and each
// line only gets the chord width available at its vertical position.
const usableRadius = float64(Size)/2 - 3

var textFont, _ = opentype.Parse(gobold.TTF)

func newFace(size float64) font.Face {
	f, _ := opentype.NewFace(textFont, &opentype.FaceOptions{
		Size: size, DPI: 72, Hinting: font.HintingFull,
	})
	return f
}

// chordWidth is the horizontal space available to a line whose vertical band
// is [yTop, yBot] on a circle of radius usableRadius centered at Size/2.
func chordWidth(yTop, yBot float64) float64 {
	c := float64(Size) / 2
	d := math.Max(math.Abs(yTop-c), math.Abs(yBot-c))
	if d >= usableRadius {
		return 0
	}
	return 2 * math.Sqrt(usableRadius*usableRadius-d*d)
}

// layout wraps words into at most n vertically-centered lines at the given
// face, honoring each line's chord width. ok is false if they don't fit.
func layout(words []string, fc font.Face, n int) (lines []string, ok bool) {
	m := fc.Metrics()
	lineH := float64(m.Ascent+m.Descent) / 64
	top := float64(Size)/2 - lineH*float64(n)/2
	w := 0
	for i := 0; i < n && w < len(words); i++ {
		maxW := chordWidth(top+lineH*float64(i), top+lineH*float64(i+1))
		line := ""
		for w < len(words) {
			cand := words[w]
			if line != "" {
				cand = line + " " + words[w]
			}
			if float64(font.MeasureString(fc, cand))/64 > maxW {
				break
			}
			line = cand
			w++
		}
		if line == "" {
			return nil, false // a single word exceeds this line's width
		}
		lines = append(lines, line)
	}
	return lines, w == len(words)
}

// fitText finds the biggest font size at which text fits the circle,
// word-wrapping as needed, and returns the wrapped lines.
func fitText(text string) (float64, []string, error) {
	words := strings.Fields(text)
	if len(words) == 0 {
		return 0, nil, errors.New("no text")
	}
	for size := 64.0; size >= 7; size-- {
		fc := newFace(size)
		m := fc.Metrics()
		lineH := float64(m.Ascent+m.Descent) / 64
		maxLines := int(2 * usableRadius / lineH)
		for n := 1; n <= maxLines; n++ {
			if lines, ok := layout(words, fc, n); ok {
				return size, lines, nil
			}
		}
	}
	return 0, nil, errors.New("text does not fit the display")
}

// TextPayload renders text as white-on-black at the biggest size that fits
// the round display, returning the EMOJI: wire payload and a PNG of the
// same image for the on-screen face mirror.
func TextPayload(text string) (b64 string, pngBytes []byte, err error) {
	size, lines, err := fitText(text)
	if err != nil {
		return "", nil, err
	}
	img := image.NewRGBA(image.Rect(0, 0, Size, Size))
	draw.Draw(img, img.Bounds(), image.Black, image.Point{}, draw.Src)

	fc := newFace(size)
	m := fc.Metrics()
	lineH := float64(m.Ascent+m.Descent) / 64
	top := float64(Size)/2 - lineH*float64(len(lines))/2
	for i, line := range lines {
		width := float64(font.MeasureString(fc, line)) / 64
		d := font.Drawer{
			Dst:  img,
			Src:  image.NewUniform(color.White),
			Face: fc,
			Dot: fixed.Point26_6{
				X: fixed.Int26_6((float64(Size) - width) / 2 * 64),
				Y: fixed.Int26_6((top+lineH*float64(i))*64) + m.Ascent,
			},
		}
		d.DrawString(line)
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return "", nil, err
	}
	return rgb565Base64(img), buf.Bytes(), nil
}
