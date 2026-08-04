package main

import "testing"

func TestBundlePlistFor(t *testing.T) {
	cases := []struct {
		exe, want string
	}{
		{"/Applications/onIT.app/Contents/MacOS/onit", "/Applications/onIT.app/Contents/Info.plist"},
		{"/Users/x/src/onIT/dist/onIT", ""},
		{"/tmp/onit", ""},
		{"onit", ""},
	}
	for _, c := range cases {
		if got := bundlePlistFor(c.exe); got != c.want {
			t.Errorf("bundlePlistFor(%q) = %q, want %q", c.exe, got, c.want)
		}
	}
}
