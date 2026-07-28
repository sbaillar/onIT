package firmware

import "testing"

func TestForBoard(t *testing.T) {
	img, ver, err := ForBoard("amoled175")
	if err != nil {
		t.Fatalf("ForBoard(amoled175) error: %v", err)
	}
	if len(img) == 0 || ver != AmoledVersion {
		t.Errorf("ForBoard(amoled175) = %d bytes, %q; want the embedded image and %q",
			len(img), ver, AmoledVersion)
	}

	// the 1.28" board was dropped after 1.18.0: sensing one must refuse
	// rather than flash the AMOLED image onto it
	for _, board := range []string{"lcd128", "gc9a01", ""} {
		if _, _, err := ForBoard(board); err == nil {
			t.Errorf("ForBoard(%q) = nil error, want unsupported board", board)
		}
	}
}
