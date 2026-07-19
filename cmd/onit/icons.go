package main

import (
	"fmt"
	"image/color"
	"strings"

	"fyne.io/fyne/v2"

	"onit/internal/busylight"
)

// Menu bar dot colors, matched to the firmware palette in busylight_round.ino.
var stateColors = map[string]color.NRGBA{
	"available": {0x90, 0xC4, 0x50, 0xFF},
	"meeting":   {0xC0, 0x30, 0x48, 0xFF},
	"sharing":   {0x60, 0x64, 0xA8, 0xFF},
	"custom":    {0xE8, 0xC2, 0x4A, 0xFF},
	"off":       {0x40, 0x40, 0x40, 0xFF},
}

var dots = map[string]fyne.Resource{}

func init() {
	for _, s := range busylight.States {
		if _, ok := stateColors[s]; !ok {
			panic("no tray color for state " + s)
		}
	}
	for s, c := range stateColors {
		dots[s] = fyne.NewStaticResource("dot-"+s+".svg", fmt.Appendf(nil,
			`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 44 44"><circle cx="22" cy="22" r="18.5" fill="#%02X%02X%02X"/></svg>`,
			c.R, c.G, c.B))
	}
}

// stateKey maps a Shown value to its palette key ("custom:msg" -> "custom").
func stateKey(shown string) string {
	if strings.HasPrefix(shown, "custom:") {
		return "custom"
	}
	return shown
}

func dotResource(state string) fyne.Resource {
	if r, ok := dots[state]; ok {
		return r
	}
	return dots["off"]
}
