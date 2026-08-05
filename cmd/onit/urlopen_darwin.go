//go:build darwin

package main

/*
#cgo LDFLAGS: -framework AppKit
// implemented in urlopen_darwin.m
void onitRegisterURLHandler(void);
*/
import "C"

// onURLOpen runs when an onit:// URL is opened while the app is running
// (the widget tap). Set once from main before registration.
var onURLOpen func()

//export onitURLOpened
func onitURLOpened() {
	if onURLOpen != nil {
		onURLOpen()
	}
}

func registerURLHandler(f func()) {
	onURLOpen = f
	C.onitRegisterURLHandler()
}
