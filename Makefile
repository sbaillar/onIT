APP     := onIT
ID      := casa.baillargeon.onit
VERSION := 1.0.0
DIST    := dist
FYNE    := go run fyne.io/tools/cmd/fyne@latest

.PHONY: build test app pkg windows clean

build:
	go build -o $(DIST)/teams-busylight ./cmd/teams-busylight
	go build -o $(DIST)/onit ./cmd/onit

test:
	go vet ./...
	go test ./...

# onIT.app bundle (menu bar app; LSUIElement hides the Dock icon)
app:
	cd cmd/onit && $(FYNE) package --target darwin --name $(APP) \
		--app-id $(ID) --app-version $(VERSION) --icon ../../assets/icon.png
	rm -rf $(DIST)/$(APP).app && mkdir -p $(DIST)
	mv cmd/onit/$(APP).app $(DIST)/
	/usr/libexec/PlistBuddy -c "Add :LSUIElement bool true" \
		$(DIST)/$(APP).app/Contents/Info.plist 2>/dev/null || \
	/usr/libexec/PlistBuddy -c "Set :LSUIElement true" \
		$(DIST)/$(APP).app/Contents/Info.plist

# macOS installer (unsigned: first launch needs right-click > Open)
pkg: app
	pkgbuild --component $(DIST)/$(APP).app --install-location /Applications \
		--identifier $(ID) --version $(VERSION) $(DIST)/$(APP).pkg

# headless agent for Windows (GUI needs fyne-cross or a Windows box)
windows:
	GOOS=windows GOARCH=amd64 go build -o $(DIST)/teams-busylight.exe ./cmd/teams-busylight

clean:
	rm -rf $(DIST)
