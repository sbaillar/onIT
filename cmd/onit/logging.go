package main

import (
	"io"
	"log"
	"os"
	"path/filepath"

	"onit/internal/firmware"
)

// setupLogging mirrors logs to a file so failures are diagnosable when the
// app is launched from Finder/login (where stderr goes nowhere).
func setupLogging() {
	p := logPath()
	os.MkdirAll(filepath.Dir(p), 0o755)
	if fi, err := os.Stat(p); err == nil && fi.Size() > 1<<20 {
		os.Remove(p) // crude size cap: start fresh past 1 MB
	}
	f, err := os.OpenFile(p, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		log.Printf("log file unavailable (%v); logging to stderr only", err)
		return
	}
	log.SetOutput(io.MultiWriter(os.Stderr, f))
	log.Printf("onIT starting (embedded firmware %s, log %s)", firmware.Version, p)
}
