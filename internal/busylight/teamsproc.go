package busylight

import (
	"os/exec"
	"runtime"
	"strings"
)

// teamsClientRunning reports whether the New Teams client process is up.
// Overridable in tests.
var teamsClientRunning = detectTeamsClient

func detectTeamsClient() bool {
	switch runtime.GOOS {
	case "darwin", "linux":
		return exec.Command("pgrep", "-x", "MSTeams").Run() == nil
	case "windows":
		out, err := exec.Command("tasklist", "/FI", "IMAGENAME eq ms-teams.exe", "/NH").Output()
		return err == nil && strings.Contains(strings.ToLower(string(out)), "ms-teams.exe")
	}
	return false
}
