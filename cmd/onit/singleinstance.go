package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func pidFilePath() string {
	cfg, _ := os.UserConfigDir()
	return filepath.Join(cfg, "onIT", "onit.pid")
}

// takeoverInstance stops a previously running onIT (if the recorded pid is
// alive and passes isOnit) and records our own pid. Returns the pid that was
// stopped, or 0 if there was nothing to stop.
func takeoverInstance(path string, isOnit func(int) bool) (int, error) {
	stopped := 0
	if b, err := os.ReadFile(path); err == nil {
		pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
		if err == nil && pid > 0 && pid != os.Getpid() && pidAlive(pid) && isOnit(pid) {
			if terminate(pid) {
				stopped = pid
			}
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return stopped, err
	}
	return stopped, os.WriteFile(path, []byte(strconv.Itoa(os.Getpid())), 0o644)
}
