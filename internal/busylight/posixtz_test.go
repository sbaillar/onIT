package busylight

import (
	"testing"
	"time"
)

func TestPosixTZ(t *testing.T) {
	tests := []struct {
		zone string
		want string
	}{
		{"America/New_York", "EST5EDT,M3.2.0,M11.1.0"},
		{"UTC", "UTC0"},
		{"America/Phoenix", "MST7"}, // no DST
		{"Europe/London", "GMT0BST,M3.5.0/1,M10.5.0"},
	}
	for _, tt := range tests {
		t.Run(tt.zone, func(t *testing.T) {
			loc, err := time.LoadLocation(tt.zone)
			if err != nil {
				t.Fatalf("LoadLocation(%s): %v", tt.zone, err)
			}
			if got := posixTZ(loc, 2026); got != tt.want {
				t.Errorf("posixTZ(%s, 2026) = %q, want %q", tt.zone, got, tt.want)
			}
		})
	}
}

func TestPosixTZFixedOffset(t *testing.T) {
	// a zone with no DST and a non-alphabetic abbreviation gets quoted
	loc := time.FixedZone("+0530", 5*3600+30*60)
	if got, want := posixTZ(loc, 2026), "<+0530>-5:30"; got != want {
		t.Errorf("posixTZ(+0530) = %q, want %q", got, want)
	}
}
