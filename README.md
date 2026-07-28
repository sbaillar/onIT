# onIT

A physical Microsoft Teams busylight: a small round screen on your desk that
shows whether you're available, in a call, or presenting — driven by a menu
bar app that follows your Teams presence.

```
Microsoft Graph (presence) ──► onIT app ──USB serial or BLE──► round display
   or the legacy Teams local API (ws://localhost:8124), if still enabled
```

## Hardware

Both boards are supported; onIT senses which one is connected and flashes
the matching firmware automatically.

| Part | Notes |
|---|---|
| [Waveshare ESP32-S3-Touch-LCD-1.28](https://www.waveshare.com/wiki/ESP32-S3-Touch-LCD-1.28) | 1.28" round IPS LCD (GC9A01, 240×240), CST816S touch, CH343P USB-UART. **Bluetooth (fw 1.11.0+) or USB.** |
| [Waveshare ESP32-S3-Touch-AMOLED-1.75](https://www.waveshare.com/wiki/ESP32-S3-Touch-AMOLED-1.75) | 1.75" round AMOLED (466×466, QSPI), CST9217 touch, native USB. **Bluetooth (preferred) or USB** — runs wireless from any USB-C power source. |
| USB-C cable | Data-capable (powers the device, carries the serial link, and flashes firmware) |

Both boards have the same capabilities — Bluetooth, the standalone clock,
and the emoji roulette. That's a project rule: device features ship on both
boards in the same release, or the release notes say why not.

No soldering, no wiring — flash either board over its USB cable. It then
works untethered: tray → **Pair busylight…**, type the 6-digit
passkey the device shows on its screen into the macOS pairing dialog, and
onIT drives it over an encrypted, bonded BLE link (falling back to USB
whenever it's plugged in and BLE is down). The tray shows which link is
live (`⋆ BLE` / `⚡ USB`).

## Install (macOS)

1. Download the latest **onIT-<version>-macos-arm64.pkg** from the [latest release](../../releases)
   and run it (needs an Apple-silicon Mac on macOS 11+). It installs
   `onIT.app` to /Applications and the headless `onitctl` CLI to
   /usr/local/bin.
2. First launch: right-click `onIT.app` → **Open** (the package is unsigned).
   The app adds itself to login items automatically; the checkbox in the
   window turns that off.
3. Connect a presence source: open onIT → **Presence setup…** and follow the
   built-in guide (register a free Azure app, sign in with a one-time code —
   about 3 minutes). If your Teams still offers the legacy third-party app
   API (Settings → Privacy), onIT uses that automatically instead until you
   sign in to Graph.
4. Plug in the device. If the window shows a firmware update (it will on a
   factory-fresh board), click **Update firmware** — the app flashes the
   bundled firmware over USB in about 30 seconds. Don't unplug during this.
Done — the light now follows your presence, even when you're in a meeting
from another device.

**Updates** come from GitHub Releases: tray → **Check for updates…**.
Settings → **Get beta updates (pre-release)** switches to the beta train,
which also offers prerelease builds — new features before they reach
stable, with less testing behind them. Unticking it stops offering betas;
you keep the build you're on until stable passes it.

The onIT dot lives in the menu bar: it mirrors the light's state, the menu
sets states directly, and "Open onIT" shows the control window (Auto/manual
state buttons, connection status, firmware updates, start-at-login).

Manual states override Teams until you click **Auto (Teams)**; the app
returns to Auto on restart.

## Standalone mode (both boards)

With no computer connected, the board shows a themed **analog clock**
instead of going dark. onIT sets it whenever they're connected — your
timezone (so it handles DST on its own) and the current time — and the
device keeps it from there on its own crystal, drifting only a second or
two a day. There's nothing to configure: no Wi-Fi, no credentials. A power
cut clears it, since neither board has a battery-backed clock, and the face
stays dimmed and hand-less until onIT next connects. **Emoji roulette**:
tap the clock to spin a wheel of your top emojis (synced to the device in
the background over either link) — fast cycling eases out over ~5 s and the
winner stays until you spin again or the computer reconnects. "Spin the
wheel" in the tray does the same while connected. Pairing mode: hold the face for 10
seconds (a progress ring fills), then pair from the tray within 2 minutes.

## States

| State | Trigger (Auto) | Display |
|---|---|---|
| available | free, busy (no call), away, or be-right-back | dark, green ring + dot |
| meeting | in a call or meeting | solid red, mic icon |
| sharing | presenting or do-not-disturb | purple, pulsing ring, "Do not disturb" |
| off | offline / no presence source for 5 s | near-black dotted ring |

## Remote presence (Conditional Access workaround)

If your org requires a managed device for sign-in (error 53003, "device
state: Unregistered"), sign in on a machine that *is* managed and relay
presence to the one with the light:

1. On the light machine: **Settings → Accept remote presence** (opens
   port 8125). A dialog shows the exact command for step 2, including a
   shared token that authenticates the pushes.
2. On the managed machine, with `onitctl` from the release zip/pkg:

   ```
   onitctl -login                                             # browser sign-in, once
   onitctl -forward http://<light-machine>:8125 -token <tok>  # keep running
   ```

The light machine shows the relayed presence within seconds and falls back
to its own sources if the pushes stop. Pass `-client`/`-tenant` to
`onitctl -login` if you use your own app registration. The relay is plain
HTTP carrying a one-word state; the token gates writes — use it on
networks you trust.

## Windows

Download the latest **onIT-<version>-windows-amd64.zip** from the [latest release](../../releases),
extract it anywhere (keep the files together), and run
`onIT.exe`. It lives in the system tray (notification area, bottom right)
with the same state menu, control window, and in-app firmware updates as the
Mac version, and registers itself to start at login on first run (the
checkbox turns that off). The binary is unsigned, so SmartScreen warns once —
**More info → Run anyway**. The device shows up as a COM port automatically
on Windows 10+.

The zip also contains `onitctl.exe`, the headless agent for no-UI
setups (run it via Task Scheduler); flash firmware manually with
`esptool --chip esp32s3 --port COMx --baud 460800 write-flash 0x0 firmware.bin`.

## Building from source

Requirements: Go 1.26+, [arduino-cli](https://arduino.github.io/arduino-cli/)
for firmware work, macOS for the GUI/pkg targets.

```bash
# one-time firmware toolchain setup
arduino-cli core install esp32:esp32
arduino-cli lib install "GFX Library for Arduino" "Adafruit GFX Library" \
  "NimBLE-Arduino" "SensorLib"   # last two for the 1.75" AMOLED sketch

make test       # go vet + unit tests
make build      # dist/onIT (GUI) + dist/onitctl (headless CLI)
make firmware   # compile sketch, refresh the image embedded in the app
make pkg        # dist/onIT-<version>-macos-arm64.pkg (app + onitctl + esptool)
make windows    # dist/onitctl.exe
```

### Signing (macOS)

`make app` signs the bundle with the `onIT Dev` code-signing identity when
one is present in the keychain, and falls back to ad-hoc otherwise. This is
not about Gatekeeper — an unsigned build runs fine after right-click →
**Open** — it's about privacy grants. macOS ties the Bluetooth permission to
the app's *signing identity*, and an ad-hoc signature's identity is its code
hash, so every rebuild looks like a brand-new app and re-prompts. Signing
with a stable certificate makes the grant persist.

To create the identity on a new machine (once):

```bash
openssl req -x509 -newkey rsa:2048 -sha256 -days 3650 -nodes \
  -keyout key.pem -out cert.pem -subj "/CN=onIT Dev/O=onIT" \
  -addext "basicConstraints=critical,CA:false" \
  -addext "keyUsage=critical,digitalSignature" \
  -addext "extendedKeyUsage=critical,codeSigning"
# macOS can't read OpenSSL 3's default PKCS#12 encryption; force the old one
openssl pkcs12 -export -out onit-dev.p12 -inkey key.pem -in cert.pem \
  -name "onIT Dev" -passout pass:onit \
  -keypbe PBE-SHA1-3DES -certpbe PBE-SHA1-3DES -macalg sha1
security import onit-dev.p12 -k ~/Library/Keychains/login.keychain-db \
  -P onit -T /usr/bin/codesign
# self-signed certs need explicit code-signing trust (asks for your password)
security add-trusted-cert -r trustRoot -p codeSign \
  -k ~/Library/Keychains/login.keychain-db cert.pem
security find-identity -v -p codesigning   # should list "onIT Dev"
```

Or copy the identity between machines by exporting it from Keychain Access.
`make app SIGN_ID=` forces an ad-hoc build; `SIGN_ID="Developer ID Application: ..."`
uses a real Apple identity if you have one.

`onitctl -ports` lists serial ports if the device isn't detected
(the app matches USB IDs 303A:1001 and 1A86:55D3 — add yours in
`internal/busylight/light.go`).

## Repo layout

- `firmware/busylight_round/` — 1.28" LCD sketch (Arduino_GFX, NimBLE GATT server, board: ESP32S3 Dev Module)
- `firmware/busylight_round_amoled/` — 1.75" AMOLED sketch (QSPI panel, NimBLE GATT server)
- `internal/busylight/` — agent core: Teams WebSocket client, serial + BLE transports, esptool flashing
- `internal/firmware/` — embedded firmware image + version (regenerated by `make firmware`)
- `cmd/onit/` — Fyne menu bar app · `cmd/onitctl/` — headless CLI
- `test_states.py` — drive the display by hand for firmware development

## Serial protocol (115200 baud)

| Direction | Line | Meaning |
|---|---|---|
| host → device | `STATE:available\|meeting\|sharing\|off` | show a state (host repeats every 2 s; device blanks after 5 s of silence) |
| host → device | `VERSION` | ask for firmware version |
| host → device | `TZ:<posix-tz>` / `TIME:<unix>` | set the standalone clock (sent on connect and every 30 min) |
| host → device | `DECKIMG:<slot>:<last>:<base64>` | upload one roulette-deck image; device replies `DECKOK:<slot>` |
| host → device | `DECKSIG:<sig>` / `DECKSIG` | set / query the deck the device holds, so an unchanged deck isn't re-sent |
| device → host | `VERSION:x.y.z[:board]` | at boot and on query; board tag `lcd128`/`amoled175` (absent on legacy 1.28" firmware) |

Over BLE the same command lines ride a GATT write characteristic, emoji
images use a binary chunked characteristic, and touch/version events arrive
as notifications — see `internal/busylight/ble.go` for the service layout.
