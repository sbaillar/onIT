package main

import (
	"log"
	"net/url"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"

	"onit/internal/busylight"
)

const signInHelp = `## Connect onIT to your Teams presence

onIT ships with a shared app registration, so for most people this is all:

1. Click **Sign in to Microsoft**
2. A code appears - enter it at **microsoft.com/devicelogin**
3. Approve the request (Presence.Read - onIT can only see your presence).
   The approval screen may say "Microsoft Graph Command Line Tools" -
   that is the built-in Microsoft sign-in client onIT uses.

If your organization requires admin approval, the sign-in page says so -
an admin approves once and after that everyone in your org can sign in.

## Advanced: use your own app registration

Only needed if your org blocks third-party apps or you want full control.
In the Azure Portal: **App registrations** -> **New registration**
(name it, leave Redirect URI empty). Then:

- **Authentication** -> **Advanced settings** ->
  **Allow public client flows** = **Yes** -> Save
- **API permissions** -> **Add a permission** -> **Microsoft Graph** ->
  **Delegated** -> **Presence.Read** -> Add (grant admin consent if asked)
- Copy the **Application (client) ID** into the Advanced section below,
  then Sign in. Set Tenant to your Directory (tenant) ID if sign-in asks
  for it; otherwise leave it empty.`

func showGraphSetup(a fyne.App, agent *busylight.Agent, refresh func()) {
	w := a.NewWindow("Presence setup")

	clientID := widget.NewEntry()
	clientID.SetPlaceHolder("default: shared onIT registration")
	clientID.SetText(a.Preferences().String("graphClientID"))
	tenant := widget.NewEntry()
	tenant.SetPlaceHolder("default: organizations")
	tenant.SetText(a.Preferences().String("graphTenant"))

	status := widget.NewLabel("")
	status.Wrapping = fyne.TextWrapWord
	setStatus := func() {
		if agent.Graph.SignedIn() {
			status.SetText("Status: signed in - presence comes from Microsoft Graph")
		} else {
			status.SetText("Status: not signed in - using the legacy Teams local API if available")
		}
	}
	setStatus()

	helpBtn := widget.NewButton("Help...", func() {
		md := widget.NewRichTextFromMarkdown(signInHelp)
		md.Wrapping = fyne.TextWrapWord
		scroll := container.NewScroll(md)
		scroll.SetMinSize(fyne.NewSize(430, 400))
		portal := widget.NewButton("Open Azure Portal", func() {
			u, _ := url.Parse("https://portal.azure.com/#view/Microsoft_AAD_RegisteredApps/ApplicationsListBlade")
			a.OpenURL(u)
		})
		d := dialog.NewCustom("Connecting to Microsoft Graph", "Close",
			container.NewBorder(nil, portal, nil, nil, scroll), w)
		d.Show()
	})

	var signInBtn *widget.Button
	signInBtn = widget.NewButton("Sign in to Microsoft", func() {
		id := strings.TrimSpace(clientID.Text)
		if id == "" {
			id = busylight.DefaultClientID
		}
		if id == "" {
			dialog.ShowInformation("No app registration",
				"This build has no shared registration baked in.\nAdd a Client ID under Advanced (see Help).", w)
			return
		}
		ten := strings.TrimSpace(tenant.Text)
		a.Preferences().SetString("graphClientID", strings.TrimSpace(clientID.Text))
		a.Preferences().SetString("graphTenant", ten)

		dc, err := busylight.StartDeviceLogin(id, ten)
		if err != nil {
			dialog.ShowError(err, w)
			return
		}
		code := widget.NewLabelWithStyle(dc.UserCode, fyne.TextAlignCenter,
			fyne.TextStyle{Bold: true, Monospace: true})
		copyBtn := widget.NewButton("Copy code", func() { a.Clipboard().SetContent(dc.UserCode) })
		openBtn := widget.NewButton("Open sign-in page", func() {
			u, _ := url.Parse(dc.VerificationURI)
			a.OpenURL(u)
		})
		openBtn.Importance = widget.HighImportance
		body := container.NewVBox(
			widget.NewLabel("1. Open the sign-in page\n2. Enter this code\n3. Approve the request"),
			code,
			container.NewGridWithColumns(2, copyBtn, openBtn),
		)
		d := dialog.NewCustom("Sign in to Microsoft", "Cancel", body, w)
		d.SetOnClosed(dc.Cancel)
		d.Show()
		signInBtn.Disable()
		go func() {
			err := agent.Graph.WaitForLogin(dc)
			fyne.Do(func() {
				signInBtn.Enable()
				d.Hide()
				if err != nil {
					log.Printf("graph sign-in: %v", err)
					dialog.ShowError(err, w)
				} else {
					setStatus()
					refresh()
				}
			})
		}()
	})
	signInBtn.Importance = widget.HighImportance

	signOutBtn := widget.NewButton("Sign out", func() {
		agent.Graph.SignOut()
		setStatus()
		refresh()
	})

	advanced := widget.NewAccordion(widget.NewAccordionItem(
		"Advanced: own app registration",
		container.NewVBox(
			widget.NewLabel("Client ID"), clientID,
			widget.NewLabel("Tenant"), tenant,
		),
	))

	w.SetContent(container.NewVBox(
		status,
		signInBtn, signOutBtn,
		widget.NewSeparator(),
		advanced,
		helpBtn,
	))
	w.Resize(fyne.NewSize(400, 0))
	w.Show()
}
