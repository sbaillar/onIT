// onitctl — headless Teams busylight agent (CLI). See internal/busylight for the logic.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"onit/internal/busylight"
	"onit/internal/emoji"
)

func openBrowser(url string) {
	switch runtime.GOOS {
	case "darwin":
		exec.Command("open", url).Start()
	case "windows":
		exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	default:
		exec.Command("xdg-open", url).Start()
	}
}

// esptoolPath finds esptool next to the onitctl binary (dev builds copy it
// into dist), falling back to PATH.
func esptoolPath() string {
	if exe, err := os.Executable(); err == nil {
		name := "esptool"
		if runtime.GOOS == "windows" {
			name += ".exe"
		}
		p := filepath.Join(filepath.Dir(exe), name)
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	if p, err := exec.LookPath("esptool"); err == nil {
		return p
	}
	return ""
}

// runBLE is the BLE bench: exercises pairing, states, emoji, and touch
// events against real hardware. state/emoji go over BLE when a device is
// bonded and reachable (USB fallback otherwise — the reported transport
// tells which).
func runBLE(light *busylight.Light, args []string) {
	usage := "usage: onitctl -ble pair | state <state> | emoji <png> | watch"
	if len(args) == 0 {
		log.Fatal(usage)
	}
	switch args[0] {
	case "pair":
		fmt.Println("scanning for onIT busylights (pairing with the first one found)...")
		err := light.PairBLE(context.Background(), func(d busylight.BLEDevice) bool {
			fmt.Printf("pairing with %s (%s); type the passkey shown on the device\n", d.Name, d.ID)
			return true
		})
		if err != nil {
			log.Fatal(err)
		}
		fmt.Println("paired")

	case "state":
		if len(args) < 2 {
			log.Fatal(usage)
		}
		if !light.SendLine("STATE:" + args[1]) {
			log.Fatal("send failed (no transport reachable)")
		}
		fmt.Printf("sent over %s\n", light.Transport())

	case "emoji":
		if len(args) < 2 {
			log.Fatal(usage)
		}
		b, err := os.ReadFile(args[1])
		if err != nil {
			log.Fatal(err)
		}
		payload, err := emoji.FromPNG(b)
		if err != nil {
			log.Fatal(err)
		}
		start := time.Now()
		if !light.SendLine("EMOJI:" + payload) {
			log.Fatal("send failed (no transport reachable)")
		}
		fmt.Printf("sent over %s in %v\n", light.Transport(), time.Since(start).Round(time.Millisecond))

	case "watch":
		light.SetOnTouch(func(kind string) { fmt.Println("TOUCH:" + kind) })
		fmt.Println("watching for touches (ctrl-c to stop)...")
		for { // VERSION doubles as connect/keepalive so the link comes back after drops
			light.SendLine("VERSION")
			time.Sleep(10 * time.Second)
		}

	default:
		log.Fatal(usage)
	}
}

func main() {
	ports := flag.Bool("ports", false, "list serial ports and exit")
	flashFW := flag.Bool("flash", false, "flash the bundled firmware matching the attached board and exit")
	force := flag.Bool("force", false, "with -flash: flash even when the sensed board type mismatches")
	ble := flag.Bool("ble", false, "BLE bench; subcommands: pair | state <state> | emoji <png> | watch")
	login := flag.Bool("login", false, "sign in to Microsoft Graph in the browser and exit")
	client := flag.String("client", "", "app registration client ID (default: built-in shared registration)")
	tenant := flag.String("tenant", "", "Entra tenant ID (default: organizations)")
	forward := flag.String("forward", "", "push presence to a remote onIT instead of driving a local light (e.g. http://hammer-mini:8125; plain HTTP - trusted networks only)")
	token := flag.String("token", "", "shared token for -forward (shown when the receiver enables remote presence)")
	listen := flag.String("listen", "", "also accept remote presence pushes on this address (e.g. :8125)")
	mic := flag.Bool("mic", true, "a live microphone escalates available to in-a-call")
	flag.Parse()

	switch {
	case *ports:
		busylight.ListPorts()

	case *flashFW:
		if err := busylight.NewAgent().FlashFirmware(esptoolPath(), *force); err != nil {
			log.Fatal(err)
		}

	case *ble:
		runBLE(busylight.NewLight(), flag.Args())

	case *login:
		id := *client
		if id == "" {
			id = busylight.DefaultClientID
		}
		bl, err := busylight.StartBrowserLogin(id, *tenant)
		if err != nil {
			log.Fatal(err)
		}
		fmt.Println("Finish signing in to Microsoft in your browser:")
		fmt.Println("  " + bl.AuthURL)
		openBrowser(bl.AuthURL)
		g := busylight.LoadGraph()
		if err := g.WaitForBrowserLogin(bl); err != nil {
			log.Fatal(err)
		}
		fmt.Println("Signed in. Run onitctl -forward http://<onit-host>:8125 to relay presence.")

	case *forward != "":
		if *token == "" {
			log.Fatal("-forward needs -token (shown when the receiver enables remote presence)")
		}
		g := busylight.LoadGraph()
		for {
			err := g.ForwardPresence(*forward, *token)
			log.Printf("forward down (%v), retrying", err)
			time.Sleep(5 * time.Second)
		}

	default:
		agent := busylight.NewAgent()
		agent.SetMicRule(*mic)
		if *listen != "" {
			rs, err := agent.ListenRemote(*listen)
			if err != nil {
				log.Fatal(err)
			}
			log.Printf("accepting remote presence on %s (token: %s)", *listen, rs.Token())
		}
		agent.Run()
	}
}
