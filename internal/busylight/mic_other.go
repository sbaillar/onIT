//go:build !darwin && !windows

package busylight

func micInUse() bool { return false }
