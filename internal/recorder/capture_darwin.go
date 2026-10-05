//go:build darwin

package recorder

/*
#cgo CFLAGS: -x objective-c -fobjc-arc -mmacosx-version-min=11.0
#cgo LDFLAGS: -framework CoreAudio -framework Foundation
// implemented in capture_darwin.m
int onitRecStart(double *rate, char *err, int errlen);
int onitRecRead(float *dst, int max);
void onitRecStop(void);
int onitRecDropped(void);
*/
import "C"

import (
	"errors"
	"unsafe"
)

// Supported: this build has a capture backend (it still needs macOS 14.2+).
const Supported = true

func captureStart() (float64, error) {
	var rate C.double
	var buf [256]C.char
	if C.onitRecStart(&rate, &buf[0], C.int(len(buf))) != 0 {
		return 0, errors.New(C.GoString(&buf[0]))
	}
	return float64(rate), nil
}

// captureRead fills buf (interleaved mic, call) and returns frames read.
func captureRead(buf []float32) int {
	return int(C.onitRecRead((*C.float)(unsafe.Pointer(&buf[0])), C.int(len(buf)/2)))
}

func captureStop() { C.onitRecStop() }

func captureDropped() int { return int(C.onitRecDropped()) }
