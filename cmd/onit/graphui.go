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

const registrationHelp = `## Connect onIT to Microsoft Graph

onIT reads your Teams presence from Microsoft Graph. That needs a free,
one-time "app registration" in your Microsoft account — about 3 minutes.

### 1 · Register the app

Click **Open Azure Portal** below (sign in with your **work account**), then:

- Search for **App registrations** → **New registration**
- **Name:** onIT
- **Supported account types:** *Accounts in this organizational directory only*
- Leave Redirect URI empty → **Register**

### 2 · Copy the Client ID

On the app's **Overview** page, copy **Application (client) ID**
and paste it into the Client ID field in onIT.
(Tenant can stay empty unless your admin tells you otherwise.)

### 3 · Allow device sign-in

**Authentication** → scroll to **Advanced settings** →
set **Allow public client flows** to **Yes** → **Save**.
*(Without this, sign-in fails with an error about public clients.)*

### 4 · Grant the permission

**API permissions** → **Add a permission** → **Microsoft Graph** →
**Delegated permissions** → search **Presence** → tick **Presence.Read** →
**Add permissions**.

If the status column says *Not granted*, click
**Grant admin consent** (or ask your admin to).

### 5 · Sign in

Back in onIT: **Sign in** → a code appears → enter it at
microsoft.com/devicelogin → approve. Done — the light now follows
your presence anywhere Teams runs, even on your phone.`

func showGraphSetup(a fyne.App, agent *busylight.Agent, refresh func()) {
	w := a.NewWindow("Presence setup")

	clientID := widget.NewEntry()
	clientID.SetPlaceHolder("Application (client) ID")
	clientID.SetText(a.Preferences().String("graphClientID"))
	tenant := widget.NewEntry()
	tenant.SetPlaceHolder("Tenant ID (optional)")
	tenant.SetText(a.Preferences().String("graphTenant"))

	status := widget.NewLabel("")
	setStatus := func() {
		if agent.Graph.SignedIn() {
			status.SetText("Status: signed in — presence comes from Microsoft Graph")
		} else {
			status.SetText("Status: not signed in — using the legacy Teams local API")
		}
	}
	setStatus()

	helpBtn := widget.NewButton("Setup guide (register the Azure app)…", func() {
		md := widget.NewRichTextFromMarkdown(registrationHelp)
		md.Wrapping = fyne.TextWrapWord
		scroll := container.NewScroll(md)
		scroll.SetMinSize(fyne.NewSize(430, 420))
		portal := widget.NewButton("Open Azure Portal", func() {
			u, _ := url.Parse("https://portal.azure.com/#view/Microsoft_AAD_RegisteredApps/ApplicationsListBlade")
			a.OpenURL(u)
		})
		portal.Importance = widget.HighImportance
		d := dialog.NewCustom("How to set up Microsoft Graph", "Close",
			container.NewBorder(nil, portal, nil, nil, scroll), w)
		d.Show()
	})

	var signInBtn *widget.Button
	signInBtn = widget.NewButton("Sign in to Microsoft", func() {
		id := strings.TrimSpace(clientID.Text)
		if id == "" {
			dialog.ShowInformation("Client ID needed",
				"Paste your Azure app's Application (client) ID first.\nThe setup guide shows where to find it.", w)
			return
		}
		ten := strings.TrimSpace(tenant.Text)
		a.Preferences().SetString("graphClientID", id)
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

	signOutBtn := widget.NewButton("Sign out", func() {
		agent.Graph.SignOut()
		setStatus()
		refresh()
	})

	w.SetContent(container.NewVBox(
		status,
		widget.NewSeparator(),
		widget.NewLabel("Client ID"), clientID,
		widget.NewLabel("Tenant (optional)"), tenant,
		signInBtn, signOutBtn,
		widget.NewSeparator(),
		helpBtn,
	))
	w.Resize(fyne.NewSize(400, 0))
	w.Show()
}
