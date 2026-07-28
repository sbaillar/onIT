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
		{"1.18.0", "1.9.9", true}, // numeric, not lexical
		{"1.9.9", "1.18.0", false},
		{"2.0.0", "1.99.99", true},

		// prereleases sort before their release (semver)
		{"1.18.0", "1.18.0-dev1", true},
		{"1.18.0-dev1", "1.18.0", false},
		{"1.18.0-dev2", "1.18.0-dev1", true},
		// numerically, not lexically: "dev10" sorts before "dev9" as a string
		{"1.18.0-dev10", "1.18.0-dev9", true},
		{"1.18.0-dev9", "1.18.0-dev10", false},
		{"1.18.0-beta2", "1.18.0-alpha9", true}, // differing labels: lexical
		{"1.18.0-dev2", "1.18.0-dev2", false},
		{"1.18.0-dev1", "1.17.2", true},
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
