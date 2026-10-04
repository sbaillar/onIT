//go:build !darwin

package main

import (
	"time"

	"onit/internal/busylight"
)

const widgetRefresh = time.Hour // nothing to keep fresh

// Widgets are macOS-only; elsewhere the state snapshot is a no-op.
func writeWidgetState(busylight.Status) {}
