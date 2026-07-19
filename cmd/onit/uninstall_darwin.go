package main

import (
	"os"
	"os/exec"
	"path/filepath"
)

const uninstallDoneMsg = "onIT and its settings were removed."

// removeInstalledFiles deletes the app bundle and CLI; macOS shows an
// admin-password prompt (the pkg installed them root-owned).
func removeInstalledFiles() error {
	script := `do shell script "rm -rf /Applications/onIT.app /usr/local/bin/onitctl" with administrator privileges`
	return exec.Command("osascript", "-e", script).Run()
}

// prefsDirs lists where Fyne stores this app's preferences.
func prefsDirs() []string {
	home, _ := os.UserHomeDir()
	cfg, _ := os.UserConfigDir()
	return []string{
		filepath.Join(home, "Library", "Preferences", "fyne", "casa.baillargeon.onit"),
		filepath.Join(cfg, "fyne", "casa.baillargeon.onit"),
	}
}
