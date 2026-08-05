APP     := onIT
ID      := casa.baillargeon.onit
VERSION := 2.3.0
DIST    := dist
FYNE    := go run fyne.io/tools/cmd/fyne@v1.7.2
GOFLAGS := -trimpath -ldflags "-s -w"
# Code-signing identity for the .app (see README "Signing"). Stable across
# builds, so macOS keeps the Bluetooth grant instead of re-asking each time.
# Empty = ad-hoc, which still runs but re-prompts after every rebuild.
SIGN_ID := $(shell security find-identity -v -p codesigning 2>/dev/null | \
             sed -n 's/.*"\(onIT Dev\)"/\1/p' | head -1)
APPLDFLAGS := -trimpath -ldflags "-s -w -X main.appVersion=$(VERSION)"

ESPTOOL_VERSION := v5.3.1
ESPTOOL := build/tools/esptool
ESPTOOL_WIN := build/tools/esptool.exe
# AMOLED board: 16MB flash + sketch-local partitions.csv. The app fits the
# stock 4MB scheme now that the Wi-Fi stack is gone, but field devices are
# already laid out this way and restacking would wipe their deck partition.
# Octal PSRAM caches the emoji deck so roulette frames never hit flash
# CDCOnBoot=cdc is essential here and only here: this board has no USB-UART
# bridge, so with the default (disabled) Serial goes to UART0's pins and the
# device is mute over USB — it flashes fine and then never answers VERSION.
# The 1.28" board keeps the default: its CH343 bridge sits on UART0.
FQBN_AMOLED := esp32:esp32:esp32s3:FlashSize=16M,PartitionScheme=custom,PSRAM=opi,CDCOnBoot=cdc
SKETCH_AMOLED := firmware/busylight_round_amoled
MINGW   := x86_64-w64-mingw32-gcc

.PHONY: build test app pkg windows windows-gui firmware widget clean

# macOS widget: WidgetKit only speaks Swift, so the extension is the one
# non-Go corner of the app. Built with bare swiftc — no Xcode project.
APPEX      := build/onITWidget.appex
WIDGET_MIN := -target arm64-apple-macosx14.0

widget: $(APPEX) $(DIST)/onit-widgetreload

# -e _NSExtensionMain: WidgetKit appexes are NSExtension plug-ins; the entry
# point is the extension XPC listener, not the Swift-generated main (Xcode's
# widget template links the same way).
$(APPEX): widget/Widget.swift widget/appex-Info.plist Makefile
	rm -rf $(APPEX) && mkdir -p $(APPEX)/Contents/MacOS
	swiftc -O -parse-as-library $(WIDGET_MIN) \
		-Xlinker -e -Xlinker _NSExtensionMain \
		-o $(APPEX)/Contents/MacOS/onITWidget widget/Widget.swift
	cp widget/appex-Info.plist $(APPEX)/Contents/Info.plist
	/usr/libexec/PlistBuddy \
		-c "Set :CFBundleShortVersionString $(VERSION)" \
		-c "Set :CFBundleVersion $(VERSION)" $(APPEX)/Contents/Info.plist

$(DIST)/onit-widgetreload: widget/reload.swift
	mkdir -p $(DIST)
	swiftc -O $(WIDGET_MIN) -o $@ widget/reload.swift

build: $(ESPTOOL)
	go build $(GOFLAGS) -o $(DIST)/onitctl ./cmd/onitctl
	go build $(APPLDFLAGS) -o $(DIST)/onIT ./cmd/onit
	cp -X $(ESPTOOL) $(DIST)/esptool  # so dev builds can flash too

test:
	go vet ./...
	go test ./...

# compile both sketches and refresh the images embedded in the app
# merged.bin is padded with 0xFF to the full flash size (4MB / 16MB); strip the
# trailing erase-value padding before embedding — esptool writes at 0x0 and the
# rest of flash stays erased, so a truncated image flashes identically while
# keeping the embedded blob (and git) to the ~1.4MB of real content.
TRUNC := python3 -c "import sys;p=sys.argv[1];d=open(p,'rb').read().rstrip(b'\xff');open(p,'wb').write(d)"

firmware:
	arduino-cli compile --fqbn $(FQBN_AMOLED) --export-binaries $(SKETCH_AMOLED)
	cp $(SKETCH_AMOLED)/build/esp32.esp32.esp32s3/busylight_round_amoled.ino.merged.bin \
		internal/firmware/firmware_amoled.bin
	$(TRUNC) internal/firmware/firmware_amoled.bin
	sed -n 's/^#define FW_VERSION "\(.*\)".*/\1/p' \
		$(SKETCH_AMOLED)/busylight_round_amoled.ino > internal/firmware/version_amoled.txt

# pinned standalone esptool, bundled into the .app for in-app flashing
$(ESPTOOL):
	mkdir -p build/tools
	curl -sL https://github.com/espressif/esptool/releases/download/$(ESPTOOL_VERSION)/esptool-$(ESPTOOL_VERSION)-macos-arm64.tar.gz \
		| tar -xz -C build/tools
	find build/tools -name esptool -type f -perm +111 -not -path "$(ESPTOOL)" \
		-exec cp {} $(ESPTOOL) \;
	chmod +x $(ESPTOOL)

# onIT.app bundle (menu bar app; LSUIElement hides the Dock icon)
app: $(ESPTOOL) widget
	cd cmd/onit && $(FYNE) package --target darwin --name $(APP) --release \
		--app-id $(ID) --app-version $(VERSION) --icon ../../assets/icon.png
	rm -rf $(DIST)/$(APP).app && mkdir -p $(DIST)
	mv cmd/onit/$(APP).app $(DIST)/
	/usr/libexec/PlistBuddy -c "Add :LSUIElement bool true" \
		$(DIST)/$(APP).app/Contents/Info.plist
	# CoreBluetooth is linked in (BLE support); without this key macOS
	# aborts the app at launch when opened from Finder/LaunchServices
	/usr/libexec/PlistBuddy -c \
		"Add :NSBluetoothAlwaysUsageDescription string 'onIT connects to your busylight over Bluetooth.'" \
		$(DIST)/$(APP).app/Contents/Info.plist
	cp $(ESPTOOL) $(DIST)/$(APP).app/Contents/Resources/esptool
	# the widget extension (WidgetKit appexes are NSExtension plug-ins and
	# live in Contents/PlugIns) and the reload helper the app execs
	mkdir -p $(DIST)/$(APP).app/Contents/PlugIns
	cp -R $(APPEX) $(DIST)/$(APP).app/Contents/PlugIns/
	cp $(DIST)/onit-widgetreload $(DIST)/$(APP).app/Contents/MacOS/
	# onit:// — the widget's tap target
	/usr/libexec/PlistBuddy \
		-c "Add :CFBundleURLTypes array" \
		-c "Add :CFBundleURLTypes:0:CFBundleURLName string $(ID).url" \
		-c "Add :CFBundleURLTypes:0:CFBundleURLSchemes array" \
		-c "Add :CFBundleURLTypes:0:CFBundleURLSchemes:0 string onit" \
		$(DIST)/$(APP).app/Contents/Info.plist
	# Sign last, once the bundle is final — editing it afterwards breaks the
	# seal. macOS ties privacy grants (Bluetooth) to the signing identity, so
	# an ad-hoc signature, whose identity is the code hash, asks again after
	# every rebuild. Signing with a stable identity makes the grant stick.
	# SIGN_ID= signs ad-hoc; see README for creating the certificate.
	# The appex is signed first, with its sandbox entitlements, and the app
	# after it WITHOUT --deep: --deep would re-sign the appex and strip the
	# entitlements, and an unsandboxed widget extension refuses to load.
	@if [ -n "$(SIGN_ID)" ]; then SIGNER="$(SIGN_ID)"; else \
		SIGNER="-"; echo "SIGN_ID unset: signing ad-hoc"; fi; \
	echo "codesign --sign '$$SIGNER' (appex, then app)"; \
	codesign --force --options runtime \
		--entitlements widget/appex.entitlements --sign "$$SIGNER" \
		$(DIST)/$(APP).app/Contents/PlugIns/$(notdir $(APPEX)) || exit 1; \
	codesign --force --options runtime --sign "$$SIGNER" \
		$(DIST)/$(APP).app || exit 1

# macOS installer: onIT.app + headless CLI in /usr/local/bin
# (unsigned: first launch needs right-click > Open)
pkg: app build
	rm -rf build/pkgroot
	mkdir -p build/pkgroot/Applications build/pkgroot/usr/local/bin
	cp -RX $(DIST)/$(APP).app build/pkgroot/Applications/
	cp -X $(DIST)/onitctl build/pkgroot/usr/local/bin/
	xattr -rc build/pkgroot
	# Pin the install location. By default the installer treats an app bundle
	# as relocatable: if LaunchServices knows the same bundle id somewhere
	# else, it overwrites *that* copy and /Applications stays empty. Anyone
	# who has ever run a build from another directory hits this — it landed a
	# release in the source tree's dist/ instead of /Applications.
	pkgbuild --analyze --root build/pkgroot build/component.plist
	/usr/libexec/PlistBuddy -c "Set :0:BundleIsRelocatable false" build/component.plist
	COPYFILE_DISABLE=1 pkgbuild --root build/pkgroot --install-location / \
		--component-plist build/component.plist \
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
