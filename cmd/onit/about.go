package main

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"

	"onit/internal/firmware"
)

func showAbout(w fyne.Window) {
	// wordmark: the green presence dot is the "o" in onIT
	dot := canvas.NewCircle(stateColors["available"])
	name := canvas.NewText("nIT", color.NRGBA{0xFF, 0xFF, 0xFF, 0xFF})
	name.TextSize = 28
	name.TextStyle = fyne.TextStyle{Bold: true}
	mark := container.NewHBox(
		container.NewGridWrap(fyne.NewSize(26, 40), container.NewPadded(dot)),
		name,
	)

	version := widget.NewLabel("Version " + appVersion + "  (firmware " + firmware.Version + " embedded)")
	desc := widget.NewLabel("A physical Teams busylight: shows your presence\non a round LCD on your desk, in sync with Microsoft\nTeams - or set it yourself.")
	byline := widget.NewLabel("By Sonny Baillargeon")
	copyright := widget.NewLabel("(c) 2026 Sonny Baillargeon. MIT License.")
	copyright.Importance = widget.LowImportance

	dialog.ShowCustom("About", "Close",
		container.NewVBox(container.NewCenter(mark), version, desc, byline, copyright), w)
}
