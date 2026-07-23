package busylight

import (
	"os"
	"path/filepath"
	"runtime"
)

// defaultTeamsLogGlobs returns where the New Teams client writes its logs.
func defaultTeamsLogGlobs() []string {
	home, _ := os.UserHomeDir()
	switch runtime.GOOS {
	case "darwin":
		return []string{filepath.Join(home,
			"Library/Group Containers/UBF8T346G9.com.microsoft.teams/Library/Application Support/Logs/MSTeams_*.log")}
	case "windows":
		local := os.Getenv("LOCALAPPDATA")
		if local == "" {
			local = filepath.Join(home, "AppData", "Local")
		}
		return []string{filepath.Join(local,
			"Packages", "MSTeams_8wekyb3d8bbwe", "LocalCache", "Microsoft", "MSTeams", "Logs", "MSTeams_*.log")}
	}
	return nil
}
