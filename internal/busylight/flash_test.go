package busylight

import "testing"

func TestPickBoard(t *testing.T) {
	tests := []struct {
		name           string
		handshake, usb string
		force          bool
		want           string // "" = expect an error
	}{
		{"no senses", "", "", false, ""},
		{"handshake only", "lcd128", "", false, "lcd128"},
		{"usb only (blank board)", "", "amoled175", false, "amoled175"},
		{"senses agree", "amoled175", "amoled175", false, "amoled175"},
		{"mismatch refused", "lcd128", "amoled175", false, ""},
		{"mismatch forced: hardware wins", "lcd128", "amoled175", true, "amoled175"},
	}
	for _, tc := range tests {
		got, err := pickBoard(tc.handshake, tc.usb, tc.force)
		if tc.want == "" {
			if err == nil {
				t.Errorf("%s: pickBoard = %q, want error", tc.name, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s: pickBoard error %v, want %q", tc.name, err, tc.want)
		} else if got != tc.want {
			t.Errorf("%s: pickBoard = %q, want %q", tc.name, got, tc.want)
		}
	}
}
