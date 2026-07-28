// onIT - menu bar app for the Teams busylight.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"image/color"
	"log"
	"os"
	"runtime"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"onit/internal/busylight"
	"onit/internal/emoji"
	"onit/internal/firmware"
)

const autoLabel = "Auto (Teams)"

// remoteAddr is where onIT listens for presence pushed by `onitctl -forward`.
const remoteAddr = ":8125"

// shortcutHint renders the window shortcut for state button n (1-4).
func shortcutHint(n int) string {
	if runtime.GOOS == "darwin" {
		return fmt.Sprintf("⌘%d", n)
	}
	return fmt.Sprintf("Ctrl+%d", n)
}

// stateLabel names a state in the UI, matching the device's own wording.
func stateLabel(s string) string {
	switch s {
	case "meeting":
		return "In a call"
	case "sharing":
		return "Presenting"
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func main() {
	hidden := flag.Bool("hidden", false, "start without showing the window (login item)")
	flag.Parse()
	setupLogging()

	a := app.NewWithID("casa.baillargeon.onit")
	if appVersion == "dev" {
		if v := a.Metadata().Version; v != "" && v != "0.0.1" {
			appVersion = v // fyne-packaged builds carry the version as metadata
		}
	}
	a.Settings().SetTheme(onitTheme{base: a.Settings().Theme()})
	// all goroutine UI updates go through fyne.Do; declare that so Fyne
	// skips its "not migrated to fyne.Do" startup warning (checked lazily
	// at a.Run, so setting it here covers packaged and go-build binaries)
	meta := a.Metadata()
	if meta.Migrations == nil {
		meta.Migrations = map[string]bool{}
	}
	meta.Migrations["fyneDo"] = true
	app.SetMetadata(meta)

	if old, err := takeoverInstance(pidFilePath(), isOnitProcess); err != nil {
		log.Printf("single-instance check failed: %v", err)
	} else if old != 0 {
		msg := fmt.Sprintf("Detected running onIT (pid %d) - stopped it and started fresh.", old)
		log.Print(msg)
		a.SendNotification(fyne.NewNotification("onIT", msg))
	}
	// the pid file only knows the newest instance; after updates, older
	// copies can survive it - sweep them by process name too
	if n := killStrays(); n > 0 {
		log.Printf("stopped %d stray onIT instance(s)", n)
	}

	agent := busylight.NewAgent()
	// roulette deck: the top-picked emojis, synced to the BLE device on
	// connect. The source reports a cheap signature (the capped known slugs)
	// so an unchanged deck skips rendering; the render closure materializes
	// one deck that both the sync and the roulette winner lookup consume.
	// deviceDeck is the deck as the DEVICE holds it: the exact list recorded
	// when we last uploaded one. Recomputing it here was wrong twice over —
	// DeckImages drops entries whose artwork won't render, so every slot
	// after one shifts (the app named a neighbouring skin tone), and the
	// ordering is by usage, which changes every time you send an emoji, so
	// after a few sends the slot named something else entirely. The device
	// only takes a new deck when it syncs, so the winner has to be read
	// against what was actually sent.
	deviceDeck := func() []emoji.Entry {
		if synced := a.Preferences().StringList(syncedDeckKey); len(synced) > 0 {
			return emoji.DeckEntries(synced, busylight.DeckSlots)
		}
		// nothing recorded yet (first run, or a deck synced by an older
		// build): the freshly computed list is the best guess available
		entries, _ := emoji.DeckImages(
			topEmojiSlugs(a.Preferences().StringList(emojiUsageKey), busylight.DeckSlots),
			busylight.DeckSlots)
		return entries
	}
	agent.SetDeckSource(func() (string, func() [][]byte) {
		slugs := topEmojiSlugs(a.Preferences().StringList(emojiUsageKey), busylight.DeckSlots)
		known := emoji.DeckEntries(slugs, busylight.DeckSlots)
		names := make([]string, len(known))
		for i, e := range known {
			names[i] = e.Slug
		}
		// Hashed, not the slug list itself: the device stores this in NVS and
		// echoes it back, so it has to survive a C string (a NUL separator
		// truncated it to the first slug, and the deck re-uploaded on every
		// connect) and fit the firmware's 220-byte command buffer.
		sum := sha256.Sum256([]byte(strings.Join(names, "\n")))
		return hex.EncodeToString(sum[:8]), func() [][]byte {
			entries, images := emoji.DeckImages(slugs, busylight.DeckSlots)
			// record exactly what is about to go to the device: this closure
			// runs only when a sync is actually happening, so it is the one
			// place that knows what the device will be holding
			sent := make([]string, len(entries))
			for i, e := range entries {
				sent[i] = e.Slug
			}
			a.Preferences().SetStringList(syncedDeckKey, sent)
			return images
		}
	})

	w := a.NewWindow("onIT")
	w.SetFixedSize(true)
	w.SetCloseIntercept(w.Hide)

	// the window mirrors the device: the face redraws the firmware screens
	face := newDeviceFace()
	var lastEmoji fyne.Resource // image last sent to the device, for the face
	capLbl := widget.NewLabel("starting...")
	capLbl.Importance = widget.LowImportance
	// Bluetooth indicator (floated top-right below): lit while the BLE link
	// is carrying the device, dim otherwise. Runic berkanan is the glyph.
	bleIcon := canvas.NewText("ᛒ", bleIconDim)
	bleIcon.TextSize = 18
	bleIcon.TextStyle = fyne.TextStyle{Bold: true}
	busyBar := widget.NewProgressBarInfinite()
	busyBar.Stop()
	busyBar.Hide()
	header := container.NewVBox(container.NewCenter(face.root), container.NewCenter(capLbl), busyBar)

	// one choice list drives both the window buttons and the tray menu
	type choice struct{ label, state string }
	choices := []choice{{autoLabel, ""}}
	for _, s := range busylight.States {
		choices = append(choices, choice{stateLabel(s), s})
	}
	btns := make([]*widget.Button, len(choices))
	stateItems := make([]*fyne.MenuItem, len(choices))
	for i, c := range choices {
		if c.state == "" {
			btns[i] = widget.NewButton(c.label, func() { agent.SetOverride(c.state) })
		} else {
			// state buttons carry their window shortcut (cmd/ctrl+1-4)
			btns[i] = widget.NewButtonWithIcon(c.label+"  "+shortcutHint(i),
				dotResource(c.state), func() { agent.SetOverride(c.state) })
			key := fyne.KeyName('0' + rune(i))
			w.Canvas().AddShortcut(
				&desktop.CustomShortcut{KeyName: key, Modifier: fyne.KeyModifierShortcutDefault},
				func(fyne.Shortcut) { agent.SetOverride(c.state) })
		}
		stateItems[i] = fyne.NewMenuItem(c.label, func() { agent.SetOverride(c.state) })
		if c.state != "" {
			stateItems[i].Icon = dotResource(c.state)
		}
	}

	var setBusy func(bool)

	// drop-down: last messages sent (here or in the emoji window), then
	// pinned messages, then the canned responses; every row has an X to
	// delete it (built-in canned ones stay suppressed once deleted)
	prefs := a.Preferences()
	options := func() []string {
		return customOptions(prefs.StringList(textHistoryKey),
			prefs.StringList(pinnedTextsKey), prefs.StringList(removedTextsKey))
	}
	customEntry := widget.NewEntry()
	customEntry.SetPlaceHolder("Custom message...")
	// messageColors returns the colors for msg: its own remembered pair if
	// it has one, the last globally picked colors otherwise
	messageColors := func(msg string) (string, string) {
		if bg, fg, ok := recallColors(prefs.StringList(messageColorsKey), msg); ok {
			return bg, fg
		}
		return prefs.String(customBgKey), prefs.String(customFgKey)
	}
	showCustom := func(msg string) {
		msg = strings.TrimSpace(msg)
		if msg != "" {
			bg, fg := messageColors(msg)
			agent.SetOverride("custom:" + customPayload(bg, fg, msg))
			prefs.SetStringList(textHistoryKey, pushHistory(prefs.StringList(textHistoryKey), msg))
		}
	}
	customEntry.OnSubmitted = showCustom

	// palette: pick the message background/font colors. Picking while a
	// message is showing updates the device live and is remembered for
	// that message, so it returns in its own colors next time.
	activeCustomText := func() string {
		if ov := agent.Status().Override; strings.HasPrefix(ov, "custom:") {
			_, _, text := splitCustom(strings.TrimPrefix(ov, "custom:"))
			return text
		}
		return ""
	}
	reapplyCustom := func() {
		if text := activeCustomText(); text != "" {
			showCustom(text)
		}
	}
	pickColor := func(title, key string) {
		d := dialog.NewColorPicker(title, "", func(c color.Color) {
			r, g, b, _ := c.RGBA()
			hex := fmt.Sprintf("%02X%02X%02X", uint8(r>>8), uint8(g>>8), uint8(b>>8))
			if text := activeCustomText(); text != "" {
				bg, fg := messageColors(text) // keep the other component
				if key == customBgKey {
					bg = hex
				} else {
					fg = hex
				}
				prefs.SetStringList(messageColorsKey,
					rememberColors(prefs.StringList(messageColorsKey), bg, fg, text))
			}
			prefs.SetString(key, hex)
			reapplyCustom()
		}, w)
		d.Advanced = true
		d.Show()
	}
	paletteBtn := widget.NewButtonWithIcon("", theme.ColorPaletteIcon(), func() {
		bgBtn := widget.NewButton("Background color...", func() { pickColor("Background color", customBgKey) })
		fontBtn := widget.NewButton("Font color...", func() { pickColor("Font color", customFgKey) })
		resetBtn := widget.NewButton("Reset to yellow / black", func() {
			if text := activeCustomText(); text != "" {
				prefs.SetStringList(messageColorsKey,
					forgetColors(prefs.StringList(messageColorsKey), text))
			}
			prefs.SetString(customBgKey, "")
			prefs.SetString(customFgKey, "")
			reapplyCustom()
		})
		dialog.ShowCustom("Message colors", "Close",
			container.NewVBox(bgBtn, fontBtn, resetBtn), w)
	})
	paletteBtn.Importance = widget.LowImportance

	var showDrop func()
	dropBtn := widget.NewButtonWithIcon("", theme.MenuDropDownIcon(), func() { showDrop() })
	dropBtn.Importance = widget.LowImportance
	customEntry.ActionItem = dropBtn
	showDrop = func() {
		opts := options()
		if len(opts) == 0 {
			return
		}
		var pop *widget.PopUp
		rows := container.NewVBox()
		for _, o := range opts {
			pick := widget.NewButton(o, func() {
				pop.Hide()
				customEntry.SetText(o) // OnChanged applies it immediately
			})
			pick.Alignment = widget.ButtonAlignLeading
			pick.Importance = widget.LowImportance
			del := widget.NewButtonWithIcon("", theme.ContentClearIcon(), func() {
				h, p, r := removeMessage(prefs.StringList(textHistoryKey),
					prefs.StringList(pinnedTextsKey), prefs.StringList(removedTextsKey), o)
				prefs.SetStringList(textHistoryKey, h)
				prefs.SetStringList(pinnedTextsKey, p)
				prefs.SetStringList(removedTextsKey, r)
				pop.Hide()
				showDrop() // reopen with the row gone
			})
			del.Importance = widget.LowImportance
			rows.Add(container.NewBorder(nil, nil, nil, del, pick))
		}
		pop = widget.NewPopUp(rows, w.Canvas())
		// Fyne clips a pop-up to the window canvas, so opening below the
		// entry — which sits near the bottom — left room for a single row.
		// Keep it below when it fits, otherwise slide it up until it does.
		pos := a.Driver().AbsolutePositionForObject(customEntry)
		h := pop.MinSize().Height
		canvasH := w.Canvas().Size().Height
		y := pos.Y + customEntry.Size().Height
		if y+h > canvasH {
			y = max(0, canvasH-h)
		}
		pop.ShowAtPosition(fyne.NewPos(pos.X, y))
		pop.Resize(fyne.NewSize(customEntry.Size().Width, h))
	}

	// the pin keeps the typed message in the drop-down permanently
	var pinBtn *widget.Button
	pinIcon := func() fyne.Resource {
		if slices.Contains(prefs.StringList(pinnedTextsKey), strings.TrimSpace(customEntry.Text)) {
			return theme.ConfirmIcon() // pinned: tapping unpins
		}
		return theme.ContentAddIcon()
	}
	pinBtn = widget.NewButtonWithIcon("", theme.ContentAddIcon(), func() {
		msg := strings.TrimSpace(customEntry.Text)
		if msg == "" {
			return
		}
		p := prefs.StringList(pinnedTextsKey)
		if i := slices.Index(p, msg); i >= 0 {
			p = slices.Delete(p, i, i+1)
		} else {
			p = append(p, msg)
		}
		prefs.SetStringList(pinnedTextsKey, p)
		pinBtn.SetIcon(pinIcon())
	})
	// picking a drop-down option (or typing one out exactly) applies it
	// immediately - no extra click needed
	customEntry.OnChanged = func(s string) {
		pinBtn.SetIcon(pinIcon())
		s = strings.TrimSpace(s)
		if s != "" && slices.Contains(options(), s) {
			showCustom(s)
		}
	}

	customBtn := widget.NewButtonWithIcon("", dotResource("custom"), func() { showCustom(customEntry.Text) })
	emojiBtn := widget.NewButtonWithIcon("",
		fyne.NewStaticResource("smile.png", emoji.PNG("smile")),
		func() { showEmojiPicker(a, agent, setBusy, func(res fyne.Resource) { lastEmoji = res }) })
	customRow := container.NewBorder(nil, nil, nil, container.NewHBox(paletteBtn, pinBtn, customBtn, emojiBtn), customEntry)

	fwLbl := widget.NewLabel("Firmware: ...")
	fwLbl.Importance = widget.LowImportance
	fwBtn := widget.NewButton("Update firmware", nil)
	// targetFW is the bundled firmware version for the sensed board. A board
	// that hasn't answered VERSION yet falls back to the only image we carry.
	targetFW := func(board string) string {
		if _, v, err := firmware.ForBoard(board); err == nil {
			return v
		}
		return firmware.AmoledVersion
	}

	var update func()
	graphSetupBtn := widget.NewButton("Presence setup...", func() {
		showGraphSetup(a, agent, func() { fyne.Do(update) })
	})

	// mic rule: a live microphone (any app) escalates green to In a call
	micCheck := widget.NewCheck("Mic in use shows In a call", func(on bool) {
		prefs.SetBool("micRule", on)
		agent.SetMicRule(on)
	})
	micOn := prefs.BoolWithFallback("micRule", true)
	micCheck.SetChecked(micOn)
	agent.SetMicRule(micOn)

	// beta channel: "Check for updates" also considers prerelease builds.
	// Leaving it stays put until stable catches up (newerVersion never
	// offers an older stable to someone already on a beta).
	betaCheck := widget.NewCheck("Get beta updates (pre-release)", func(on bool) {
		prefs.SetBool(betaKey, on)
	})
	betaCheck.SetChecked(prefs.Bool(betaKey))

	// Verbose logging: every protocol line in both directions, for working
	// out what a misbehaving device and app are actually saying to each other.
	verboseCheck := widget.NewCheck("Verbose logging (protocol lines)", func(on bool) {
		prefs.SetBool(verboseKey, on)
		busylight.Verbose.Store(on)
	})
	verboseCheck.SetChecked(prefs.Bool(verboseKey))
	busylight.Verbose.Store(prefs.Bool(verboseKey))

	loginCheck := widget.NewCheck("Start at login", nil)
	loginCheck.SetChecked(autostartEnabled())
	loginCheck.OnChanged = func(on bool) {
		if err := setAutostart(on); err != nil {
			log.Printf("autostart update failed: %v", err)
		}
	}

	// tray shortcuts: top-5 emojis and top-5 messages as submenus,
	// rebuilt as usage and history evolve
	emojiBySlug := map[string]emoji.Entry{}
	for _, e := range emoji.All() {
		emojiBySlug[e.Slug] = e
	}
	emojiParent := fyne.NewMenuItem("Send emoji", nil)
	emojiParent.ChildMenu = fyne.NewMenu("")
	msgParent := fyne.NewMenuItem("Set message", nil)
	msgParent.ChildMenu = fyne.NewMenu("")
	refreshTrayShortcuts := func() {
		var eItems []*fyne.MenuItem
		for _, slug := range topEmojiSlugs(prefs.StringList(emojiUsageKey), 5) {
			e, ok := emojiBySlug[slug]
			if !ok {
				continue
			}
			it := fyne.NewMenuItem(e.Name, func() {
				sendEmojiNow(a, agent, setBusy, func(res fyne.Resource) { lastEmoji = res }, e)
			})
			it.Icon = fyne.NewStaticResource(e.Slug+".png", e.PNG())
			eItems = append(eItems, it)
		}
		emojiParent.ChildMenu.Items = eItems
		var mItems []*fyne.MenuItem
		for i, msg := range options() {
			if i == 5 {
				break
			}
			mItems = append(mItems, fyne.NewMenuItem(msg, func() { showCustom(msg) }))
		}
		msgParent.ChildMenu.Items = mItems
	}
	refreshTrayShortcuts()

	menuItems := []*fyne.MenuItem{
		fyne.NewMenuItem("Open onIT", func() { w.Show(); w.RequestFocus() }),
		fyne.NewMenuItemSeparator(),
	}
	menuItems = append(menuItems, stateItems...)
	menuItems = append(menuItems,
		fyne.NewMenuItemSeparator(),
		emojiParent, msgParent,
	)
	// device section: live transport indicator plus BLE pairing. Its rows
	// come and go with bond state (fyne.MenuItem cannot hide), so the tray
	// item list is recomposed from menuItems + section + menuTail on update.
	transportItem := fyne.NewMenuItem("—", nil)
	transportItem.Disabled = true // indicator line, not clickable (renders dimmed)
	pairItem := fyne.NewMenuItem("Pair busylight...", func() { w.Show(); showBLEPair(a, agent, w) })
	lostItem := fyne.NewMenuItem("Pairing lost - re-pair...", func() { w.Show(); showBLEPair(a, agent, w) })
	forgetItem := fyne.NewMenuItem("Forget device", func() { agent.ForgetBLE() })
	// The window face mirrors the device during a spin: same 5s ease-out
	// through the same deck, so both screens are doing the same thing. The
	// winner event ends it early and settles on the real result.
	// A generation counter, not a channel: winners can arrive twice over (the
	// device emits an event on both links, and each dispatch is its own
	// goroutine), and two closers of one channel panic.
	var spinGen atomic.Uint64
	stopSpinFace := func() { spinGen.Add(1) }
	spinFace := func() {
		deck := deviceDeck()
		if len(deck) == 0 {
			return
		}
		mine := spinGen.Add(1)
		agent.SetOverride("emoji") // so the face shows emoji, not the off screen
		go func() {
			deadline := time.Now().Add(spinDuration)
			for i := 0; time.Now().Before(deadline); i++ {
				time.Sleep(spinFrameGap(i))
				if spinGen.Load() != mine {
					return // superseded by the winner, or by a newer spin
				}
				res := emojiRes(deck[i%len(deck)])
				fyne.Do(func() {
					if spinGen.Load() != mine {
						return // the winner landed while this frame was queued
					}
					lastEmoji = res
					face.Set("emoji", res) // face only: update() rebuilds the tray
				})
			}
		}()
	}

	spinItem := fyne.NewMenuItem("Spin the wheel", func() {
		go func() {
			// Only mirror a spin the device actually started: spinFace takes
			// the "emoji" override, and with no spin there is no winner event
			// to hand it back, which would strand presence behind a blank
			// emoji screen.
			if err := agent.Spin(); err != nil {
				log.Printf("spin failed: %v", err)
				return
			}
			fyne.Do(spinFace)
		}()
	})
	syncItem := fyne.NewMenuItem("syncing emojis...", nil)
	syncItem.Disabled = true // indicator line, not clickable (renders dimmed)

	var showSettings func() // assigned once the settings window exists
	// Marked IsQuit so Fyne doesn't append a second Quit of its own
	// (addMissingQuitForMenu only adds one when the last item isn't a quit),
	// which means it has to stay last.
	quitItem := fyne.NewMenuItem("Quit onIT", func() { a.Quit() })
	quitItem.IsQuit = true
	menuTail := []*fyne.MenuItem{
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem("Settings...", func() { showSettings() }),
		fyne.NewMenuItem("Show log...", func() { showLog(a) }),
		fyne.NewMenuItem("Check for updates...", func() { w.Show(); checkForUpdates(w, prefs.Bool(betaKey)) }),
		fyne.NewMenuItemSeparator(),
		quitItem,
	}
	trayMenu := fyne.NewMenu("onIT")
	rebuildTray := func(st busylight.Status) {
		switch st.Transport {
		case "ble":
			transportItem.Label = "⋆ BLE"
		case "usb":
			transportItem.Label = "⚡ USB"
		default:
			transportItem.Label = "—"
		}
		dev := []*fyne.MenuItem{fyne.NewMenuItemSeparator(), transportItem}
		if st.DeckSyncing {
			dev = append(dev, syncItem)
		}
		if st.Transport != "" { // the deck syncs over either link now
			dev = append(dev, spinItem)
		}
		if st.PairingLost {
			dev = append(dev, lostItem)
		}
		// Pairing is only an offer when there's nothing to pair with: while
		// BLE is live the transport line above already says so, dimmed, the
		// way the USB line does.
		if st.Transport != "ble" {
			dev = append(dev, pairItem)
		}
		if st.BLEBonded {
			dev = append(dev, forgetItem)
		}
		trayMenu.Items = append(append(append([]*fyne.MenuItem{}, menuItems...), dev...), menuTail...)
	}
	rebuildTray(agent.Status())
	// roulette winner: nothing intrusive - the "Spin the wheel" line briefly
	// shows which emoji the wheel settled on, then reverts
	var spinRevert *time.Timer // pending revert of the winner line (UI thread only)
	agent.SetOnRoulette(func(slot int) {
		// resolve slot against the current deck, derived the same way the
		// device's deck was (entries correspond 1:1 with the synced images by
		// construction — see emoji.DeckImages). Computed at event time rather
		// than cached, so it survives an app restart with an unchanged deck.
		deck := deviceDeck()
		if slot < 0 || slot >= len(deck) {
			return
		}
		e := deck[slot]
		// Adopt the winner as the shown state. The device keeps it on screen
		// only until the next STATE: line, and the heartbeat sends one every
		// 2s — STATE:off with no presence source — which wiped the winner
		// almost as soon as the wheel stopped. "emoji" is the one state the
		// firmware won't apply over a winner, so the spin result stays up
		// until the next spin or a state the user picks.
		stopSpinFace() // the real winner supersedes the mirrored animation
		agent.SetOverride("emoji")
		fyne.Do(func() {
			lastEmoji = emojiRes(e) // window face mirrors it
			if spinRevert != nil {
				spinRevert.Stop() // a newer winner supersedes any pending revert
			}
			spinItem.Label = "Spin the wheel - " + e.Name
			spinItem.Icon = emojiRes(e)
			trayMenu.Refresh()
			spinRevert = time.AfterFunc(5*time.Second, func() {
				fyne.Do(func() {
					spinItem.Label = "Spin the wheel"
					spinItem.Icon = nil
					trayMenu.Refresh()
				})
			})
		})
	})
	desk, isDesk := a.(desktop.App)
	if isDesk {
		desk.SetSystemTrayMenu(trayMenu) // Fyne appends Quit
		desk.SetSystemTrayIcon(dotResource("off"))
	}

	setBusy = func(on bool) {
		widgets := []fyne.Disableable{customEntry, customBtn, emojiBtn, fwBtn}
		if on {
			busyBar.Show()
			busyBar.Start()
			for _, b := range btns {
				b.Disable()
			}
			for _, x := range widgets {
				x.Disable()
			}
		} else {
			busyBar.Stop()
			busyBar.Hide()
			for _, b := range btns {
				b.Enable()
			}
			for _, x := range widgets {
				x.Enable()
			}
			w.Resize(fyne.NewSize(260, 0)) // the hidden bar leaves the window tall
		}
	}

	lastShown := ""
	flashing := false
	update = func() {
		st := agent.Status()

		face.Set(st.Shown, lastEmoji)

		src := "no presence source"
		switch {
		case st.TeamsConnected && st.Source == "remote":
			src = "Remote relay"
		case st.TeamsConnected && st.Source == "graph":
			src = "Microsoft Graph"
		case st.TeamsConnected && st.Source == "teamslog":
			src = "Teams app (local)"
		case st.TeamsConnected:
			src = "Teams local API"
		}
		light := "light connected"
		if !st.LightConnected {
			light = "light not found"
		}
		capLbl.SetText(src + "  /  " + light)

		wantBLE := bleIconDim
		if st.Transport == "ble" {
			wantBLE = bleIconLit
		}
		if bleIcon.Color != wantBLE {
			bleIcon.Color = wantBLE
			bleIcon.Refresh()
		}

		shownKey := stateKey(st.Shown)
		for i, c := range choices {
			want := widget.MediumImportance
			if st.Override == c.state && (c.state != "" || st.Override == "") {
				want = widget.HighImportance
			}
			if btns[i].Importance != want {
				btns[i].Importance = want
				btns[i].Refresh()
			}
			// highlight the live state too: ringed dot + check, so in Auto
			// the menu still shows what the light is doing right now
			live := c.state != "" && c.state == shownKey
			if c.state != "" {
				icon := dotResource(c.state)
				if live {
					icon = activeDotResource(c.state)
				}
				if stateItems[i].Icon != icon {
					stateItems[i].Icon = icon
				}
			}
			checked := want == widget.HighImportance || live
			if stateItems[i].Checked != checked {
				stateItems[i].Checked = checked
			}
		}
		rebuildTray(st)        // transport/bond state may have changed
		refreshTrayShortcuts() // usage/history may have changed
		trayMenu.Refresh()

		if !flashing {
			switch {
			case !st.LightConnected:
				fwLbl.SetText("Firmware: no device")
				fwBtn.Disable()
			case st.DeviceFW == targetFW(st.Board):
				fwLbl.SetText("Firmware " + st.DeviceFW + " - up to date")
				fwBtn.SetText("Reflash firmware")
				fwBtn.Importance = widget.LowImportance // usable, not inviting
				fwBtn.Enable()
				fwBtn.Refresh()
			default:
				from := st.DeviceFW
				if from == "" {
					from = "unknown"
				}
				fwLbl.SetText("Firmware " + from + " -> " + targetFW(st.Board))
				fwBtn.SetText("Update firmware")
				fwBtn.Importance = widget.HighImportance
				fwBtn.Enable()
				fwBtn.Refresh()
			}
		}

		if isDesk && st.Shown != lastShown {
			lastShown = st.Shown
			desk.SetSystemTrayIcon(dotResource(stateKey(st.Shown)))
		}
	}
	agent.OnChange(func() { fyne.Do(update) })

	fwBtn.OnTapped = func() {
		flashing = true
		setBusy(true)
		fwLbl.SetText("Flashing " + targetFW(agent.Status().Board) + " - do not unplug...")
		go func() {
			err := agent.FlashFirmware(esptoolPath(), false)
			fyne.Do(func() {
				flashing = false
				setBusy(false)
				if err != nil {
					log.Printf("flash failed: %v", err)
					dialog.ShowError(fmt.Errorf("firmware update failed:\n\n%v\n\nFull log: %s", err, logPath()), w)
					return
				}
				fwLbl.SetText("Flashed - waiting for device...")
			})
		}()
	}

	grid := container.NewGridWithColumns(2)
	for _, b := range btns[1:] { // 4 states, 2x2
		grid.Add(b)
	}
	// Remote presence: accept pushes from `onitctl -forward` on another
	// machine (e.g. an org-managed one that can sign in to Graph).
	var remoteSrv *busylight.RemoteServer
	if a.Preferences().Bool("remoteListen") {
		var err error
		if remoteSrv, err = agent.ListenRemote(remoteAddr); err != nil {
			log.Printf("remote listener: %v", err)
		}
	}
	remoteCheck := widget.NewCheck("Accept remote presence (port 8125)", func(on bool) {
		a.Preferences().SetBool("remoteListen", on)
		if on && remoteSrv == nil {
			var err error
			if remoteSrv, err = agent.ListenRemote(remoteAddr); err != nil {
				dialog.ShowError(err, w)
				remoteSrv = nil
				return
			}
			host, _ := os.Hostname()
			cmd := fmt.Sprintf("onitctl -forward http://%s:8125 -token %s",
				host, remoteSrv.Token())
			cmdEntry := widget.NewEntry() // selectable, so the token can be copied
			cmdEntry.SetText(cmd)
			dialog.ShowCustom("Remote presence enabled", "Close", container.NewVBox(
				widget.NewLabel("On the machine that can sign in to Microsoft, run:"),
				cmdEntry,
				widget.NewButton("Copy command", func() { a.Clipboard().SetContent(cmd) }),
			), w)
		} else if !on && remoteSrv != nil {
			remoteSrv.Close()
			remoteSrv = nil
		}
	})
	remoteCheck.SetChecked(remoteSrv != nil)

	// Settings live in their own window: inline they made the main window
	// grow and shrink, and any pop-up opened near the bottom of the short
	// window had nowhere to unfold into (Fyne clips pop-ups to the canvas).
	// Built once and hidden on close so update() keeps its widget pointers.
	settingsWin := a.NewWindow("onIT Settings")
	settingsWin.SetContent(container.NewVBox(
		fwLbl, fwBtn, graphSetupBtn, remoteCheck, micCheck, betaCheck, verboseCheck, loginCheck))
	settingsWin.SetCloseIntercept(settingsWin.Hide)
	settingsWin.Resize(fyne.NewSize(300, 0))
	showSettings = func() { settingsWin.Show(); settingsWin.RequestFocus() }

	settingsBtn := widget.NewButtonWithIcon("Settings", theme.SettingsIcon(), showSettings)
	settingsBtn.Alignment = widget.ButtonAlignLeading
	settingsBtn.Importance = widget.LowImportance
	// help menu in the top-left corner (an LSUIElement app has no menu bar)
	helpMenu := fyne.NewMenu("",
		fyne.NewMenuItem("Check for updates...", func() { checkForUpdates(w, prefs.Bool(betaKey)) }),
		fyne.NewMenuItem("About onIT...", func() { showAbout(a) }),
	)
	var helpBtn *widget.Button
	helpBtn = widget.NewButtonWithIcon("", theme.HelpIcon(), func() {
		pos := a.Driver().AbsolutePositionForObject(helpBtn)
		widget.ShowPopUpMenuAtPosition(helpMenu, w.Canvas(),
			pos.Add(fyne.NewPos(0, helpBtn.Size().Height)))
	})
	helpBtn.Importance = widget.LowImportance

	w.SetContent(container.NewStack(
		container.NewVBox(
			header,
			widget.NewSeparator(),
			btns[0], // Auto (Teams)
			grid,
			customRow,
			widget.NewSeparator(),
			settingsBtn,
		),
		container.NewBorder( // floats over the face's empty corners
			container.NewHBox(helpBtn, layout.NewSpacer(),
				container.NewPadded(bleIcon)), nil, nil, nil, nil),
	))

	w.Resize(fyne.NewSize(260, 0)) // height from content; keep it compact

	// first launch from the installed location: enable the login item
	exe, _ := os.Executable()
	if !a.Preferences().Bool("autostartConfigured") && autostartAutoEnable(exe) {
		if err := setAutostart(true); err != nil {
			log.Printf("autostart install failed: %v", err)
		} else {
			loginCheck.SetChecked(true)
		}
		a.Preferences().SetBool("autostartConfigured", true)
	}

	go agent.Run()

	if *hidden {
		a.Run()
	} else {
		w.ShowAndRun()
	}
}
