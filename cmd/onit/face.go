package main

import (
	"image/color"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"

	"onit/internal/emoji"
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
	`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 48 48">` +
		`<rect x="18" y="6" width="12" height="22" rx="6" fill="#FFFFFF"/>` +
		`<path d="M12 22 A12 12 0 0 0 36 22" stroke="#FFFFFF" stroke-width="2" fill="none"/>` +
		`<rect x="23" y="34" width="3" height="8" fill="#FFFFFF"/>` +
		`</svg>`))

var shareIcon = fyne.NewStaticResource("share.svg", []byte(
	`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 46 46">` +
		`<rect x="4" y="7.6" width="38" height="24.7" rx="2" stroke="#FFFFFF" stroke-width="2" fill="none"/>` +
		`<path d="M23 24.7 L23 17.1 M23 17.1 L18.2 21.9 M23 17.1 L27.8 21.9" stroke="#FFFFFF" stroke-width="3" fill="none"/>` +
		`<rect x="15.2" y="38" width="15.2" height="2" fill="#FFFFFF"/>` +
		`</svg>`))

// 48 dashes of 3.5deg, like ringDashed(114, 3, C_GRAY_RING, 48, 3.5).
var dashRing = fyne.NewStaticResource("dashring.svg", []byte(
	`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 240 240">` +
		`<circle cx="120" cy="120" r="112.5" stroke="#404040" stroke-width="3" fill="none" stroke-dasharray="6.87 7.85"/>` +
		`</svg>`))

type deviceFace struct {
	root  *fyne.Container
	disc  *canvas.Circle // fill + solid ring
	dash  *canvas.Image  // dotted ring (off)
	dot   *canvas.Circle // presence dot (available)
	mic   *canvas.Image
	share *canvas.Image
	emoji *canvas.Image
	line1 *canvas.Text
	line2 *canvas.Text
}

func newDeviceFace() *deviceFace {
	f := &deviceFace{
		disc:  canvas.NewCircle(faceBgIdle),
		dash:  canvas.NewImageFromResource(dashRing),
		dot:   canvas.NewCircle(stateColors["available"]),
		mic:   canvas.NewImageFromResource(micIcon),
		share: canvas.NewImageFromResource(shareIcon),
		emoji: &canvas.Image{FillMode: canvas.ImageFillContain},
		line1: canvas.NewText("", faceWhite),
		line2: canvas.NewText("", faceWhite),
	}
	f.line1.TextStyle = fyne.TextStyle{Bold: true}
	f.line2.TextStyle = fyne.TextStyle{Bold: true}

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
		f.dot, f.mic, f.share, f.line1, f.line2)
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

// Set renders the screen the firmware draws for shown; emojiName is the
// emoji last sent to the device (the wire payload has no name).
func (f *deviceFace) Set(shown, emojiName string) {
	for _, o := range []fyne.CanvasObject{f.dash, f.dot, f.mic, f.share, f.emoji, f.line1, f.line2} {
		o.Hide()
	}
	switch stateKey(shown) {
	case "available": // green ring, presence dot
		f.fill(faceBgIdle, stateColors["available"], fs(4))
		f.dot.Show()
		f.setText(f.line1, "Available", 19, faceWhite, 136)
	case "meeting": // red, mic
		f.fill(stateColors["meeting"], faceWhite, fs(7))
		f.mic.Show()
		f.setText(f.line1, "In a call", 19, faceWhite, 146)
	case "sharing": // purple, monitor
		f.fill(stateColors["sharing"], faceWhite, fs(8))
		f.share.Show()
		f.setText(f.line1, "Presenting", 19, faceWhite, 134)
		f.setText(f.line2, "Do not disturb", 10, faceLavender, 164)
	case "custom": // yellow, auto-fitted message
		f.fill(stateColors["custom"], faceBlack, fs(5))
		f.setCustom(strings.TrimPrefix(shown, "custom:"))
	case "emoji":
		f.fill(faceBgIdle, faceBgIdle, 0)
		if png := emoji.PNG(emojiName); png != nil {
			f.emoji.Resource = fyne.NewStaticResource(emojiName+".png", png)
			f.emoji.Show()
			f.emoji.Refresh()
		} else {
			f.setText(f.line1, "?", 19, faceGrayText, 130)
		}
	default: // off: black, dotted ring
		f.fill(faceBlack, faceBlack, 0)
		f.dash.Show()
		f.setText(f.line1, "- -", 13, faceGrayText, 124)
	}
}

// setCustom auto-fits the message like drawCustom: shrink through three
// sizes, then wrap into two lines at the space nearest the middle.
func (f *deviceFace) setCustom(msg string) {
	sizes := []float32{19, 14, 10}
	maxW := fs(180)
	style := fyne.TextStyle{Bold: true}
	for _, s := range sizes {
		if fyne.MeasureText(msg, s, style).Width <= maxW {
			f.setText(f.line1, msg, s, faceBlack, 120)
			return
		}
	}
	best, mid := -1, len(msg)/2
	for i := range len(msg) {
		if msg[i] == ' ' && (best < 0 || absInt(i-mid) < absInt(best-mid)) {
			best = i
		}
	}
	if best > 0 {
		a, b := msg[:best], msg[best+1:]
		for _, s := range sizes[1:] {
			if fyne.MeasureText(a, s, style).Width <= maxW &&
				fyne.MeasureText(b, s, style).Width <= maxW {
				f.setText(f.line1, a, s, faceBlack, 96)
				f.setText(f.line2, b, s, faceBlack, 144)
				return
			}
		}
	}
	f.setText(f.line1, msg, sizes[2], faceBlack, 120) // best effort
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
