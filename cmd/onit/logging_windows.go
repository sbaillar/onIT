package main

import (
	"os"
	"path/filepath"
)

func logPath() string {
	cache, _ := os.UserCacheDir() // %LOCALAPPDATA%
	return filepath.Join(cache, "onIT", "onIT.log")
}
