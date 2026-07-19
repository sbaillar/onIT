package main

import (
	"log"
	"os/exec"
	"strings"
	"time"
)

// relaunchWhenInstalled waits for the pkg to land the new version in
// /Applications, then starts it. Launching by hand can't work while the
// old app runs: LaunchServices sees the bundle id already active and just
// focuses the old instance, so the new binary - and its single-instance
// takeover - never runs. `open -n` forces a fresh process, which then
// stops this one.
func relaunchWhenInstalled(ver string) {
	go func() {
		const app = "/Applications/onIT.app"
		for range 600 { // poll up to 30 min while the user runs the installer
			time.Sleep(3 * time.Second)
			out, err := exec.Command("/usr/libexec/PlistBuddy", "-c",
				"Print :CFBundleShortVersionString", app+"/Contents/Info.plist").Output()
			if err != nil || strings.TrimSpace(string(out)) != ver {
				continue
			}
			time.Sleep(2 * time.Second) // let the installer finish copying
			log.Printf("update %s installed - relaunching", ver)
			if err := exec.Command("open", "-n", app).Start(); err != nil {
				log.Printf("relaunch failed: %v", err)
			}
			return
		}
		log.Print("update: gave up waiting for the install to finish")
	}()
}
