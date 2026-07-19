package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const launchAgentLabel = "casa.baillargeon.onit"

const plistTemplate = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>%s</string>
	<key>ProgramArguments</key>
	<array>
		<string>%s</string>
		<string>-hidden</string>
	</array>
	<key>RunAtLoad</key>
	<true/>
</dict>
</plist>
`

// autostartAutoEnable reports whether first launch should enable the login
// item: only when running from the installed location, so a dev build's
// temp path never lands in the plist.
func autostartAutoEnable(exe string) bool {
	return strings.HasPrefix(exe, "/Applications/")
}

func plistPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "LaunchAgents", launchAgentLabel+".plist")
}

func autostartEnabled() bool {
	_, err := os.Stat(plistPath())
	return err == nil
}

func setAutostart(on bool) error {
	if !on {
		err := os.Remove(plistPath())
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	exe = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(exe)
	p := plistPath()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return os.WriteFile(p,
		[]byte(fmt.Sprintf(plistTemplate, launchAgentLabel, exe)), 0o644)
}
