package busylight

/*
#cgo LDFLAGS: -framework CoreAudio
#include <CoreAudio/CoreAudio.h>
#include <stdlib.h>
#include <unistd.h>

// othersRunningInput: 1 if a process other than this one is capturing audio
// input, 0 if none, -1 if the per-process list is unavailable (before macOS
// 14). onIT's own meeting recorder holds the mic open, so the device-level
// "running somewhere" flag would read as a call forever once it starts.
static int othersRunningInput() {
	AudioObjectPropertyAddress list = {
		kAudioHardwarePropertyProcessObjectList,
		kAudioObjectPropertyScopeGlobal,
		kAudioObjectPropertyElementMain,
	};
	UInt32 sz = 0;
	if (AudioObjectGetPropertyDataSize(kAudioObjectSystemObject, &list, 0, NULL, &sz) != 0) {
		return -1;
	}
	if (sz == 0) {
		return 0;
	}
	AudioObjectID *procs = malloc(sz);
	if (AudioObjectGetPropertyData(kAudioObjectSystemObject, &list, 0, NULL, &sz, procs) != 0) {
		free(procs);
		return -1;
	}
	int found = 0;
	pid_t me = getpid();
	for (UInt32 i = 0; i < sz / sizeof(AudioObjectID) && !found; i++) {
		AudioObjectPropertyAddress pidA = {
			kAudioProcessPropertyPID, kAudioObjectPropertyScopeGlobal, kAudioObjectPropertyElementMain};
		AudioObjectPropertyAddress inA = {
			kAudioProcessPropertyIsRunningInput, kAudioObjectPropertyScopeGlobal, kAudioObjectPropertyElementMain};
		pid_t pid = 0;
		UInt32 running = 0, psz = sizeof(pid), rsz = sizeof(running);
		if (AudioObjectGetPropertyData(procs[i], &pidA, 0, NULL, &psz, &pid) == 0 && pid != me &&
		    AudioObjectGetPropertyData(procs[i], &inA, 0, NULL, &rsz, &running) == 0 && running) {
			found = 1;
		}
	}
	free(procs);
	return found;
}

static int micRunning() {
	int others = othersRunningInput();
	if (others >= 0) {
		return others;
	}
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

// micInUse reports whether another app is capturing from a microphone
// (onIT's own recorder doesn't count).
func micInUse() bool { return C.micRunning() == 1 }
