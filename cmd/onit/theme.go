package main

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
)

// onitTheme makes the window read like the device: the LCD's near-black
// #101018, the firmware palette for states, presence green as the accent.
type onitTheme struct{ base fyne.Theme }

func (t onitTheme) Color(n fyne.ThemeColorName, _ fyne.ThemeVariant) color.Color {
	switch n {
	case theme.ColorNameBackground:
		return color.NRGBA{0x10, 0x10, 0x18, 0xFF}
	case theme.ColorNamePrimary, theme.ColorNameFocus:
		return color.NRGBA{0x90, 0xC4, 0x50, 0xFF} // presence green
	case theme.ColorNameForegroundOnPrimary:
		return color.NRGBA{0x10, 0x10, 0x18, 0xFF}
	case theme.ColorNameButton:
		return color.NRGBA{0x1C, 0x1C, 0x2A, 0xFF}
	case theme.ColorNameInputBackground:
		return color.NRGBA{0x18, 0x18, 0x24, 0xFF}
	case theme.ColorNameSeparator:
		return color.NRGBA{0x2A, 0x2A, 0x3A, 0xFF}
	case theme.ColorNameDisabled: // secondary text (LowImportance labels)
		return color.NRGBA{0x9A, 0x9A, 0xAC, 0xFF}
	case theme.ColorNamePlaceHolder:
		return color.NRGBA{0x6E, 0x6E, 0x82, 0xFF}
	case theme.ColorNameError:
		return color.NRGBA{0xC0, 0x30, 0x48, 0xFF} // device busy red
	}
	return t.base.Color(n, theme.VariantDark)
}

func (t onitTheme) Font(s fyne.TextStyle) fyne.Resource     { return t.base.Font(s) }
func (t onitTheme) Icon(n fyne.ThemeIconName) fyne.Resource { return t.base.Icon(n) }

func (t onitTheme) Size(n fyne.ThemeSizeName) float32 {
	switch n {
	case theme.SizeNameInputRadius, theme.SizeNameSelectionRadius:
		return 8
	}
	return t.base.Size(n)
}
