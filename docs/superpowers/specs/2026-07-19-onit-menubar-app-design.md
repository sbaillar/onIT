# onIT — menu bar app + macOS installer

Design approved 2026-07-19. App name per Sonny: **onIT**.

## Goal

A small macOS menu bar app that controls the ESP32-S3 busylight both automatically
(Teams presence) and manually, autostarts at login, and ships as a `.pkg` installer.
Code stays cross-platform (Go + Fyne) so a Windows tray build is possible later.

## Architecture

One Go module, three packages:

- `internal/busylight` — core agent extracted from the existing `main.go`:
  Teams local WebSocket client (localhost:8124, token in `~/.teams_busylight_token`),
  serial `Light` (USB VID match: 303A, 1A86; 115200 baud, `STATE:`/`CMD:` protocol),
  state mapping, plus a new **override**:
  - override `""` = Auto: light follows Teams (`available|meeting|muted|sharing`,
    `off` when Teams is unreachable).
  - override set = manual: light shows that state regardless of Teams.
  - Teams updates keep flowing in the background either way; device taps forward
    `toggle-mute` whenever the Teams socket is up. Stale taps queued while
    disconnected are drained at session start.
  - Heartbeat (2s, firmware watchdog is 5s) moves out of the WS session into the
    agent so manual states survive Teams being down.
  - `Status()` exposes: teams connected, light connected, mode, shown state;
    an on-change callback drives the UI.
- `cmd/teams-busylight` — the existing headless CLI, behavior unchanged
  (kept for Windows/debugging; `-ports` flag included).
- `cmd/onit` — the Fyne GUI. Menu bar icon = colored dot for current state, with
  quick-set menu items, Open, Quit. Small window (~240×220): two status lines
  (Teams / Light), buttons **Auto (Teams)** · Available · Meeting · Muted ·
  Sharing · Off, and a "Start at login" checkbox. Window close hides it;
  no Dock icon (LSUIElement). Override resets to Auto on app restart.

## Autostart

"Start at login" writes/removes `~/Library/LaunchAgents/casa.baillargeon.onit.plist`
(RunAtLoad → the installed app binary). First launch enables it automatically;
the checkbox is the off switch.

## Installer

`make pkg`: `fyne package` → `onIT.app` (Info.plist patched for LSUIElement),
then `pkgbuild --install-location /Applications` → `dist/onIT.pkg`.
Unsigned: first launch needs right-click → Open (Gatekeeper).

## Error handling

Existing reconnect loops (serial re-open on demand, WS retry every 5s), surfaced
as the two status lines instead of logs.

## Testing

- Unit: override-vs-auto resolution, Teams state mapping.
- E2E: fake Teams WS server drives Auto mode against the real device.
- Manual: buttons drive the light; install `.pkg`, confirm menu bar + login item.
