package main

import (
	"image/color"
	"strconv"
	"strings"
)

// Custom-message colors ride inside the override payload as
// "RRGGBB,RRGGBB:text" (background, font). A bare message keeps the
// firmware defaults, so older firmware renders it unchanged.
const (
	defaultCustomBg = "E8C24A" // C_YELLOW
	defaultCustomFg = "000000"
)

func isHex6(s string) bool {
	if len(s) != 6 {
		return false
	}
	_, err := strconv.ParseUint(s, 16, 32)
	return err == nil
}

// splitCustom splits an override payload into colors and message.
func splitCustom(payload string) (bg, fg, text string) {
	if len(payload) >= 14 && payload[6] == ',' && payload[13] == ':' &&
		isHex6(payload[:6]) && isHex6(payload[7:13]) {
		return strings.ToUpper(payload[:6]), strings.ToUpper(payload[7:13]), payload[14:]
	}
	return defaultCustomBg, defaultCustomFg, payload
}

// customPayload builds the wire payload, omitting default colors so old
// firmware keeps working until it's flashed.
func customPayload(bg, fg, text string) string {
	if bg == "" {
		bg = defaultCustomBg
	}
	if fg == "" {
		fg = defaultCustomFg
	}
	if strings.EqualFold(bg, defaultCustomBg) && strings.EqualFold(fg, defaultCustomFg) {
		return text
	}
	return strings.ToUpper(bg) + "," + strings.ToUpper(fg) + ":" + text
}

// Per-message color memory: entries are stored as wire payloads
// ("RRGGBB,RRGGBB:text"), newest first, capped so preferences stay small.
const maxRememberedColors = 50

// rememberColors records text's colors (default colors erase the memory).
func rememberColors(list []string, bg, fg, text string) []string {
	out := forgetColors(list, text)
	if strings.EqualFold(bg, defaultCustomBg) && strings.EqualFold(fg, defaultCustomFg) {
		return out
	}
	out = append([]string{customPayload(bg, fg, text)}, out...)
	if len(out) > maxRememberedColors {
		out = out[:maxRememberedColors]
	}
	return out
}

// recallColors looks up remembered colors for text.
func recallColors(list []string, text string) (bg, fg string, ok bool) {
	for _, e := range list {
		if b, f, t := splitCustom(e); t == text {
			return b, f, true
		}
	}
	return "", "", false
}

// forgetColors removes text's memory, if any.
func forgetColors(list []string, text string) []string {
	var out []string
	for _, e := range list {
		if _, _, t := splitCustom(e); t != text {
			out = append(out, e)
		}
	}
	return out
}

// hexColor parses RRGGBB, falling back to the default background.
func hexColor(s string) color.NRGBA {
	v, err := strconv.ParseUint(s, 16, 32)
	if err != nil || len(s) != 6 {
		v, _ = strconv.ParseUint(defaultCustomBg, 16, 32)
	}
	return color.NRGBA{uint8(v >> 16), uint8(v >> 8), uint8(v), 0xFF}
}
