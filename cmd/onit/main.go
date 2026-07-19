// onIT — menu bar app for the Teams busylight.
package main

import (
	"flag"
	"log"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/widget"

	"onit/internal/busylight"
)

func title(s string) string {
	return strings.ToUpper(s[:1]) + s[1:]
}

func main() {
	hidden := flag.Bool("hidden", false, "start without showing the window (login item)")
	flag.Parse()

	a := app.NewWithID("casa.baillargeon.onit")
	agent := busylight.NewAgent()

	w := a.NewWindow("onIT")
	w.SetFixedSize(true)
	w.SetCloseIntercept(w.Hide)

	teamsLbl := widget.NewLabel("Teams: …")
	lightLbl := widget.NewLabel("Light: …")
	modeLbl := widget.NewLabel("Mode: Auto (Teams)")

	autoBtn := widget.NewButton("Auto (Teams)", func() { agent.SetOverride("") })
	stateBtns := make([]*widget.Button, len(busylight.States))
	for i, s := range busylight.States {
		state := s
		stateBtns[i] = widget.NewButton(title(state), func() { agent.SetOverride(state) })
	}

	loginCheck := widget.NewCheck("Start at login", nil)
	loginCheck.SetChecked(autostartEnabled())
	loginCheck.OnChanged = func(on bool) {
		if err := setAutostart(on); err != nil {
			log.Printf("autostart update failed: %v", err)
		}
	}

	// menu bar: quick-set entries + Open (Fyne appends Quit itself)
	desk, isDesk := a.(desktop.App)
	if isDesk {
		items := []*fyne.MenuItem{
			fyne.NewMenuItem("Open onIT", func() { w.Show(); w.RequestFocus() }),
			fyne.NewMenuItemSeparator(),
			fyne.NewMenuItem("Auto (Teams)", func() { agent.SetOverride("") }),
		}
		for _, s := range busylight.States {
			state := s
			items = append(items, fyne.NewMenuItem(title(state), func() { agent.SetOverride(state) }))
		}
		desk.SetSystemTrayMenu(fyne.NewMenu("onIT", items...))
		desk.SetSystemTrayIcon(dotResource("off"))
	}

	lastShown := ""
	update := func() {
		st := agent.Status()
		if st.TeamsConnected {
			teamsLbl.SetText("Teams: connected")
		} else {
			teamsLbl.SetText("Teams: offline")
		}
		if st.LightConnected {
			lightLbl.SetText("Light: connected")
		} else {
			lightLbl.SetText("Light: not found")
		}
		if st.Override == "" {
			modeLbl.SetText("Mode: Auto (Teams)")
		} else {
			modeLbl.SetText("Mode: manual (" + st.Override + ")")
		}
		if isDesk && st.Shown != lastShown {
			lastShown = st.Shown
			desk.SetSystemTrayIcon(dotResource(st.Shown))
		}
	}
	agent.OnChange(func() { fyne.Do(update) })

	w.SetContent(container.NewVBox(
		teamsLbl, lightLbl, modeLbl,
		widget.NewSeparator(),
		autoBtn,
		container.NewGridWithColumns(2,
			stateBtns[0], stateBtns[1], stateBtns[2], stateBtns[3]),
		stateBtns[4], // off, full width
		widget.NewSeparator(),
		loginCheck,
	))
	w.Resize(fyne.NewSize(250, 0)) // height from content; keep it compact

	// first launch: install the login item (checkbox is the off switch)
	if !a.Preferences().Bool("autostartConfigured") {
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
