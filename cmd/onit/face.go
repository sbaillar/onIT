package main

import (
	"image/color"
	"math"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
)

// The round face replicates the firmware's 240x240 screen layouts
// (drawAvailable & co. in busylight_round.ino) at faceSize pixels,
// so the window shows exactly what the device shows.
const faceSize = 190

// fs scales a firmware screen coordinate (240px) to face pixels.
func fs(v float32) float32 { return v * faceSize / 240 }

var (
	faceWhite    = color.NRGBA{0xFF, 0xFF, 0xFF, 0xFF}
	faceBlack    = color.NRGBA{0x00, 0x00, 0x00, 0xFF}
	faceBgIdle   = color.NRGBA{0x10, 0x10, 0x18, 0xFF} // C_BG_IDLE
	faceLavender = color.NRGBA{0xD8, 0xD8, 0xF0, 0xFF} // C_LAVENDER
	faceGrayText = color.NRGBA{0x58, 0x58, 0x58, 0xFF} // C_GRAY_TEXT
)

// Icons traced from the firmware's iconMic/iconShare (24x24 grid, scale 2).
var micIcon = fyne.NewStaticResource("mic.svg", []byte(
	`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 48 48">`+
		`<rect x="18" y="6" width="12" height="22" rx="6" fill="#FFFFFF"/>`+
		`<path d="M12 22 A12 12 0 0 0 36 22" stroke="#FFFFFF" stroke-width="2" fill="none"/>`+
		`<rect x="23" y="34" width="3" height="8" fill="#FFFFFF"/>`+
		`</svg>`))

var shareIcon = fyne.NewStaticResource("share.svg", []byte(
	`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 46 46">`+
		`<rect x="4" y="7.6" width="38" height="24.7" rx="2" stroke="#FFFFFF" stroke-width="2" fill="none"/>`+
		`<path d="M23 24.7 L23 17.1 M23 17.1 L18.2 21.9 M23 17.1 L27.8 21.9" stroke="#FFFFFF" stroke-width="3" fill="none"/>`+
		`<rect x="15.2" y="38" width="15.2" height="2" fill="#FFFFFF"/>`+
		`</svg>`))

// 48 dashes of 3.5deg, like ringDashed(114, 3, C_GRAY_RING, 48, 3.5).
var dashRing = fyne.NewStaticResource("dashring.svg", []byte(
	`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 240 240">`+
		`<circle cx="120" cy="120" r="112.5" stroke="#404040" stroke-width="3" fill="none" stroke-dasharray="6.87 7.85"/>`+
		`</svg>`))

type deviceFace struct {
	root  *fyne.Container
	disc  *canvas.Circle // fill + solid ring
	dash  *canvas.Image  // dotted ring (off)
	dot   *canvas.Circle // presence dot (available)
	mic   *canvas.Image
	share *canvas.Image
	emoji *canvas.Image
	lines [5]*canvas.Text // lines[0]/[1] double as the state captions
}

func newDeviceFace() *deviceFace {
	f := &deviceFace{
		disc:  canvas.NewCircle(faceBgIdle),
		dash:  canvas.NewImageFromResource(dashRing),
		dot:   canvas.NewCircle(faceWhite), // on the full-green available screen
		mic:   canvas.NewImageFromResource(micIcon),
		share: canvas.NewImageFromResource(shareIcon),
		emoji: &canvas.Image{FillMode: canvas.ImageFillContain},
	}
	for i := range f.lines {
		f.lines[i] = canvas.NewText("", faceWhite)
		f.lines[i].TextStyle = fyne.TextStyle{Bold: true}
	}

	f.disc.Resize(fyne.NewSize(faceSize, faceSize))
	f.dash.Resize(fyne.NewSize(faceSize, faceSize))
	f.emoji.Resize(fyne.NewSize(faceSize, faceSize))
	place := func(o fyne.CanvasObject, cx, cy, size float32) {
		o.Resize(fyne.NewSize(size, size))
		o.Move(fyne.NewPos(fs(cx)-size/2, fs(cy)-size/2))
	}
	place(f.dot, 120, 92, 2*fs(11))
	place(f.mic, 120, 80, fs(48))
	place(f.share, 120, 74, fs(46))

	inner := container.NewWithoutLayout(f.disc, f.emoji, f.dash,
		f.dot, f.mic, f.share)
	for _, l := range f.lines {
		inner.Add(l)
	}
	f.root = container.NewGridWrap(fyne.NewSize(faceSize, faceSize), inner)
	return f
}

func (f *deviceFace) fill(bg, ring color.Color, ringW float32) {
	f.disc.FillColor = bg
	f.disc.StrokeColor = ring
	f.disc.StrokeWidth = ringW
	f.disc.Refresh()
}

// setText centers s at screen-y cy, like the firmware's textCentered.
func (f *deviceFace) setText(t *canvas.Text, s string, size float32, c color.Color, cy float32) {
	t.Text, t.TextSize, t.Color = s, size, c
	m := fyne.MeasureText(s, size, t.TextStyle)
	t.Resize(m)
	t.Move(fyne.NewPos(faceSize/2-m.Width/2, fs(cy)-m.Height/2))
	t.Show()
	t.Refresh()
}

// Set renders the screen the firmware draws for shown; emojiRes is the
// emoji or text image last sent to the device (the wire payload has no name).
func (f *deviceFace) Set(shown string, emojiRes fyne.Resource) {
	for _, o := range []fyne.CanvasObject{f.dash, f.dot, f.mic, f.share, f.emoji} {
		o.Hide()
	}
	for _, l := range f.lines {
		l.Hide()
	}
	switch stateKey(shown) {
	case "available": // full-screen green, white ring and dot
		f.fill(stateColors["available"], faceWhite, fs(4))
		f.dot.Show()
		f.setText(f.lines[0], "Available", 19, faceWhite, 136)
	case "meeting": // red, mic
		f.fill(stateColors["meeting"], faceWhite, fs(7))
		f.mic.Show()
		f.setText(f.lines[0], "In a call", 19, faceWhite, 146)
	case "sharing": // purple, monitor
		f.fill(stateColors["sharing"], faceWhite, fs(8))
		f.share.Show()
		f.setText(f.lines[0], "Presenting", 19, faceWhite, 134)
		f.setText(f.lines[1], "Do not disturb", 10, faceLavender, 164)
	case "custom": // user-colored (default yellow), auto-fitted message
		bg, fg, text := splitCustom(strings.TrimPrefix(shown, "custom:"))
		f.fill(hexColor(bg), hexColor(fg), fs(5))
		f.setCustom(text, hexColor(fg))
	case "emoji":
		f.fill(faceBgIdle, faceBgIdle, 0)
		if emojiRes != nil {
			f.emoji.Resource = emojiRes
			f.emoji.Show()
			f.emoji.Refresh()
		} else {
			f.setText(f.lines[0], "?", 19, faceGrayText, 130)
		}
	default: // off: black, dotted ring
		f.fill(faceBlack, faceBlack, 0)
		f.dash.Show()
		f.setText(f.lines[0], "- -", 13, faceGrayText, 124)
	}
}

// The custom screen's usable radius in firmware coordinates (240px face,
// ring at 114 with 5px stroke, a little padding).
const customRadius = 100

// customChord is the width available to a text band [yTop, yBot].
func customChord(yTop, yBot float32) float32 {
	d := max(yTop-120, 120-yTop, yBot-120, 120-yBot)
	if d >= customRadius {
		return 0
	}
	return 2 * float32(math.Sqrt(float64(customRadius*customRadius-d*d)))
}

// customLayout wraps words into at most n vertically-centered lines,
// honoring each line's chord width. ok is false if they don't fit.
func customLayout(words []string, size, lineH float32, n int) (lines []string, ok bool) {
	style := fyne.TextStyle{Bold: true}
	top := 120 - lineH*float32(n)/2
	w := 0
	for i := 0; i < n && w < len(words); i++ {
		maxW := customChord(top+lineH*float32(i), top+lineH*float32(i+1))
		line := ""
		for w < len(words) {
			cand := words[w]
			if line != "" {
				cand = line + " " + words[w]
			}
			if fyne.MeasureText(cand, size, style).Width*240/faceSize > maxW {
				break
			}
			line = cand
			w++
		}
		if line == "" {
			return nil, false
		}
		lines = append(lines, line)
	}
	return lines, w == len(words)
}

// setCustom auto-fits the message like the firmware's drawCustom: the
// biggest of three sizes that fits the circle, word-wrapped to the chord
// width at each line.
func (f *deviceFace) setCustom(msg string, fg color.Color) {
	words := strings.Fields(msg)
	style := fyne.TextStyle{Bold: true}
	// mirrors the firmware ladder: pixel-doubled 24/18pt, then 24/18/12/9pt
	for _, size := range []float32{51, 38, 25, 19, 14, 10} {
		lineH := fyne.MeasureText("Agy", size, style).Height * 240 / faceSize * 1.05
		maxLines := min(len(f.lines), int(2*customRadius/lineH))
		for n := 1; n <= maxLines; n++ {
			lines, ok := customLayout(words, size, lineH, n)
			if !ok {
				continue
			}
			top := 120 - lineH*float32(n)/2
			for i, l := range lines {
				f.setText(f.lines[i], l, size, fg, top+lineH*(float32(i)+0.5))
			}
			return
		}
	}
	f.setText(f.lines[0], msg, 10, fg, 120) // best effort
}
