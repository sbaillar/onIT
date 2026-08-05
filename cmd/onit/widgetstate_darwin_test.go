package main

import (
	"encoding/json"
	"testing"
	"time"

	"onit/internal/busylight"
)

func TestWidgetStateJSON(t *testing.T) {
	now := time.Date(2026, 8, 5, 12, 0, 0, 987654321, time.UTC)
	cases := []struct {
		shown string
		want  widgetState
	}{
		{"meeting", widgetState{State: "meeting", Label: "In a call", Color: "#C03048"}},
		// custom states carry their message; the key/color must not fall
		// through to the "off" grey (stateKey normalizes, like the tray)
		{"custom:Lunch break", widgetState{State: "custom", Label: "Custom", Color: "#E8C24A"}},
		// unknown states get the "off" grey rather than a zero color
		{"nonsense", widgetState{State: "nonsense", Label: "Nonsense", Color: "#404040"}},
	}
	for _, c := range cases {
		data, err := widgetStateJSON(busylight.Status{Shown: c.shown, LightConnected: true}, now)
		if err != nil {
			t.Fatal(err)
		}
		var got widgetState
		if err := json.Unmarshal(data, &got); err != nil {
			t.Fatal(err)
		}
		c.want.Connected = true
		c.want.UpdatedAt = now.Truncate(time.Second) // whole seconds for Swift's .iso8601
		if got != c.want {
			t.Errorf("Shown=%q: got %+v, want %+v", c.shown, got, c.want)
		}
	}
}
