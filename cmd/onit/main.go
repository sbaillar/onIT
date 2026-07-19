// onIT - menu bar app for the Teams busylight.
package main

import (
	"flag"
	"fmt"
	"image/color"
	"log"
	"os"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/widget"

	"onit/internal/busylight"
	"onit/internal/firmware"
)

const autoLabel = "Auto (Teams)"

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

	// header: the window mirrors the device - big state dot + state name
	headerDot := canvas.NewCircle(stateColors["off"])
	headerName := canvas.NewText("Off", color.NRGBA{0xFF, 0xFF, 0xFF, 0xFF})
	headerName.TextSize = 22
	headerName.TextStyle = fyne.TextStyle{Bold: true}
	capLbl := widget.NewLabel("starting...")
	capLbl.Importance = widget.LowImportance
	header := container.NewVBox(
		container.NewHBox(
			container.NewGridWrap(fyne.NewSize(24, 34), layoutCircle(headerDot)),
			headerName,
		),
		capLbl,
	)

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

	fwLbl := widget.NewLabel("Firmware: ...")
	fwLbl.Importance = widget.LowImportance
	fwBtn := widget.NewButton("Update firmware", nil)

	var update func()
	graphSetupBtn := widget.NewButton("Presence setup...", func() {
		showGraphSetup(a, agent, func() { fyne.Do(update) })
	})

	uninstallBtn := widget.NewButton("Uninstall onIT...", nil)
	uninstallBtn.Importance = widget.DangerImportance

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
	menuItems = append(menuItems,
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem("Check for updates...", func() { w.Show(); checkForUpdates(w) }),
		fyne.NewMenuItem("About onIT...", func() { w.Show(); showAbout(w) }),
	)
	trayMenu := fyne.NewMenu("onIT", menuItems...)
	desk, isDesk := a.(desktop.App)
	if isDesk {
		desk.SetSystemTrayMenu(trayMenu) // Fyne appends Quit
		desk.SetSystemTrayIcon(dotResource("off"))
	}

	lastShown := ""
	flashing := false
	update = func() {
		st := agent.Status()

		headerDot.FillColor = stateColorOr(st.Shown)
		headerDot.Refresh()
		headerName.Text = title(st.Shown)
		headerName.Refresh()

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
				fwBtn.Enable()
			default:
				from := st.DeviceFW
				if from == "" {
					from = "unknown"
				}
				fwLbl.SetText("Firmware " + from + " -> " + firmware.Version)
				fwBtn.SetText("Update firmware")
				fwBtn.Enable()
			}
		}

		if isDesk && st.Shown != lastShown {
			lastShown = st.Shown
			desk.SetSystemTrayIcon(dotResource(st.Shown))
		}
	}
	agent.OnChange(func() { fyne.Do(update) })

	fwBtn.OnTapped = func() {
		flashing = true
		fwBtn.Disable()
		for _, b := range btns {
			b.Disable()
		}
		fwLbl.SetText("Flashing " + firmware.Version + " - do not unplug...")
		go func() {
			err := agent.FlashFirmware(esptoolPath(), firmware.Bin)
			fyne.Do(func() {
				flashing = false
				fwBtn.Enable()
				for _, b := range btns {
					b.Enable()
				}
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
	w.SetContent(container.NewVBox(
		header,
		widget.NewSeparator(),
		btns[0], // Auto (Teams)
		grid,
		widget.NewSeparator(),
		fwLbl, fwBtn,
		widget.NewSeparator(),
		graphSetupBtn,
		loginCheck,
		uninstallBtn,
	))

	uninstallBtn.OnTapped = func() {
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

func stateColorOr(state string) color.NRGBA {
	if c, ok := stateColors[state]; ok {
		return c
	}
	return stateColors["off"]
}

// layoutCircle keeps a canvas circle round inside a GridWrap cell by
// padding it vertically to match the header text height.
func layoutCircle(c *canvas.Circle) fyne.CanvasObject {
	return container.NewPadded(c)
}
