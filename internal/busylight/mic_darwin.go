package busylight

/*
#cgo LDFLAGS: -framework CoreAudio
#include <CoreAudio/CoreAudio.h>

static int micRunning() {
	AudioObjectPropertyAddress addr = {
		kAudioHardwarePropertyDefaultInputDevice,
		kAudioObjectPropertyScopeGlobal,
		kAudioObjectPropertyElementMain,
	};
	AudioDeviceID dev = kAudioObjectUnknown;
	UInt32 sz = sizeof(dev);
	if (AudioObjectGetPropertyData(kAudioObjectSystemObject, &addr, 0, NULL, &sz, &dev) != 0 ||
	    dev == kAudioObjectUnknown) {
		return 0;
	}
	AudioObjectPropertyAddress run = {
		kAudioDevicePropertyDeviceIsRunningSomewhere,
		kAudioObjectPropertyScopeGlobal,
		kAudioObjectPropertyElementMain,
	};
	UInt32 running = 0;
	sz = sizeof(running);
	if (AudioObjectGetPropertyData(dev, &run, 0, NULL, &sz, &running) != 0) {
		return 0;
	}
	return running ? 1 : 0;
}
*/
import "C"

// micInUse reports whether any app is capturing from the default microphone.
func micInUse() bool { return C.micRunning() == 1 }
