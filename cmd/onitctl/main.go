// onitctl — headless Teams busylight agent (CLI). See internal/busylight for the logic.
package main

import (
	"flag"

	"onit/internal/busylight"
)

func main() {
	ports := flag.Bool("ports", false, "list serial ports and exit")
	flag.Parse()
	if *ports {
		busylight.ListPorts()
		return
	}
	busylight.NewAgent().Run()
}
