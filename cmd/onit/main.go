// onIT - menu bar app for the Teams busylight.
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
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

func title(s string) string {
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
	lastEmoji := "" // name of the emoji last sent, for the face
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
		choices = append(choices, choice{title(s), s})
	}
	btns := make([]*widget.Button, len(choices))
	stateItems := make([]*fyne.MenuItem, len(choices))
	for i, c := range choices {
		if c.state == "" {
			btns[i] = widget.NewButton(c.label, func() { agent.SetOverride(c.state) })
		} else {
			btns[i] = widget.NewButtonWithIcon(c.label, dotResource(c.state),
				func() { agent.SetOverride(c.state) })
		}
		stateItems[i] = fyne.NewMenuItem(c.label, func() { agent.SetOverride(c.state) })
		if c.state != "" {
			stateItems[i].Icon = dotResource(c.state)
		}
	}

	var setBusy func(bool)

	customEntry := widget.NewEntry()
	customEntry.SetPlaceHolder("Custom message...")
	showCustom := func(msg string) {
		msg = strings.TrimSpace(msg)
		if msg != "" {
			agent.SetOverride("custom:" + msg)
		}
	}
	customEntry.OnSubmitted = showCustom
	customBtn := widget.NewButtonWithIcon("", dotResource("custom"), func() { showCustom(customEntry.Text) })
	emojiBtn := widget.NewButtonWithIcon("",
		fyne.NewStaticResource("smile.png", emoji.PNG("smile")),
		func() { showEmojiPicker(a, agent, setBusy, func(name string) { lastEmoji = name }) })
	customRow := container.NewBorder(nil, nil, nil, container.NewHBox(customBtn, emojiBtn), customEntry)

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
