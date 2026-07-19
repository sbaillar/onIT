package main

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
)

func pidAlive(pid int) bool {
	if err := syscall.Kill(pid, 0); err != nil && err != syscall.EPERM {
		return false
	}
	// a zombie (dead but unreaped child) still answers kill(pid, 0)
	out, err := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "stat=").Output()
	if err != nil {
		return false
	}
	st := strings.TrimSpace(string(out))
	return st != "" && !strings.HasPrefix(st, "Z")
}

// isOnitProcess guards against pid reuse: only kill if the process's binary
// name looks like ours.
func isOnitProcess(pid int) bool {
	out, err := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "comm=").Output()
	if err != nil {
		return false
	}
	return strings.Contains(strings.ToLower(strings.TrimSpace(string(out))), "onit")
}

// terminate asks nicely, then kills. Reports whether the process is gone.
func terminate(pid int) bool {
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	p.Signal(syscall.SIGTERM)
	for range 30 { // up to 3s of grace
		if !pidAlive(pid) {
			return true
		}
		time.Sleep(100 * time.Millisecond)
	}
	p.Kill()
	time.Sleep(200 * time.Millisecond)
	return !pidAlive(pid)
}
