// Package firmware embeds the busylight firmware image built by `make firmware`.
package firmware

import (
	_ "embed"
	"strings"
)

// Bin is the merged flash image (bootloader + partition table + app),
// written to device offset 0x0.
//
//go:embed firmware.bin
var Bin []byte

//go:embed version.txt
var rawVersion string

// Version is FW_VERSION from the sketch this image was built from.
var Version = strings.TrimSpace(rawVersion)
