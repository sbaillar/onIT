package busylight

import (
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"time"

	"go.bug.st/serial/enumerator"

	"onit/internal/firmware"
)

// portBoard returns the board type implied by the port's USB bridge chip:
// the CH343 bridge is the 1.28" LCD board, native ESP32-S3 USB the 1.75"
// AMOLED. "" when the port is unknown. This is the fallback sense for a
// blank/unresponsive board that never answers VERSION.
func portBoard(name string) string {
	ports, err := enumerator.GetDetailedPortsList()
	if err != nil {
		return ""
	}
	for _, p := range ports {
		if p.Name != name || !p.IsUSB {
			continue
		}
		switch strings.ToUpper(p.VID) + ":" + strings.ToUpper(p.PID) {
		case "1A86:55D3":
			return "lcd128"
		case "303A:1001":
			return "amoled175"
		}
	}
	return ""
}

// pickBoard resolves the board to flash from the two senses: the firmware's
// VERSION handshake (primary) and the USB VID/PID hardware identity
// (fallback). When both are known and disagree the board is running the
// wrong firmware — refused without force; with force the hardware wins.
func pickBoard(handshake, usb string, force bool) (string, error) {
	switch {
	case handshake == "" && usb == "":
		return "", errors.New("cannot sense board type (no VERSION reply, unknown USB IDs)")
	case handshake == "":
		return usb, nil
	case usb == "" || usb == handshake:
		return handshake, nil
	case force:
		return usb, nil
	}
	return "", fmt.Errorf("device firmware reports %s but the USB IDs say %s; flash %s firmware with -force", handshake, usb, usb)
}

// FlashFirmware senses the attached board, picks the matching embedded
// firmware image, and writes it with esptool, suspending normal serial
// traffic for the duration. A wrong-board flash is refused unless force.
// Blocks until done; call from a goroutine, not the UI thread.
func (a *Agent) FlashFirmware(esptool string, force bool) error {
	if esptool == "" {
		return errors.New("esptool not found (app bundle Resources or PATH)")
	}
	// Flashing always goes over USB, even when the device talks BLE: open
	// the serial port now (a no-op if already open) and ask for the banner.
	a.light.serial.sendLine("VERSION")
	port := a.light.PortName()
	if port == "" {
		return errors.New("no device connected")
	}
	// Handshake first: give the board a moment to answer VERSION before
	// falling back to USB IDs. Only the serial device's own banner counts —
	// a BLE-connected device must never answer for the board being flashed.
	for range 20 {
		if a.light.serial.bannerBoard() != "" {
			break
		}
		time.Sleep(150 * time.Millisecond)
	}
	board, err := pickBoard(a.light.serial.bannerBoard(), portBoard(port), force)
	if err != nil {
		return err
	}
	image, fwVer, err := firmware.ForBoard(board)
	if err != nil {
		return err
	}
	if !a.flashing.CompareAndSwap(false, true) {
		return errors.New("flash already in progress")
	}
	defer a.flashing.Store(false)

	tmp, err := os.CreateTemp("", "onit-fw-*.bin")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(image); err != nil {
		tmp.Close()
		return err
	}
	tmp.Close()

	// warn on the device itself; fw >= 1.2.0 shows a red pulsing
	// "Flashing - do not power off" screen (sticky, watchdog-exempt)
	a.light.Send("flashing")
	time.Sleep(400 * time.Millisecond) // let it render before we drop the port

	a.light.Close()        // release the port for esptool
	a.light.ClearVersion() // the answer must come from the new firmware
	log.Printf("Flashing %s %s (%d bytes) to %s", board, fwVer, len(image), port)
	out, err := exec.Command(esptool,
		"--chip", "esp32s3", "--port", port, "--baud", "460800",
		"write-flash", "0x0", tmp.Name()).CombinedOutput()
	log.Printf("esptool output:\n%s", out)
	if err != nil {
		tail := string(out)
		if len(tail) > 400 {
			tail = tail[len(tail)-400:]
		}
		return fmt.Errorf("esptool failed: %w\n%s", err, tail)
	}
	log.Print("Flash complete")
	time.Sleep(2 * time.Second) // board reboots into the new image
	a.wake()                    // reconnect; boot banner refreshes the version
	return nil
}

// Flashing reports whether a firmware flash is in progress.
func (a *Agent) Flashing() bool { return a.flashing.Load() }
