// Package emoji embeds the curated emoji images the app can send to the
// device (rendered at build time; the firmware has no emoji fonts).
package emoji

import (
	"bytes"
	"embed"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"image"
	_ "image/png"
)

//go:embed img/*.png
var files embed.FS

// Names in picker order.
var Names = []string{
	"thumbsup", "heart", "laugh", "coffee",
	"pizza", "headphones", "sleep", "noentry",
	"phone", "check", "fire", "party",
	"shh", "brain", "run", "beer",
}

// Size is the square pixel size of every embedded emoji.
const Size = 120

// PNG returns the embedded image (for UI buttons), nil if unknown.
func PNG(name string) []byte {
	b, err := files.ReadFile("img/" + name + ".png")
	if err != nil {
		return nil
	}
	return b
}

// RGB565Base64 returns the EMOJI: wire payload: Size x Size RGB565
// little-endian pixels, base64 encoded.
func RGB565Base64(name string) (string, error) {
	b := PNG(name)
	if b == nil {
		return "", fmt.Errorf("unknown emoji %q", name)
	}
	img, _, err := image.Decode(bytes.NewReader(b))
	if err != nil {
		return "", err
	}
	if img.Bounds().Dx() != Size || img.Bounds().Dy() != Size {
		return "", fmt.Errorf("emoji %s is %v, want %dx%d", name, img.Bounds().Size(), Size, Size)
	}
	raw := make([]byte, 0, Size*Size*2)
	var px [2]byte
	for y := 0; y < Size; y++ {
		for x := 0; x < Size; x++ {
			r, g, bl, _ := img.At(img.Bounds().Min.X+x, img.Bounds().Min.Y+y).RGBA()
			v := uint16(r>>11)<<11 | uint16(g>>10)<<5 | uint16(bl>>11)
			binary.LittleEndian.PutUint16(px[:], v)
			raw = append(raw, px[0], px[1])
		}
	}
	return base64.StdEncoding.EncodeToString(raw), nil
}
