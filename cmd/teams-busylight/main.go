// Headless Teams busylight agent (CLI). See internal/busylight for the logic.
package main

import (
	"os"

	"onit/internal/busylight"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "-ports" {
		busylight.ListPorts()
		return
	}
	busylight.NewAgent().Run()
}
