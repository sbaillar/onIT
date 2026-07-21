package main

import (
	"fmt"
	"hash/crc32"
	"log"
	"slices"
	"sort"
	"strconv"
	"strings"

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

const (
	textHistoryKey = "textHistory"
	pinnedTextsKey = "pinnedTexts"
	emojiUsageKey  = "emojiUsage"
)

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

// customOptions builds the message drop-down: recent messages first, then
// pinned messages, then the canned responses — each tier deduplicated
// against the ones above it.
func customOptions(history, pinned []string) []string {
	opts := slices.Clone(history)
	for _, tier := range [][]string{pinned, cannedTexts} {
		for _, c := range tier {
			if !slices.Contains(opts, c) {
				opts = append(opts, c)
			}
		}
	}
	return opts
}

// Usage counts live in preferences as "slug count" strings.

// bumpUsage increments slug's send count.
func bumpUsage(list []string, slug string) []string {
	for i, e := range list {
		s, n, ok := strings.Cut(e, " ")
		if ok && s == slug {
			c, _ := strconv.Atoi(n)
			list[i] = fmt.Sprintf("%s %d", slug, c+1)
			return list
		}
	}
	return append(list, slug+" 1")
}

// topUsed returns up to n slugs, most used first.
func topUsed(list []string, n int) []string {
	type sc struct {
		slug  string
		count int
	}
	var all []sc
	for _, e := range list {
		s, c, ok := strings.Cut(e, " ")
		if !ok {
			continue
		}
		count, err := strconv.Atoi(c)
		if err != nil {
			continue
		}
		all = append(all, sc{s, count})
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].count > all[j].count })
	var out []string
	for _, e := range all {
		if len(out) == n {
			break
		}
		out = append(out, e.slug)
	}
	return out
}

// showEmojiPicker lets the user send any standard emoji — or a short text,
// auto-fitted to the round display — to the device (transfer takes ~2s at
// 115200 baud). The top row quick-selects the 10 most-sent emojis. onPick
// reports the sent image so the window face can mirror it.
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

	all := emoji.All()
	bySlug := make(map[string]emoji.Entry, len(all))
	for _, e := range all {
		bySlug[e.Slug] = e
	}

	sendEmoji := func(e emoji.Entry) {
		payload, err := e.Payload()
		if err != nil {
			log.Printf("emoji %s: %v", e.Slug, err)
			return
		}
		prefs := a.Preferences()
		prefs.SetStringList(emojiUsageKey, bumpUsage(prefs.StringList(emojiUsageKey), e.Slug))
		send(fyne.NewStaticResource(e.Slug+".png", e.PNG()), payload)
	}

	// quick select: the 10 most-sent emojis (a starter set until then)
	quick := topUsed(a.Preferences().StringList(emojiUsageKey), 10)
	if len(quick) < 10 {
		for _, s := range []string{"thumbs-up", "red-heart", "face-with-tears-of-joy",
			"hot-beverage", "pizza", "headphone", "check-mark-button", "fire",
			"party-popper", "no-entry"} {
			if len(quick) == 10 {
				break
			}
			if !slices.Contains(quick, s) {
				quick = append(quick, s)
			}
		}
	}
	quickRow := container.NewGridWithColumns(10)
	for _, s := range quick {
		if e, ok := bySlug[s]; ok {
			res := fyne.NewStaticResource(e.Slug+".png", e.PNG())
			quickRow.Add(widget.NewButtonWithIcon("", res, func() { sendEmoji(e) }))
		}
	}

	// searchable virtualized grid over the full set
	filtered := all
	var grid *widget.GridWrap
	grid = widget.NewGridWrap(
		func() int { return len(filtered) },
		func() fyne.CanvasObject {
			b := widget.NewButtonWithIcon("", nil, nil)
			b.Importance = widget.LowImportance
			return b
		},
		func(id widget.GridWrapItemID, o fyne.CanvasObject) {
			e := filtered[id]
			b := o.(*widget.Button)
			b.SetIcon(fyne.NewStaticResource(e.Slug+".png", e.PNG()))
			b.OnTapped = func() { sendEmoji(e) }
		},
	)
	search := widget.NewEntry()
	search.SetPlaceHolder("Search emoji...")
	search.OnChanged = func(q string) {
		q = strings.ToLower(strings.TrimSpace(q))
		if q == "" {
			filtered = all
		} else {
			filtered = nil
			for _, e := range all {
				if strings.Contains(e.Name, q) || strings.Contains(e.Slug, q) {
					filtered = append(filtered, e)
				}
			}
		}
		grid.Refresh()
		grid.ScrollToTop()
	}

	entry := widget.NewEntry()
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

	w.SetContent(container.NewPadded(container.NewBorder(
		container.NewVBox(quickRow, search), textRow, nil, nil, grid)))
	w.Resize(fyne.NewSize(560, 640)) // roughly double the old picker
	w.Show()
}
