package main

import (
	"os"
	"path/filepath"
)

func logPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "Logs", "onIT.log")
}
