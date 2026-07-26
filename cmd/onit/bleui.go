package main

import (
	"context"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"onit/internal/busylight"
)

// showBLEPair scans for busylights advertising the onIT service and pairs
// with the one the user picks. The scan offers one candidate at a time
// (choose blocks scanning while the user decides): the name button pairs,
// the X skips it and resumes scanning. During pairing macOS shows its own
// passkey dialog; the 6-digit code appears on the device screen.
func showBLEPair(a fyne.App, agent *busylight.Agent, w fyne.Window) {
	ctx, cancel := context.WithCancel(context.Background())
	status := widget.NewLabel("Scanning for busylights...")
	rows := container.NewVBox()
	d := dialog.NewCustom("Pair busylight", "Cancel", container.NewVBox(status, rows), w)
	d.SetOnClosed(cancel)
	d.Resize(fyne.NewSize(280, 0))
	d.Show()
	go func() {
		err := agent.PairBLE(ctx, func(dev busylight.BLEDevice) bool {
			name := dev.Name
			if name == "" {
				name = dev.ID
			}
			// The sends are non-blocking: once a device is chosen nothing
			// receives on pick anymore, and extra clicks on any row must
			// never block the Fyne main thread.
			pick := make(chan bool, 1)
			offer := func(ok bool) {
				select {
				case pick <- ok:
				default:
				}
			}
			var row *fyne.Container
			fyne.Do(func() {
				pair := widget.NewButton(name, func() { offer(true) })
				pair.Alignment = widget.ButtonAlignLeading
				skip := widget.NewButtonWithIcon("", theme.ContentClearIcon(), func() { offer(false) })
				skip.Importance = widget.LowImportance
				row = container.NewBorder(nil, nil, nil, skip, pair)
				rows.Add(row)
				status.SetText("Tap a device to pair - the passkey shows on its screen")
			})
			select {
			case ok := <-pick:
				if ok {
					fyne.Do(func() { status.SetText("Pairing with " + name + "...") })
				} else {
					fyne.Do(func() {
						rows.Remove(row)
						status.SetText("Scanning for busylights...")
					})
				}
				return ok
			case <-ctx.Done():
				return false
			}
		})
		fyne.Do(func() {
			d.Hide()
			if err != nil && ctx.Err() == nil {
				dialog.ShowError(err, w)
			}
		})
	}()
}
