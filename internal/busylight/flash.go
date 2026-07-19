package busylight

import (
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"time"
)

// FlashFirmware writes a merged flash image to the device with esptool,
// suspending normal serial traffic for the duration. Blocks until done;
// call from a goroutine, not the UI thread.
func (a *Agent) FlashFirmware(esptool string, image []byte) error {
	if esptool == "" {
		return errors.New("esptool not found (app bundle Resources or PATH)")
	}
	port := a.light.PortName()
	if port == "" {
		return errors.New("no device connected")
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

	a.light.Close() // release the port for esptool
	log.Printf("Flashing %d bytes to %s", len(image), port)
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
