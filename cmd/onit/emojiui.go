package main

import (
	"fmt"
	"hash/crc32"
	"log"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"

	"onit/internal/busylight"
	"onit/internal/emoji"
)

// showEmojiPicker lets the user send one of the embedded emojis — or a short
// text, auto-fitted to the round display — to the device (transfer takes ~2s
// at 115200 baud). onPick reports the sent image so the window face can
// mirror it.
func showEmojiPicker(a fyne.App, agent *busylight.Agent, setBusy func(bool), onPick func(res fyne.Resource)) {
	w := a.NewWindow("Send an emoji")

	send := func(res fyne.Resource, payload string) {
		w.Close()
		onPick(res)
		setBusy(true)
		go func() {
			if !agent.ShowEmoji(payload) {
				log.Printf("emoji: device not connected")
			}
			fyne.Do(func() { setBusy(false) })
		}()
	}

	grid := container.NewGridWithColumns(4)
	for _, n := range emoji.Names {
		res := fyne.NewStaticResource(n+".png", emoji.PNG(n))
		grid.Add(widget.NewButtonWithIcon("", res, func() {
			payload, err := emoji.RGB565Base64(n)
			if err != nil {
				log.Printf("emoji %s: %v", n, err)
				return
			}
			send(res, payload)
		}))
	}

	entry := widget.NewEntry()
	entry.SetPlaceHolder("or type a message...")
	sendText := func(text string) {
		payload, png, err := emoji.TextPayload(text)
		if err != nil {
			dialog.ShowError(err, w)
			return
		}
		name := fmt.Sprintf("text-%08x.png", crc32.ChecksumIEEE(png))
		send(fyne.NewStaticResource(name, png), payload)
	}
	entry.OnSubmitted = sendText
	textRow := container.NewBorder(nil, nil, nil,
		widget.NewButton("Send", func() { sendText(entry.Text) }), entry)

	w.SetContent(container.NewPadded(container.NewVBox(grid, textRow)))
	w.Show()
}
