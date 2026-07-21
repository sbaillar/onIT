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

// cannedTexts populate the message box's drop-down.
var cannedTexts = []string{
	"Back in 5", "Be right back", "On lunch",
	"In a meeting", "Do not disturb", "Come on in",
}

const textHistoryKey = "textHistory"

// pushHistory prepends text to the sent-message history: newest first,
// no duplicates, capped at 3.
func pushHistory(h []string, text string) []string {
	out := []string{text}
	for _, t := range h {
		if t != text && len(out) < 3 {
			out = append(out, t)
		}
	}
	return out
}

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

	entry := widget.NewSelectEntry(cannedTexts)
	entry.SetPlaceHolder("or type a message...")
	sendText := func(text string) {
		payload, png, err := emoji.TextPayload(text)
		if err != nil {
			dialog.ShowError(err, w)
			return
		}
		prefs := a.Preferences()
		prefs.SetStringList(textHistoryKey, pushHistory(prefs.StringList(textHistoryKey), text))
		name := fmt.Sprintf("text-%08x.png", crc32.ChecksumIEEE(png))
		send(fyne.NewStaticResource(name, png), payload)
	}
	entry.OnSubmitted = sendText
	textRow := container.NewBorder(nil, nil, nil,
		widget.NewButton("Send", func() { sendText(entry.Text) }), entry)

	// the last messages sent, newest first, one click to resend
	history := container.NewVBox()
	for _, t := range a.Preferences().StringList(textHistoryKey) {
		b := widget.NewButton(t, func() { sendText(t) })
		b.Alignment = widget.ButtonAlignLeading
		b.Importance = widget.LowImportance
		history.Add(b)
	}

	w.SetContent(container.NewPadded(container.NewVBox(grid, history, textRow)))
	w.Show()
}
