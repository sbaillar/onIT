package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

// esptoolPath finds the bundled esptool — macOS: onIT.app/Contents/Resources,
// Windows: next to onIT.exe — falling back to PATH for dev builds.
func esptoolPath() string {
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		var candidates []string
		if runtime.GOOS == "darwin" {
			candidates = append(candidates, filepath.Join(dir, "..", "Resources", "esptool"))
		}
		name := "esptool"
		if runtime.GOOS == "windows" {
			name += ".exe"
		}
		candidates = append(candidates, filepath.Join(dir, name))
		for _, p := range candidates {
			if _, err := os.Stat(p); err == nil {
				return p
			}
		}
	}
	if p, err := exec.LookPath("esptool"); err == nil {
		return p
	}
	return ""
}
