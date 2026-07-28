//go:build !darwin

package main

// Window position is remembered on macOS only: Fyne has no API for it, and
// the workaround is AppKit-specific. Elsewhere the window opens wherever the
// window manager puts it.

func windowOrigin(string) (x, y float64, ok bool) { return 0, 0, false }

func setWindowOrigin(string, float64, float64) bool { return false }
