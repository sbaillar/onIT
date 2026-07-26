package firmware

import (
	"strings"
	"testing"
)

func TestForBoard(t *testing.T) {
	img, ver, err := ForBoard("lcd128")
	if err != nil {
		t.Fatalf("ForBoard(lcd128) error: %v", err)
	}
	if len(img) == 0 || ver != Version {
		t.Errorf("ForBoard(lcd128) = %d bytes, %q; want the embedded image and %q", len(img), ver, Version)
	}

	if _, _, err := ForBoard("gc9a01"); err == nil {
		t.Error("ForBoard(gc9a01) = nil error, want unknown board")
	}

	// an amoled image that hasn't been built yet must fail clearly, not flash
	// an empty file
	if len(AmoledBin) == 0 {
		if _, _, err := ForBoard("amoled175"); err == nil || !strings.Contains(err.Error(), "amoled175") {
			t.Errorf("ForBoard(amoled175) with no image = %v, want a clear missing-image error", err)
		}
	}
}
