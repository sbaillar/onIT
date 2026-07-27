package main

import "testing"

func TestNewerVersion(t *testing.T) {
	cases := []struct {
		a, b string
		want bool // a is newer than b
	}{
		{"1.17.2", "1.17.1", true},
		{"1.17.1", "1.17.2", false},
		{"1.17.2", "1.17.2", false},
		{"1.18.0", "1.9.9", true},   // numeric, not lexical
		{"1.9.9", "1.18.0", false},
		{"2.0.0", "1.99.99", true},

		// prereleases sort before their release (semver)
		{"1.18.0", "1.18.0-dev1", true},
		{"1.18.0-dev1", "1.18.0", false},
		{"1.18.0-dev2", "1.18.0-dev1", true},
		{"1.18.0-dev1", "1.17.2", true},
		{"1.17.2", "1.18.0-dev1", false},

		// a beta user must never be offered the older stable
		{"1.17.2", "1.18.0-dev1", false},

		// unparsable input never claims to be newer
		{"dev", "1.17.2", false},
		{"1.17.2", "dev", false},
	}
	for _, c := range cases {
		if got := newerVersion(c.a, c.b); got != c.want {
			t.Errorf("newerVersion(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}
