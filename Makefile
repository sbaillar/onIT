APP     := onIT
ID      := casa.baillargeon.onit
VERSION := 1.9.1
DIST    := dist
FYNE    := go run fyne.io/tools/cmd/fyne@v1.7.2
GOFLAGS := -trimpath -ldflags "-s -w"
APPLDFLAGS := -trimpath -ldflags "-s -w -X main.appVersion=$(VERSION)"

ESPTOOL_VERSION := v5.3.1
ESPTOOL := build/tools/esptool
ESPTOOL_WIN := build/tools/esptool.exe
FQBN    := esp32:esp32:esp32s3
SKETCH  := firmware/busylight_round
MINGW   := x86_64-w64-mingw32-gcc

.PHONY: build test app pkg windows windows-gui firmware clean

build: $(ESPTOOL)
	go build $(GOFLAGS) -o $(DIST)/onitctl ./cmd/onitctl
	go build $(APPLDFLAGS) -o $(DIST)/onIT ./cmd/onit
	cp -X $(ESPTOOL) $(DIST)/esptool  # so dev builds can flash too

test:
	go vet ./...
	go test ./...

# compile the sketch and refresh the image embedded in the app
firmware:
	arduino-cli compile --fqbn $(FQBN) --export-binaries $(SKETCH)
	cp $(SKETCH)/build/esp32.esp32.esp32s3/busylight_round.ino.merged.bin \
		internal/firmware/firmware.bin
	sed -n 's/^#define FW_VERSION "\(.*\)".*/\1/p' \
		$(SKETCH)/busylight_round.ino > internal/firmware/version.txt

# pinned standalone esptool, bundled into the .app for in-app flashing
$(ESPTOOL):
	mkdir -p build/tools
	curl -sL https://github.com/espressif/esptool/releases/download/$(ESPTOOL_VERSION)/esptool-$(ESPTOOL_VERSION)-macos-arm64.tar.gz \
		| tar -xz -C build/tools
	find build/tools -name esptool -type f -perm +111 -not -path "$(ESPTOOL)" \
		-exec cp {} $(ESPTOOL) \;
	chmod +x $(ESPTOOL)

# onIT.app bundle (menu bar app; LSUIElement hides the Dock icon)
app: $(ESPTOOL)
	cd cmd/onit && $(FYNE) package --target darwin --name $(APP) --release \
		--app-id $(ID) --app-version $(VERSION) --icon ../../assets/icon.png
	rm -rf $(DIST)/$(APP).app && mkdir -p $(DIST)
	mv cmd/onit/$(APP).app $(DIST)/
	/usr/libexec/PlistBuddy -c "Add :LSUIElement bool true" \
		$(DIST)/$(APP).app/Contents/Info.plist
	cp $(ESPTOOL) $(DIST)/$(APP).app/Contents/Resources/esptool

# macOS installer: onIT.app + headless CLI in /usr/local/bin
# (unsigned: first launch needs right-click > Open)
pkg: app build
	rm -rf build/pkgroot
	mkdir -p build/pkgroot/Applications build/pkgroot/usr/local/bin
	cp -RX $(DIST)/$(APP).app build/pkgroot/Applications/
	cp -X $(DIST)/onitctl build/pkgroot/usr/local/bin/
	xattr -rc build/pkgroot
	COPYFILE_DISABLE=1 pkgbuild --root build/pkgroot --install-location / \
		--identifier $(ID) --version $(VERSION) $(DIST)/$(APP)-$(VERSION)-macos-arm64.pkg

# headless agent for Windows
windows:
	GOOS=windows GOARCH=amd64 go build $(GOFLAGS) -o $(DIST)/onitctl.exe ./cmd/onitctl

$(ESPTOOL_WIN):
	mkdir -p build/tools
	curl -sL -o build/tools/esptool-win.zip \
		https://github.com/espressif/esptool/releases/download/$(ESPTOOL_VERSION)/esptool-$(ESPTOOL_VERSION)-windows-amd64.zip
	unzip -o -q build/tools/esptool-win.zip -d build/tools/esptool-win
	find build/tools/esptool-win -name esptool.exe -exec cp {} $(ESPTOOL_WIN) \;

# Windows tray app + headless CLI + esptool, zipped
# (CGO via mingw-w64; -H=windowsgui hides the console)
windows-gui: $(ESPTOOL_WIN) windows
	CGO_ENABLED=1 CC=$(MINGW) GOOS=windows GOARCH=amd64 \
		go build -trimpath -ldflags "-s -w -H=windowsgui -X main.appVersion=$(VERSION)" -o $(DIST)/onIT.exe ./cmd/onit
	cd $(DIST) && rm -f onIT-$(VERSION)-windows-amd64.zip && \
		cp ../$(ESPTOOL_WIN) esptool.exe && \
		zip -q onIT-$(VERSION)-windows-amd64.zip onIT.exe esptool.exe onitctl.exe && \
		rm esptool.exe

clean:
	rm -rf $(DIST)
