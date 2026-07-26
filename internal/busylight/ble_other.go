//go:build !darwin

package busylight

import (
	"context"
	"errors"
)

var errBLEUnavailable = errors.New("BLE is not available on this platform")

func newBLELink(deviceID string, onEvent func(line string)) bleLink { return nil }

func blePair(ctx context.Context, choose func(BLEDevice) bool) (BLEDevice, error) {
	return BLEDevice{}, errBLEUnavailable
}
