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
	return laterPrerelease(apre, bpre)
}

// laterPrerelease orders two prerelease suffixes. Suffixes that differ only in
// a trailing number compare on that number — lexically "dev10" sorts before
// "dev9", which would strand a dev9 tester on the older build. Anything else
// falls back to a plain string compare.
func laterPrerelease(a, b string) bool {
	aw, an := splitTrailingNumber(a)
	bw, bn := splitTrailingNumber(b)
	if aw == bw && an >= 0 && bn >= 0 {
		return an > bn
	}
	return a > b
}

// splitTrailingNumber splits "dev10" into "dev" and 10; the number is -1 when
// there isn't one.
func splitTrailingNumber(s string) (string, int) {
	i := len(s)
	for i > 0 && s[i-1] >= '0' && s[i-1] <= '9' {
		i--
	}
	if i == len(s) {
		return s, -1
	}
	n, err := strconv.Atoi(s[i:])
	if err != nil {
		return s, -1
	}
	return s[:i], n
}
