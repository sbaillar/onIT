package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"onit/internal/busylight"
)

// The macOS widget (widget/Widget.swift) renders from a JSON snapshot the
// app drops on every state change. The path is fixed — the sandboxed widget
// is granted read access to exactly this directory (widget/appex.entitlements).

type widgetState struct {
	State     string    `json:"state"` // raw key; not rendered, kept so the file reads at a glance
	Label     string    `json:"label"` // UI wording, e.g. "In a call"
	Color     string    `json:"color"` // #RRGGBB of the state
	Connected bool      `json:"connected"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func widgetStateJSON(st busylight.Status, now time.Time) ([]byte, error) {
	key := stateKey(st.Shown) // "custom:msg" -> "custom", like the tray dot
	c, ok := stateColors[key]
	if !ok {
		c = stateColors["off"]
	}
	return json.MarshalIndent(widgetState{
		State:     key,
		Label:     stateLabel(key),
		Color:     fmt.Sprintf("#%02X%02X%02X", c.R, c.G, c.B),
		Connected: st.LightConnected,
		// whole seconds so the widget's stock ISO-8601 decoder reads it
		UpdatedAt: now.Truncate(time.Second),
	}, "", "  ")
}

// writeWidgetState snapshots the status for the widget and pokes WidgetKit
// to re-render. Failures only log: the widget is a passenger, never a
// reason the app stumbles.
func writeWidgetState(st busylight.Status) {
	if err := writeWidgetStateFile(st); err != nil {
		log.Printf("widget state: %v", err)
		return
	}
	go reloadWidget()
}

func writeWidgetStateFile(st busylight.Status) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	path := filepath.Join(home, "Library", "Application Support", "onIT", "widget-state.json")
	data, err := widgetStateJSON(st, time.Now())
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	// atomic swap so the widget never reads a half-written file
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// reloadWidget pokes WidgetKit to re-render. WidgetCenter has no ObjC
// surface cgo could reach, so the poke is a tiny embedded Swift helper;
// WidgetKit coalesces bursts on its side.
func reloadWidget() {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	helper := filepath.Join(filepath.Dir(exe), "onit-widgetreload")
	if _, err := os.Stat(helper); err != nil {
		return // dev build without the helper: the widget just polls
	}
	if out, err := exec.Command(helper).CombinedOutput(); err != nil {
		log.Printf("widget reload: %v (%s)", err, out)
	}
}
