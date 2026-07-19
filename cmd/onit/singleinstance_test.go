package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func spawnSleeper(t *testing.T) *exec.Cmd {
	t.Helper()
	cmd := exec.Command("sleep", "60")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cmd.Process.Kill(); cmd.Wait() })
	return cmd
}

func writePidFile(t *testing.T, pid int) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "onit.pid")
	if err := os.WriteFile(path, []byte(strconv.Itoa(pid)), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestTakeoverKillsRunningInstance(t *testing.T) {
	old := spawnSleeper(t)
	path := writePidFile(t, old.Process.Pid)

	stopped, err := takeoverInstance(path, func(int) bool { return true })
	if err != nil {
		t.Fatal(err)
	}
	if stopped != old.Process.Pid {
		t.Fatalf("stopped = %d, want %d", stopped, old.Process.Pid)
	}

	done := make(chan struct{})
	go func() { old.Wait(); close(done) }()
	select {
	case <-done: // old instance is gone
	case <-time.After(5 * time.Second):
		t.Fatal("old process still running after takeover")
	}

	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := strconv.Atoi(string(b)); got != os.Getpid() {
		t.Errorf("pid file = %q, want our pid %d", b, os.Getpid())
	}
}

func TestTakeoverIgnoresDeadPid(t *testing.T) {
	// spawn and fully reap a process so its pid is dead
	c := exec.Command("true")
	if err := c.Run(); err != nil {
		t.Fatal(err)
	}
	path := writePidFile(t, c.Process.Pid)

	stopped, err := takeoverInstance(path, func(int) bool { return true })
	if err != nil {
		t.Fatal(err)
	}
	if stopped != 0 {
		t.Errorf("stopped = %d, want 0 for a dead pid", stopped)
	}
}

func TestTakeoverRefusesForeignProcess(t *testing.T) {
	foreign := spawnSleeper(t)
	path := writePidFile(t, foreign.Process.Pid)

	stopped, err := takeoverInstance(path, func(int) bool { return false })
	if err != nil {
		t.Fatal(err)
	}
	if stopped != 0 {
		t.Errorf("stopped = %d, want 0 when the pid is not an onIT process", stopped)
	}
	if !pidAlive(foreign.Process.Pid) {
		t.Error("foreign process was killed despite failing the identity check")
	}
}

func TestTakeoverNoPidFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "onit.pid")
	stopped, err := takeoverInstance(path, func(int) bool { return true })
	if err != nil {
		t.Fatal(err)
	}
	if stopped != 0 {
		t.Errorf("stopped = %d, want 0 with no pid file", stopped)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("pid file not created: %v", err)
	}
}
