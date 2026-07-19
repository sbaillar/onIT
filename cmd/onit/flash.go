package main

import (
	"os"
	"os/exec"
	"path/filepath"
)

// esptoolPath finds the bundled esptool (onIT.app/Contents/Resources/esptool),
// falling back to PATH for dev builds.
func esptoolPath() string {
	if exe, err := os.Executable(); err == nil {
		p := filepath.Join(filepath.Dir(exe), "..", "Resources", "esptool")
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	if p, err := exec.LookPath("esptool"); err == nil {
		return p
	}
	return ""
}
