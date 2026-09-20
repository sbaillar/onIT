package busylight

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// The New Teams client logs every presence change locally, e.g.
//
//	{ user id :7de..., availability: DoNotDisturb, unread notification count: 0 }
//
// Tailing that file yields full presence with no sign-in, no app
// registration, and no Conditional Access involvement - the Teams client
// already authenticated. The format is undocumented and may shift with
// Teams updates; when it does, the source simply reports down and the
// agent falls back.

// Overridable in tests.
var (
	teamsLogGlobs = defaultTeamsLogGlobs()
	teamsLogTick  = 2 * time.Second
)

const teamsLogSeedSize = 256 << 10 // how far back to look for the current state

// Teams logs presence only when it changes, so a quiet log is not a dead
// one: while the client is running the log is live however old its mtime.
// The staleness window only applies when the client can't be seen at all
// (process detection failed), so the source still winds down eventually.
// Overridable in tests.
var teamsLogStale = 10 * time.Minute

// availability: X / "availability":"X" /
// OnAvailabilityUpdate Received availability update: X
var presenceLineRe = regexp.MustCompile(`[Aa]vailability(?:\s+[Uu]pdate)?"?\s*:\s*"?([A-Za-z]+)`)

// parsePresenceLine extracts a light state from one Teams log line.
func parsePresenceLine(line string) (string, bool) {
	m := presenceLineRe.FindStringSubmatch(line)
	if m == nil {
		return "", false
	}
	// Unknown is Teams not knowing yet (startup, reconnect), not the user
	// going away: hold the last state rather than blanking the light.
	if m[1] == "PresenceUnknown" {
		return "", false
	}
	// the availability field sometimes carries activity values (InACall,
	// Presenting); mapPresence handles both positions
	return mapPresence(m[1], m[1]), true
}

// newestTeamsLog finds the most recently modified Teams client log.
func newestTeamsLog() (string, error) {
	best, bestTime := "", time.Time{}
	for _, g := range teamsLogGlobs {
		matches, _ := filepath.Glob(g)
		for _, m := range matches {
			st, err := os.Stat(m)
			if err != nil {
				continue
			}
			if st.ModTime().After(bestTime) {
				best, bestTime = m, st.ModTime()
			}
		}
	}
	if best == "" {
		return "", &sourceSwitch{"no Teams client logs found"}
	}
	return best, nil
}

// teamsLogAvailable reports whether a live Teams log exists to tail: the
// client is running, or the log was written recently.
func teamsLogAvailable() bool {
	path, err := newestTeamsLog()
	if err != nil {
		return false
	}
	st, err := os.Stat(path)
	return err == nil && !teamsLogDead(st.ModTime())
}

// teamsLogDead reports whether a log last written at mod should be given
// up on. A running client keeps its log live no matter how quiet it is.
func teamsLogDead(mod time.Time) bool {
	return time.Since(mod) > teamsLogStale && !teamsClientRunning()
}

// teamsLogSession tails the newest Teams log until it goes stale or a newer
// log appears (Teams restarted). Blocks; runs as a presence session.
func (a *Agent) teamsLogSession() error {
	path, err := newestTeamsLog()
	if err != nil {
		return err
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	// seed from the tail so a mid-session start shows the current state —
	// the *current* one only. Applying every state found in the tail walked
	// the light through the whole recent history on each start, and since
	// this runs again after every rotation or stale reader, that read as the
	// light flapping between states seconds apart.
	st, err := f.Stat()
	if err != nil {
		return err
	}
	pos := st.Size()
	seedFrom := max(0, pos-teamsLogSeedSize)
	buf := make([]byte, pos-seedFrom)
	if _, err := f.ReadAt(buf, seedFrom); err == nil {
		seed := ""
		for _, line := range strings.Split(string(buf), "\n") {
			if state, ok := parsePresenceLine(line); ok {
				seed = state
			}
		}
		if seed != "" {
			a.setTeams(true, seed)
		}
	}

	rest := "" // partial trailing line between reads
	for {
		time.Sleep(teamsLogTick)
		if a.Graph.SignedIn() { // a re-auth landed: Graph is the preferred source
			return &sourceSwitch{"graph signed in"}
		}
		if np, err := newestTeamsLog(); err == nil && np != path {
			return &sourceSwitch{"teams log rotated"}
		}
		st, err := os.Stat(path)
		if err != nil {
			return err
		}
		if st.Size() < pos { // truncated
			pos, rest = 0, ""
		}
		if st.Size() > pos {
			chunk := make([]byte, st.Size()-pos)
			if _, err := f.ReadAt(chunk, pos); err != nil {
				return err
			}
			pos = st.Size()
			lines := strings.Split(rest+string(chunk), "\n")
			rest = lines[len(lines)-1]
			for _, line := range lines[:len(lines)-1] {
				if state, ok := parsePresenceLine(line); ok {
					a.setTeams(true, state)
				}
			}
		}
		if teamsLogDead(st.ModTime()) {
			return &sourceSwitch{"teams log stale (client closed?)"}
		}
	}
}
