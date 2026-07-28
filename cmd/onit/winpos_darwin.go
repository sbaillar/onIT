//go:build darwin

package main

/*
#cgo LDFLAGS: -framework AppKit
#include <stdlib.h>

// implemented in winpos_darwin.m — the bodies are Objective-C, which cgo
// cannot compile in a preamble
int onitWindowOrigin(const char *title, double *x, double *y);
int onitSetWindowOrigin(const char *title, double x, double y);
*/
import "C"

import "unsafe"

// windowOrigin reports the window's on-screen position; false when no window
// with that title exists yet.
func windowOrigin(title string) (x, y float64, ok bool) {
	t := C.CString(title)
	defer C.free(unsafe.Pointer(t))
	var cx, cy C.double
	if C.onitWindowOrigin(t, &cx, &cy) == 0 {
		return 0, 0, false
	}
	return float64(cx), float64(cy), true
}

// setWindowOrigin moves the window, ignoring a position that no longer lands
// on an attached screen.
func setWindowOrigin(title string, x, y float64) bool {
	t := C.CString(title)
	defer C.free(unsafe.Pointer(t))
	return C.onitSetWindowOrigin(t, C.double(x), C.double(y)) != 0
}
