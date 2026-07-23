// onitctl — headless Teams busylight agent (CLI). See internal/busylight for the logic.
package main

import (
	"flag"
	"fmt"
	"log"
	"os/exec"
	"runtime"
	"time"

	"onit/internal/busylight"
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

func main() {
	ports := flag.Bool("ports", false, "list serial ports and exit")
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
