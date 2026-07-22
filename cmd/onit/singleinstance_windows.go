package main

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/windows"
)

func pidAlive(pid int) bool {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	defer windows.CloseHandle(h)
	var code uint32
	if windows.GetExitCodeProcess(h, &code) != nil {
		return false
	}
	return code == 259 // STILL_ACTIVE
}

// isOnitProcess guards against pid reuse: only kill if the process's image
// name looks like ours.
func isOnitProcess(pid int) bool {
	out, err := exec.Command("tasklist", "/FI", "PID eq "+strconv.Itoa(pid), "/FO", "CSV", "/NH").Output()
	if err != nil {
		return false
	}
	return strings.Contains(strings.ToLower(string(out)), "onit")
}

// onitPids lists every running onIT.exe by exact image name (onitctl.exe
// never matches).
func onitPids() []int {
	out, err := exec.Command("tasklist", "/FI", "IMAGENAME eq onIT.exe", "/FO", "CSV", "/NH").Output()
	if err != nil {
		return nil
	}
	var pids []int
	for _, line := range strings.Split(string(out), "\n") {
		cols := strings.Split(line, "\",\"")
		if len(cols) < 2 {
			continue
		}
		if pid, err := strconv.Atoi(strings.Trim(cols[1], "\"")); err == nil {
			pids = append(pids, pid)
		}
	}
	return pids
}

// terminate kills the process (Windows has no graceful signal for GUI apps
// without a console). Reports whether the process is gone.
func terminate(pid int) bool {
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	p.Kill()
	for range 30 {
		if !pidAlive(pid) {
			return true
		}
		time.Sleep(100 * time.Millisecond)
	}
	return false
}
