package main

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"

	"onit/internal/firmware"
)

// wordmark renders the logo: the ring-and-dot "o" followed by "nIT".
func wordmark() fyne.CanvasObject {
	green := stateColors["available"]
	ring := canvas.NewCircle(color.NRGBA{0x0D, 0x0D, 0x16, 0xFF})
	ring.StrokeColor = green
	ring.StrokeWidth = 4
	dot := canvas.NewCircle(green)
	o := container.NewStack(ring,
		container.NewCenter(container.NewGridWrap(fyne.NewSize(11, 11), dot)))
	name := canvas.NewText("nIT", color.NRGBA{0xFF, 0xFF, 0xFF, 0xFF})
	name.TextSize = 30
	name.TextStyle = fyne.TextStyle{Bold: true}
	return container.NewHBox(
		container.NewGridWrap(fyne.NewSize(32, 42), container.NewPadded(o)),
		name,
	)
}

func showAbout(a fyne.App) {
	w := a.NewWindow("About onIT")

	version := widget.NewLabel("Version " + appVersion + " (firmware " + firmware.Version + " embedded)")
	version.Alignment = fyne.TextAlignCenter
	desc := widget.NewLabel("A physical Teams busylight: shows your presence on a\nround LCD on your desk, in sync with Microsoft Teams -\nor set it yourself.")
	desc.Alignment = fyne.TextAlignCenter
	byline := widget.NewLabel("By Sonny Baillargeon")
	byline.Alignment = fyne.TextAlignCenter
	copyright := widget.NewLabel("(c) 2026 Sonny Baillargeon. MIT License.")
	copyright.Alignment = fyne.TextAlignCenter
	copyright.Importance = widget.LowImportance
	closeBtn := widget.NewButton("Close", w.Close)

	w.SetContent(container.NewPadded(container.NewVBox(
		container.NewCenter(wordmark()),
		version, desc, byline, copyright,
		closeBtn,
	)))
	w.Show()
}
