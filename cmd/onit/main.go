// onIT - menu bar app for the Teams busylight.
package main

import (
	"flag"
	"fmt"
	"image/color"
	"log"
	"os"
	"runtime"
	"slices"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
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

	if old, err := takeoverInstance(pidFilePath(), isOnitProcess); err != nil {
		log.Printf("single-instance check failed: %v", err)
	} else if old != 0 {
		msg := fmt.Sprintf("Detected running onIT (pid %d) - stopped it and started fresh.", old)
		log.Print(msg)
		a.SendNotification(fyne.NewNotification("onIT", msg))
	}

	agent := busylight.NewAgent()

	w := a.NewWindow("onIT")
	w.SetFixedSize(true)
	w.SetCloseIntercept(w.Hide)

	// the window mirrors the device: the face redraws the firmware screens
	face := newDeviceFace()
	var lastEmoji fyne.Resource // image last sent to the device, for the face
	capLbl := widget.NewLabel("starting...")
	capLbl.Importance = widget.LowImportance
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
	showCustom := func(msg string) {
		msg = strings.TrimSpace(msg)
		if msg != "" {
			agent.SetOverride("custom:" + customPayload(
				prefs.String(customBgKey), prefs.String(customFgKey), msg))
			prefs.SetStringList(textHistoryKey, pushHistory(prefs.StringList(textHistoryKey), msg))
		}
	}
	customEntry.OnSubmitted = showCustom

	// palette: pick the message background/font colors; reapplies a live
	// custom message so the device updates as you pick
	reapplyCustom := func() {
		if ov := agent.Status().Override; strings.HasPrefix(ov, "custom:") {
			_, _, text := splitCustom(strings.TrimPrefix(ov, "custom:"))
			showCustom(text)
		}
	}
	pickColor := func(title, key string) {
		d := dialog.NewColorPicker(title, "", func(c color.Color) {
			r, g, b, _ := c.RGBA()
			prefs.SetString(key, fmt.Sprintf("%02X%02X%02X", uint8(r>>8), uint8(g>>8), uint8(b>>8)))
			reapplyCustom()
		}, w)
		d.Advanced = true
		d.Show()
	}
	paletteBtn := widget.NewButtonWithIcon("", theme.ColorPaletteIcon(), func() {
		bgBtn := widget.NewButton("Background color...", func() { pickColor("Background color", customBgKey) })
		fontBtn := widget.NewButton("Font color...", func() { pickColor("Font color", customFgKey) })
		resetBtn := widget.NewButton("Reset to yellow / black", func() {
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
		pos := a.Driver().AbsolutePositionForObject(customEntry)
		pop.ShowAtPosition(pos.Add(fyne.NewPos(0, customEntry.Size().Height)))
		pop.Resize(fyne.NewSize(customEntry.Size().Width, pop.MinSize().Height))
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

	var update func()
	graphSetupBtn := widget.NewButton("Presence setup...", func() {
		showGraphSetup(a, agent, func() { fyne.Do(update) })
	})

	loginCheck := widget.NewCheck("Start at login", nil)
	loginCheck.SetChecked(autostartEnabled())
	loginCheck.OnChanged = func(on bool) {
		if err := setAutostart(on); err != nil {
			log.Printf("autostart update failed: %v", err)
		}
	}

	menuItems := []*fyne.MenuItem{
		fyne.NewMenuItem("Open onIT", func() { w.Show(); w.RequestFocus() }),
		fyne.NewMenuItemSeparator(),
	}
	menuItems = append(menuItems, stateItems...)
	doUninstall := func() {
		w.Show()
		dialog.ShowConfirm("Uninstall onIT",
			"This removes the start-at-login entry, sign-in tokens,\n"+
				"all settings, and the app itself. The device is not affected.\n\nContinue?",
			func(ok bool) {
				if !ok {
					return
				}
				if err := setAutostart(false); err != nil {
					log.Printf("uninstall: autostart: %v", err)
				}
				if err := busylight.RemoveToken(); err != nil {
					log.Printf("uninstall: token: %v", err)
				}
				for _, d := range prefsDirs() {
					os.RemoveAll(d)
				}
				os.Remove(pidFilePath())
				os.Remove(logPath())
				if err := removeInstalledFiles(); err != nil {
					log.Printf("uninstall: app files: %v", err)
				}
				done := dialog.NewInformation("Uninstalled", uninstallDoneMsg, w)
				done.SetOnClosed(a.Quit)
				done.Show()
			}, w)
	}

	menuItems = append(menuItems,
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem("Check for updates...", func() { w.Show(); checkForUpdates(w) }),
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem("Uninstall onIT...", doUninstall),
	)
	trayMenu := fyne.NewMenu("onIT", menuItems...)
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
		case st.TeamsConnected:
			src = "Teams local API"
		}
		light := "light connected"
		if !st.LightConnected {
			light = "light not found"
		}
		capLbl.SetText(src + "  /  " + light)

		for i, c := range choices {
			want := widget.MediumImportance
			if st.Override == c.state && (c.state != "" || st.Override == "") {
				want = widget.HighImportance
			}
			if btns[i].Importance != want {
				btns[i].Importance = want
				btns[i].Refresh()
			}
			if stateItems[i].Checked != (want == widget.HighImportance) {
				stateItems[i].Checked = want == widget.HighImportance
			}
		}
		trayMenu.Refresh()

		if !flashing {
			switch {
			case !st.LightConnected:
				fwLbl.SetText("Firmware: no device")
				fwBtn.Disable()
			case st.DeviceFW == firmware.Version:
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
				fwLbl.SetText("Firmware " + from + " -> " + firmware.Version)
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
		fwLbl.SetText("Flashing " + firmware.Version + " - do not unplug...")
		go func() {
			err := agent.FlashFirmware(esptoolPath(), firmware.Bin)
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

	// Not an Accordion: Fyne grows the fixed-size window when content
	// expands but never shrinks it back, and Accordion offers no toggle
	// hook - so a look-alike button that resizes the window on collapse.
	settingsBody := container.NewVBox(fwLbl, fwBtn, graphSetupBtn, remoteCheck, loginCheck)
	settingsBody.Hide()
	var settingsBtn *widget.Button
	settingsBtn = widget.NewButtonWithIcon("Settings", theme.MenuDropDownIcon(), func() {
		if settingsBody.Visible() {
			settingsBody.Hide()
			settingsBtn.SetIcon(theme.MenuDropDownIcon())
			w.Resize(fyne.NewSize(260, 0)) // snap back to content height
		} else {
			settingsBody.Show()
			settingsBtn.SetIcon(theme.MenuDropUpIcon())
		}
	})
	settingsBtn.Alignment = widget.ButtonAlignLeading
	settingsBtn.Importance = widget.LowImportance
	settings := container.NewVBox(settingsBtn, settingsBody)
	// help menu in the top-left corner (an LSUIElement app has no menu bar)
	helpMenu := fyne.NewMenu("",
		fyne.NewMenuItem("Check for updates...", func() { checkForUpdates(w) }),
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
			settings,
		),
		container.NewBorder( // floats over the face's empty corner
			container.NewHBox(helpBtn, layout.NewSpacer()), nil, nil, nil, nil),
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
