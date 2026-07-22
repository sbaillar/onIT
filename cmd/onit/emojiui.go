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
	"fyne.io/fyne/v2/layout"
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
	textHistoryKey   = "textHistory"
	pinnedTextsKey   = "pinnedTexts"
	removedTextsKey  = "removedTexts"
	customBgKey      = "customBg"
	customFgKey      = "customFg"
	messageColorsKey = "messageColors"
	emojiUsageKey    = "emojiUsage"
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
// against the ones above it. Canned messages the user deleted (removed)
// stay hidden.
func customOptions(history, pinned, removed []string) []string {
	opts := slices.Clone(history)
	for _, c := range pinned {
		if !slices.Contains(opts, c) {
			opts = append(opts, c)
		}
	}
	for _, c := range cannedTexts {
		if !slices.Contains(opts, c) && !slices.Contains(removed, c) {
			opts = append(opts, c)
		}
	}
	return opts
}

// removeMessage deletes text from every tier it appears in; built-in canned
// messages are suppressed via the removed list.
func removeMessage(history, pinned, removed []string, text string) (h, p, r []string) {
	drop := func(l []string) []string {
		if i := slices.Index(l, text); i >= 0 {
			return slices.Delete(slices.Clone(l), i, i+1)
		}
		return l
	}
	h, p, r = drop(history), drop(pinned), removed
	if slices.Contains(cannedTexts, text) && !slices.Contains(removed, text) {
		r = append(slices.Clone(removed), text)
	}
	return h, p, r
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

// emojiCell is a bare tappable emoji image - lighter than a Button, so
// thousands of them can sit in one scrolling grid.
type emojiCell struct {
	widget.Icon
	onTap func()
}

func newEmojiCell(res fyne.Resource, onTap func()) *emojiCell {
	c := &emojiCell{onTap: onTap}
	c.ExtendBaseWidget(c)
	c.SetResource(res)
	return c
}

func (c *emojiCell) Tapped(*fyne.PointEvent) { c.onTap() }
func (c *emojiCell) MinSize() fyne.Size      { return fyne.NewSize(40, 40) }

const emojiRows = 6

// emojiGrid lays entries out like the iPhone keyboard: column-major in a
// fixed number of rows, scrolling horizontally.
func emojiGrid(items []emoji.Entry, onTap func(emoji.Entry)) fyne.CanvasObject {
	cols := (len(items) + emojiRows - 1) / emojiRows
	objs := make([]fyne.CanvasObject, 0, emojiRows*cols)
	for r := 0; r < emojiRows; r++ {
		for c := 0; c < cols; c++ {
			i := c*emojiRows + r // column-major: read down, then right
			if i >= len(items) {
				objs = append(objs, layout.NewSpacer())
				continue
			}
			e := items[i]
			objs = append(objs, newEmojiCell(
				fyne.NewStaticResource(e.Slug+".png", e.PNG()),
				func() { onTap(e) }))
		}
	}
	return container.NewGridWithRows(emojiRows, objs...)
}

// showEmojiPicker lets the user send any standard emoji — or a short text,
// auto-fitted to the round display — to the device (transfer takes ~2s at
// 115200 baud). The top row quick-selects the 10 most-sent emojis. onPick
// reports the sent image so the window face can mirror it.
// pickerWin is the open picker window, if any - clicking the emoji button
// again focuses it instead of stacking a second one.
var pickerWin fyne.Window

func showEmojiPicker(a fyne.App, agent *busylight.Agent, setBusy func(bool), onPick func(res fyne.Resource)) {
	if pickerWin != nil {
		pickerWin.RequestFocus()
		return
	}
	w := a.NewWindow("Send an emoji")
	pickerWin = w
	w.SetOnClosed(func() { pickerWin = nil })

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
			quickRow.Add(newEmojiCell(
				fyne.NewStaticResource(e.Slug+".png", e.PNG()),
				func() { sendEmoji(e) }))
		}
	}

	// the full set, iPhone style: category order, scrolling to the right
	fullGrid := emojiGrid(all, sendEmoji)
	scroll := container.NewHScroll(fullGrid)
	search := widget.NewEntry()
	search.SetPlaceHolder("Search emoji...")
	search.OnChanged = func(q string) {
		q = strings.ToLower(strings.TrimSpace(q))
		if q == "" {
			scroll.Content = fullGrid
		} else {
			var filtered []emoji.Entry
			for _, e := range all {
				if strings.Contains(e.Name, q) || strings.Contains(e.Slug, q) {
					filtered = append(filtered, e)
				}
			}
			scroll.Content = emojiGrid(filtered, sendEmoji)
		}
		scroll.Offset = fyne.Position{}
		scroll.Refresh()
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
		container.NewVBox(quickRow, search), textRow, nil, nil, scroll)))
	w.Resize(fyne.NewSize(680, 480)) // wide: the grid scrolls to the right
	w.Show()
}
