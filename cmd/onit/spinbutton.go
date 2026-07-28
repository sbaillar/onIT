package main

import (
	"fmt"
	"image/color"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// spinFrames is how many rotations of the arrows exist; the icon steps
// through them as the device reports frames, so the button turns in time
// with the wheel rather than on a timer of its own.
const spinFrames = 12

var spinIcons [spinFrames]fyne.Resource

func init() {
	// two arrowheads chasing each other round a broken ring, rotated per frame
	for i := range spinIcons {
		deg := i * 360 / spinFrames
		spinIcons[i] = fyne.NewStaticResource(fmt.Sprintf("spin-%d.svg", i), fmt.Appendf(nil,
			`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 44 44">`+
				`<g transform="rotate(%d 22 22)" fill="none" stroke="#8A8A8A" stroke-width="3.4" stroke-linecap="round">`+
				`<path d="M22 7 A15 15 0 0 1 35.0 29.5"/>`+
				`<path d="M22 37 A15 15 0 0 1 9.0 14.5"/>`+
				`</g>`+
				`<g transform="rotate(%d 22 22)" fill="#8A8A8A">`+
				`<path d="M35.0 29.5 l5.0 -2.0 l-1.5 5.6 z"/>`+
				`<path d="M9.0 14.5 l-5.0 2.0 l1.5 -5.6 z"/>`+
				`</g></svg>`, deg, deg))
	}
}

// spinButton is the arrows-over-SPIN control next to the device face. It is
// its own widget rather than a widget.Button because the icon sits above the
// label and has to keep turning while the wheel does.
type spinButton struct {
	widget.BaseWidget
	onTap func()

	icon  *canvas.Image
	label *canvas.Text
	bg    *canvas.Rectangle
	frame int
}

func newSpinButton(onTap func()) *spinButton {
	b := &spinButton{onTap: onTap}
	b.icon = canvas.NewImageFromResource(spinIcons[0])
	b.icon.FillMode = canvas.ImageFillContain
	b.icon.SetMinSize(fyne.NewSize(26, 26))
	b.label = canvas.NewText("SPIN", theme.Color(theme.ColorNameForeground))
	b.label.TextSize = 10
	b.label.TextStyle = fyne.TextStyle{Bold: true}
	b.label.Alignment = fyne.TextAlignCenter
	b.bg = canvas.NewRectangle(color.Transparent)
	b.bg.CornerRadius = 8
	b.ExtendBaseWidget(b)
	return b
}

func (b *spinButton) CreateRenderer() fyne.WidgetRenderer {
	body := container.NewVBox(container.NewCenter(b.icon), b.label)
	return widget.NewSimpleRenderer(container.NewStack(b.bg, container.NewPadded(body)))
}

// Tapped flashes the button the way a pressed control does — Fyne gives that
// for free on widget.Button, but not on a custom one — then fires.
func (b *spinButton) Tapped(*fyne.PointEvent) {
	b.press()
	if b.onTap != nil {
		b.onTap()
	}
}

func (b *spinButton) press() {
	b.bg.FillColor = theme.Color(theme.ColorNamePressed)
	b.bg.Refresh()
	time.AfterFunc(140*time.Millisecond, func() {
		fyne.Do(func() {
			b.bg.FillColor = color.Transparent
			b.bg.Refresh()
		})
	})
}

// advance turns the arrows one step. Called per frame the device reports, so
// the button and the wheel slow down together.
func (b *spinButton) advance() {
	b.frame = (b.frame + 1) % spinFrames
	b.icon.Resource = spinIcons[b.frame]
	b.icon.Refresh()
}
