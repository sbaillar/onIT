package main

import (
	"fmt"
	"log"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"

	"onit/internal/busylight"
	"onit/internal/emoji"
)

// showEmojiPicker lets the user send one of the embedded emojis to the
// device (transfer takes ~2s at 115200 baud). onPick reports the chosen
// name so the window face can mirror it.
func showEmojiPicker(a fyne.App, agent *busylight.Agent, setBusy func(bool), onPick func(name string)) {
	w := a.NewWindow("Send an emoji")
	grid := container.NewGridWithColumns(4)
	for _, n := range emoji.Names {
		res := fyne.NewStaticResource(n+".png", emoji.PNG(n))
		grid.Add(widget.NewButtonWithIcon("", res, func() {
			w.Close()
			onPick(n)
			setBusy(true)
			go func() {
				payload, err := emoji.RGB565Base64(n)
				if err == nil && !agent.ShowEmoji(payload) {
					err = fmt.Errorf("device not connected")
				}
				if err != nil {
					log.Printf("emoji %s: %v", n, err)
				}
				fyne.Do(func() { setBusy(false) })
			}()
		}))
	}
	w.SetContent(container.NewPadded(grid))
	w.Show()
}
