package main

// Temporary visual check: renders the face for every state to PNGs.
// Run: go test -run TestCaptureFaces -args -capture <dir>

import (
	"flag"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"fyne.io/fyne/v2/driver/software"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
)

var captureDir = flag.String("capture", "", "directory to write face captures")

func TestCaptureFaces(t *testing.T) {
	if *captureDir == "" {
		t.Skip("no -capture dir")
	}
	test.NewApp()
	states := map[string]string{
		"available": "available",
		"meeting":   "meeting",
		"sharing":   "sharing",
		"off":       "off",
		"custom":    "custom:Back at 3pm",
		"custom2":   "custom:Walking the dog around the block",
		"emoji":     "emoji",
	}
	for name, shown := range states {
		f := newDeviceFace()
		f.Set(shown, "heart")
		img := software.Render(f.root, onitTheme{base: theme.DefaultTheme()})
		out, err := os.Create(filepath.Join(*captureDir, name+".png"))
		if err != nil {
			t.Fatal(err)
		}
		if err := png.Encode(out, img); err != nil {
			t.Fatal(err)
		}
		out.Close()
	}
}
