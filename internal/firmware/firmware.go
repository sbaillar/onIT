// Package firmware embeds the busylight firmware image built by `make firmware`.
package firmware

import (
	_ "embed"
	"fmt"
	"strings"
)

// AmoledBin is the merged flash image (bootloader + partition table + app)
// for the 1.75" AMOLED board, written to device offset 0x0.
//
//go:embed firmware_amoled.bin
var AmoledBin []byte

//go:embed version_amoled.txt
var rawAmoledVersion string

// AmoledVersion is FW_VERSION from the sketch this image was built from.
var AmoledVersion = strings.TrimSpace(rawAmoledVersion)

// ForBoard returns the embedded image and version for a board type as sensed
// from the VERSION handshake or the USB IDs. Only the 1.75" AMOLED board is
// supported; the 1.28" LCD board was dropped after 1.18.0.
func ForBoard(board string) (image []byte, version string, err error) {
	if board != "amoled175" {
		return nil, "", fmt.Errorf("unsupported board type %q (this build supports the 1.75\" AMOLED only)", board)
	}
	if len(AmoledBin) == 0 {
		return nil, "", fmt.Errorf("no firmware image bundled in this build (run `make firmware`)")
	}
	return AmoledBin, AmoledVersion, nil
}
