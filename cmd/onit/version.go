package main

import (
	"strconv"
	"strings"
)

// parseVersion splits "1.18.0-dev1" into its numeric parts and prerelease
// suffix. ok is false for anything that isn't three numbers (e.g. "dev").
func parseVersion(v string) (nums [3]int, pre string, ok bool) {
	core, pre, _ := strings.Cut(v, "-")
	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return nums, "", false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return nums, "", false
		}
		nums[i] = n
	}
	return nums, pre, true
}

// newerVersion reports whether a is a strictly newer version than b.
// Prereleases sort before the release they lead to ("1.18.0-dev1" < "1.18.0"),
// so switching off beta updates never offers the older stable as an "update".
// Unparsable versions are never newer — an unrecognized build stays put.
func newerVersion(a, b string) bool {
	an, apre, aok := parseVersion(a)
	bn, bpre, bok := parseVersion(b)
	if !aok || !bok {
		return false
	}
	for i := range an {
		if an[i] != bn[i] {
			return an[i] > bn[i]
		}
	}
	switch {
	case apre == bpre:
		return false
	case apre == "": // a is the release, b a prerelease of it
		return true
	case bpre == "":
		return false
	}
	return apre > bpre
}
