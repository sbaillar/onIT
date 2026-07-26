// Package firmware embeds the busylight firmware images built by `make firmware`.
package firmware

import (
	_ "embed"
	"fmt"
	"strings"
)

// Bin is the merged flash image (bootloader + partition table + app) for the
// 1.28" LCD board, written to device offset 0x0.
//
//go:embed firmware.bin
var Bin []byte

//go:embed version.txt
var rawVersion string

// Version is FW_VERSION from the sketch this image was built from.
var Version = strings.TrimSpace(rawVersion)

// AmoledBin is the merged flash image for the 1.75" AMOLED board — empty
// until `make firmware` has built the busylight_round_amoled sketch.
//
//go:embed firmware_amoled.bin
var AmoledBin []byte

//go:embed version_amoled.txt
var rawAmoledVersion string

// AmoledVersion is FW_VERSION from the amoled sketch ("" until built).
var AmoledVersion = strings.TrimSpace(rawAmoledVersion)

// ForBoard returns the embedded image and version for a board type as sensed
// from the VERSION handshake or USB IDs ("lcd128" or "amoled175").
func ForBoard(board string) (image []byte, version string, err error) {
	switch board {
	case "lcd128":
		image, version = Bin, Version
	case "amoled175":
		image, version = AmoledBin, AmoledVersion
	default:
		return nil, "", fmt.Errorf("unknown board type %q", board)
	}
	if len(image) == 0 {
		return nil, "", fmt.Errorf("no %s firmware image bundled in this build (run `make firmware`)", board)
	}
	return image, version, nil
}
