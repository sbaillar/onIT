package busylight

import "testing"

func TestIsDeviceFault(t *testing.T) {
	faults := []string{
		"rst:0xc (SW_CPU_RESET),boot:0x8",
		"Guru Meditation Error: Core 1 panic'ed (LoadProhibited)",
		"Backtrace: 0x4200a1b2:0x3fceb2c0",
		"assert failed: xQueueGenericSend queue.c:832",
		"ets Jul 29 2019 12:21:46",
	}
	for _, l := range faults {
		if !isDeviceFault(l) {
			t.Errorf("isDeviceFault(%q) = false, want true", l)
		}
	}
	normal := []string{"VERSION:1.14.2:lcd128", "TOUCH:TAP", "ROULETTE:3", "DECKOK:0"}
	for _, l := range normal {
		if isDeviceFault(l) {
			t.Errorf("isDeviceFault(%q) = true, want false", l)
		}
	}
}
