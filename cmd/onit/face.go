package main

import (
	"image/color"
	"math"
	"strings"
	"time"

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
	faceGrayRing = color.NRGBA{0x40, 0x40, 0x40, 0xFF} // C_GRAY_RING
	faceGreen    = color.NRGBA{0x90, 0xC4, 0x50, 0xFF} // C_GREEN
	faceRedSec   = color.NRGBA{0xFF, 0x00, 0x00, 0xFF} // the white face's second hand
)

// clockTheme picks the face the device is drawing: 0 dark with ticks,
// 1 white with numerals. Mirrored here so the window shows the same clock.
var clockThemeShown = 0

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

	// the standalone clock, mirroring the device's own face
	ticks  []*canvas.Line
	nums   [4]*canvas.Text // 12/3/6/9 on the white face
	hourH  *canvas.Line
	minH   *canvas.Line
	secH   *canvas.Line
	hub    *canvas.Circle
	hubDot *canvas.Circle
}

func newDeviceFace() *deviceFace {
	f := &deviceFace{
		disc:  canvas.NewCircle(faceBgIdle),
		dash:  canvas.NewImageFromResource(dashRing),
		dot:   canvas.NewCircle(faceWhite), // on the full-green available screen
		mic:   canvas.NewImageFromResource(micIcon),
		share: canvas.NewImageFromResource(shareIcon),
		emoji: &canvas.Image{FillMode: canvas.ImageFillContain},
		hourH: canvas.NewLine(faceWhite),
		minH:  canvas.NewLine(faceLavender),
		secH:  canvas.NewLine(faceGreen),
		hub:   canvas.NewCircle(faceLavender),
	}
	f.hubDot = canvas.NewCircle(faceBgIdle)
	f.hourH.StrokeWidth, f.minH.StrokeWidth, f.secH.StrokeWidth = fs(7), fs(5), fs(3)
	for i := 0; i < 60; i++ {
		l := canvas.NewLine(faceGrayRing)
		l.StrokeWidth = fs(1)
		f.ticks = append(f.ticks, l)
	}
	for i := range f.nums {
		f.nums[i] = canvas.NewText("", faceBlack)
		f.nums[i].TextStyle = fyne.TextStyle{Bold: true}
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

	place(f.hub, 120, 120, 2*fs(6))
	place(f.hubDot, 120, 120, 2*fs(3))

	inner := container.NewWithoutLayout(f.disc, f.emoji, f.dash,
		f.dot, f.mic, f.share)
	for _, t := range f.ticks {
		inner.Add(t)
	}
	for _, n := range f.nums {
		inner.Add(n)
	}
	inner.Add(f.hourH)
	inner.Add(f.minH)
	inner.Add(f.secH)
	inner.Add(f.hub)
	inner.Add(f.hubDot)
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
	for _, o := range []fyne.CanvasObject{f.dash, f.dot, f.mic, f.share, f.emoji,
		f.hourH, f.minH, f.secH, f.hub, f.hubDot} {
		o.Hide()
	}
	for _, t := range f.ticks {
		t.Hide()
	}
	for _, n := range f.nums {
		n.Hide()
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
	default: // off: the standalone clock, the same face the device draws
		f.setClock(time.Now())
	}
}

// setClock draws the device's standalone clock. Both faces are mirrored so
// the window shows what the device shows rather than a placeholder.
func (f *deviceFace) setClock(now time.Time) {
	white := clockThemeShown == 1
	bg := faceBgIdle
	if white {
		bg = faceWhite
	}
	f.fill(bg, bg, 0)

	// hour marks: numerals at 12/3/6/9 on the white face, ticks on the dark
	if white {
		for i, label := range [4]string{"12", "3", "6", "9"} {
			cx, cy := clockPoint(float64(i)*90, 84)
			n := f.nums[i]
			n.Text, n.TextSize, n.Color = label, 15, faceBlack
			m := fyne.MeasureText(n.Text, n.TextSize, n.TextStyle)
			n.Resize(m)
			n.Move(fyne.NewPos(cx-m.Width/2, cy-m.Height/2))
			n.Show()
			n.Refresh()
		}
		for i, t := range f.ticks {
			if i%5 != 0 || i%15 == 0 {
				continue // only the eight hours without a numeral
			}
			f.setLine(t, float64(i)*6, 101, 109, faceBlack, fs(2))
		}
	} else {
		for i, t := range f.ticks {
			if i%5 == 0 {
				f.setLine(t, float64(i)*6, 99, 110, faceLavender, fs(2))
			} else {
				f.setLine(t, float64(i)*6, 105, 110, faceGrayRing, fs(1))
			}
		}
	}

	hourCol, minCol, secCol, hubCol := faceWhite, faceLavender, faceGreen, faceLavender
	if white {
		hourCol, minCol, secCol, hubCol = faceBlack, faceBlack, faceRedSec, faceBlack
	}
	h, m, sec := now.Hour()%12, now.Minute(), now.Second()
	f.setHand(f.hourH, float64(h)*30+float64(m)*0.5, 54, hourCol)
	f.setHand(f.minH, float64(m)*6+float64(sec)*0.1, 77, minCol)
	f.setHand(f.secH, float64(sec)*6, 91, secCol)
	f.hub.FillColor, f.hubDot.FillColor = hubCol, bg
	f.hub.Show()
	f.hubDot.Show()
	f.hub.Refresh()
	f.hubDot.Refresh()
}

// clockPoint is the screen position of a point at angle deg (0 = 12 o'clock,
// clockwise) and radius r, in the firmware's 240px coordinates.
func clockPoint(deg, r float64) (x, y float32) {
	rad := deg * math.Pi / 180
	return fs(120) + fs(float32(math.Sin(rad)*r)), fs(120) - fs(float32(math.Cos(rad)*r))
}

// setLine places a radial line between two radii, for the hour marks.
func (f *deviceFace) setLine(l *canvas.Line, deg, r1, r2 float64, c color.Color, w float32) {
	x1, y1 := clockPoint(deg, r1)
	x2, y2 := clockPoint(deg, r2)
	l.Position1, l.Position2 = fyne.NewPos(x1, y1), fyne.NewPos(x2, y2)
	l.StrokeColor, l.StrokeWidth = c, w
	l.Show()
	l.Refresh()
}

// setHand draws a hand from the hub out to length (firmware coordinates),
// with the same short tail behind the pivot the device draws.
func (f *deviceFace) setHand(l *canvas.Line, deg, length float64, c color.Color) {
	x1, y1 := clockPoint(deg+180, 12)
	x2, y2 := clockPoint(deg, length)
	l.Position1, l.Position2 = fyne.NewPos(x1, y1), fyne.NewPos(x2, y2)
	l.StrokeColor = c
	l.Show()
	l.Refresh()
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
