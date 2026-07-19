package main

import (
	"os"
	"path/filepath"
)

const uninstallDoneMsg = "Settings were removed. You can now delete the onIT folder."

// removeInstalledFiles: nothing to do on Windows — the exe lives wherever the
// user unzipped it and can't delete itself while running.
func removeInstalledFiles() error { return nil }

// prefsDirs lists where Fyne stores this app's preferences (%APPDATA%).
func prefsDirs() []string {
	cfg, _ := os.UserConfigDir()
	return []string{filepath.Join(cfg, "fyne", "casa.baillargeon.onit")}
}
