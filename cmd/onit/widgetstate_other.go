//go:build !darwin

package main

import "onit/internal/busylight"

// Widgets are macOS-only; elsewhere the state snapshot is a no-op.
func writeWidgetState(busylight.Status) {}
