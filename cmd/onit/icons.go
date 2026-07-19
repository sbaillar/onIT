package main

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"math"

	"fyne.io/fyne/v2"
)

// Menu bar dot colors, matched to the firmware palette in busylight_round.ino.
var stateColors = map[string]color.NRGBA{
	"available": {0x90, 0xC4, 0x50, 0xFF},
	"meeting":   {0xC0, 0x30, 0x48, 0xFF},
	"muted":     {0xE0, 0x50, 0x70, 0xFF},
	"sharing":   {0x60, 0x64, 0xA8, 0xFF},
	"off":       {0x40, 0x40, 0x40, 0xFF},
}

var dotCache = map[string]fyne.Resource{}

// dotResource returns a filled-circle icon for a state (menu bar sized).
func dotResource(state string) fyne.Resource {
	if r, ok := dotCache[state]; ok {
		return r
	}
	c, ok := stateColors[state]
	if !ok {
		c = stateColors["off"]
	}
	r := fyne.NewStaticResource("dot-"+state+".png", drawDot(44, c))
	dotCache[state] = r
	return r
}

// drawDot renders a centered filled circle with a soft edge.
func drawDot(size int, c color.NRGBA) []byte {
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	cx, cy := float64(size-1)/2, float64(size-1)/2
	radius := float64(size) * 0.42
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			d := math.Hypot(float64(x)-cx, float64(y)-cy)
			a := radius + 0.5 - d // 1px anti-aliased edge
			if a <= 0 {
				continue
			}
			if a > 1 {
				a = 1
			}
			px := c
			px.A = uint8(float64(c.A) * a)
			img.SetNRGBA(x, y, px)
		}
	}
	var buf bytes.Buffer
	png.Encode(&buf, img)
	return buf.Bytes()
}
