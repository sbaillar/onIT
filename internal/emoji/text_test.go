package emoji

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/png"
	"math"
	"strings"
	"testing"
)

func TestFitTextScalesWithLength(t *testing.T) {
	shortSize, shortLines, err := fitText("OK")
	if err != nil {
		t.Fatal(err)
	}
	if len(shortLines) != 1 {
		t.Errorf("lines(OK) = %q, want one line", shortLines)
	}
	if shortSize < 30 {
		t.Errorf("size(OK) = %v, want a big font (>= 30)", shortSize)
	}

	long := "back in five minutes please hold on"
	longSize, longLines, err := fitText(long)
	if err != nil {
		t.Fatal(err)
	}
	if len(longLines) < 2 {
		t.Errorf("lines(long) = %q, want wrapped", longLines)
	}
	if longSize >= shortSize {
		t.Errorf("size(long) = %v, want smaller than size(OK) = %v", longSize, shortSize)
	}
	if got := strings.Join(longLines, " "); got != long {
		t.Errorf("wrapped text = %q, want all words in order (%q)", got, long)
	}
}

func TestFitTextRejectsEmptyAndAbsurd(t *testing.T) {
	if _, _, err := fitText("  "); err == nil {
		t.Error("fitText accepted blank text")
	}
	if _, _, err := fitText(strings.Repeat("M", 200)); err == nil {
		t.Error("fitText accepted an unbreakable 200-char word")
	}
}

func TestTextPayloadStaysInsideTheCircle(t *testing.T) {
	b64, pngBytes, err := TextPayload("Back in 5")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) != Size*Size*2 {
		t.Fatalf("payload = %d bytes, want %d", len(raw), Size*Size*2)
	}

	img, err := png.Decode(bytes.NewReader(pngBytes))
	if err != nil {
		t.Fatal(err)
	}
	if img.Bounds().Dx() != Size || img.Bounds().Dy() != Size {
		t.Fatalf("png is %v, want %dx%d", img.Bounds().Size(), Size, Size)
	}

	lit := 0
	c := float64(Size) / 2
	for y := 0; y < Size; y++ {
		for x := 0; x < Size; x++ {
			r, g, b, _ := img.At(x, y).RGBA()
			if r|g|b == 0 {
				continue
			}
			lit++
			// every non-black pixel must be inside the usable circle
			d := math.Hypot(float64(x)+0.5-c, float64(y)+0.5-c)
			if d > c {
				t.Fatalf("lit pixel at (%d,%d) outside the circle (d=%.1f)", x, y, d)
			}
		}
	}
	if lit == 0 {
		t.Error("rendered image is entirely black")
	}
}

func TestTextPayloadMatchesImage(t *testing.T) {
	b64, pngBytes, err := TextPayload("hi")
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(pngBytes))
	if err != nil {
		t.Fatal(err)
	}
	if got := rgb565Base64(img.(image.Image)); got != b64 {
		t.Error("payload does not match the PNG rendering")
	}
}
