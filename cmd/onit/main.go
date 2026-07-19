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
	agent := busylight.NewAgent()

	w := a.NewWindow("onIT")
	w.SetFixedSize(true)
	w.SetCloseIntercept(w.Hide)

	teamsLbl := widget.NewLabel("Presence: ...")
	lightLbl := widget.NewLabel("Light: ...")
	modeLbl := widget.NewLabel("Mode: " + autoLabel)

	// one choice list drives both the window buttons and the tray menu
	type choice struct{ label, state string }
	choices := []choice{{autoLabel, ""}}
	for _, s := range busylight.States {
		choices = append(choices, choice{title(s), s})
	}
	btns := make([]*widget.Button, len(choices))
	menuItems := []*fyne.MenuItem{
		fyne.NewMenuItem("Open onIT", func() { w.Show(); w.RequestFocus() }),
		fyne.NewMenuItemSeparator(),
	}
	for i, c := range choices {
		btns[i] = widget.NewButton(c.label, func() { agent.SetOverride(c.state) })
		menuItems = append(menuItems, fyne.NewMenuItem(c.label, func() { agent.SetOverride(c.state) }))
	}

	fwLbl := widget.NewLabel("Firmware: ...")
	updateBtn := widget.NewButton("Update firmware", nil)
	updateBtn.Hide()

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

	desk, isDesk := a.(desktop.App)
	if isDesk {
		desk.SetSystemTrayMenu(fyne.NewMenu("onIT", menuItems...)) // Fyne appends Quit
		desk.SetSystemTrayIcon(dotResource("off"))
	}

	lastShown := ""
	flashing := false
	update = func() {
		st := agent.Status()
		switch {
		case st.TeamsConnected && st.Source == "graph":
			teamsLbl.SetText("Presence: Microsoft Graph")
		case st.TeamsConnected:
			teamsLbl.SetText("Presence: Teams local API")
		default:
			teamsLbl.SetText("Presence: offline")
		}
		if st.LightConnected {
			lightLbl.SetText("Light: connected")
		} else {
			lightLbl.SetText("Light: not found")
		}
		if st.Override == "" {
			modeLbl.SetText("Mode: " + autoLabel)
		} else {
			modeLbl.SetText("Mode: manual (" + st.Override + ")")
		}
		if !flashing {
			switch {
			case !st.LightConnected:
				fwLbl.SetText("Firmware: (no device)")
				updateBtn.Hide()
			case st.DeviceFW == firmware.Version:
				fwLbl.SetText("Firmware: " + st.DeviceFW + " (up to date)")
				updateBtn.Hide()
			case st.DeviceFW == "":
				fwLbl.SetText("Firmware: unknown -> " + firmware.Version)
				updateBtn.Show()
			default:
				fwLbl.SetText("Firmware: " + st.DeviceFW + " -> " + firmware.Version)
				updateBtn.Show()
			}
		}
		if isDesk && st.Shown != lastShown {
			lastShown = st.Shown
			desk.SetSystemTrayIcon(dotResource(st.Shown))
		}
	}
	agent.OnChange(func() { fyne.Do(update) })

	updateBtn.OnTapped = func() {
		flashing = true
		updateBtn.Disable()
		for _, b := range btns {
			b.Disable()
		}
		fwLbl.SetText("Flashing " + firmware.Version + " - do not unplug...")
		go func() {
			err := agent.FlashFirmware(esptoolPath(), firmware.Bin)
			fyne.Do(func() {
				flashing = false
				updateBtn.Enable()
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
	for _, b := range btns[1 : len(btns)-1] { // states except "off"
		grid.Add(b)
	}
	w.SetContent(container.NewVBox(
		teamsLbl, lightLbl, modeLbl,
		widget.NewSeparator(),
		btns[0], // Auto (Teams)
		grid,
		btns[len(btns)-1], // off, full width
		widget.NewSeparator(),
		fwLbl, updateBtn,
		loginCheck,
		graphSetupBtn,
		uninstallBtn,
	))

	uninstallBtn.OnTapped = func() {
		dialog.ShowConfirm("Uninstall onIT",
			"This removes the start-at-login entry, the Teams pairing token,\n"+
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
				if err := removeInstalledFiles(); err != nil {
					log.Printf("uninstall: app files: %v", err)
				}
				done := dialog.NewInformation("Uninstalled", uninstallDoneMsg, w)
				done.SetOnClosed(a.Quit)
				done.Show()
			}, w)
	}
	w.Resize(fyne.NewSize(250, 0)) // height from content; keep it compact

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
