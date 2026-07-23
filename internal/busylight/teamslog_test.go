package busylight

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestParsePresenceLine(t *testing.T) {
	cases := []struct {
		line, want string
		ok         bool
	}{
		// real New Teams format (macOS and Windows share the engine)
		{"tate total number of users: 1 { user id :7de0322da3a505d8, availability: DoNotDisturb, unread notification count: 0 }", "sharing", true},
		{"{ user id :x, availability: Available, unread notification count: 2 }", "available", true},
		{"{ user id :x, availability: InACall, unread notification count: 0 }", "meeting", true},
		{"{ user id :x, availability: Presenting, unread notification count: 0 }", "sharing", true},
		{"{ user id :x, availability: Busy, unread notification count: 0 }", "available", true}, // calendar-busy stays green
		{"{ user id :x, availability: PresenceUnknown, unread notification count: 0 }", "off", true},
		// JSON-style variant some builds emit
		{`... "availability":"Away","activity":"Away" ...`, "available", true},
		{"no presence here", "", false},
	}
	for _, c := range cases {
		got, ok := parsePresenceLine(c.line)
		if ok != c.ok || got != c.want {
			t.Errorf("parsePresenceLine(%q) = %q %v, want %q %v", c.line, got, ok, c.want, c.ok)
		}
	}
}

func TestNewestTeamsLog(t *testing.T) {
	dir := t.TempDir()
	oldGlobs := teamsLogGlobs
	teamsLogGlobs = []string{filepath.Join(dir, "MSTeams_*.log")}
	defer func() { teamsLogGlobs = oldGlobs }()

	if _, err := newestTeamsLog(); err == nil {
		t.Fatal("newestTeamsLog succeeded with no logs")
	}

	older := filepath.Join(dir, "MSTeams_2026-01-01.log")
	newer := filepath.Join(dir, "MSTeams_2026-02-01.log")
	os.WriteFile(older, []byte("x"), 0o644)
	os.WriteFile(newer, []byte("x"), 0o644)
	past := time.Now().Add(-time.Hour)
	os.Chtimes(older, past, past)

	got, err := newestTeamsLog()
	if err != nil || got != newer {
		t.Fatalf("newestTeamsLog = %q %v, want %q", got, err, newer)
	}
}

func TestTeamsLogSessionFollowsAppends(t *testing.T) {
	dir := t.TempDir()
	oldGlobs, oldTick := teamsLogGlobs, teamsLogTick
	teamsLogGlobs = []string{filepath.Join(dir, "MSTeams_*.log")}
	teamsLogTick = 10 * time.Millisecond
	defer func() { teamsLogGlobs, teamsLogTick = oldGlobs, oldTick }()

	path := filepath.Join(dir, "MSTeams_2026-07-23.log")
	os.WriteFile(path, []byte("boot noise\n{ user id :x, availability: Available, unread notification count: 0 }\npartial"), 0o644)

	a := NewAgent()
	done := make(chan error, 1)
	go func() { done <- a.teamsLogSession() }()

	// seeded from the existing tail
	waitFor(t, func() bool { return a.Status().Shown == "available" }, "seed state")

	// appended lines are picked up
	f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	f.WriteString(" line end\n{ user id :x, availability: InACall, unread notification count: 0 }\n")
	f.Close()
	waitFor(t, func() bool { return a.Status().Shown == "meeting" }, "appended state")

	// a newer log file ends the session so the next one starts fresh
	newer := filepath.Join(dir, "MSTeams_2026-07-24.log")
	os.WriteFile(newer, []byte(""), 0o644)
	future := time.Now().Add(time.Hour)
	os.Chtimes(newer, future, future)
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("session ended without error on rotation")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("session did not notice the rotated log")
	}
}

func waitFor(t *testing.T, cond func() bool, what string) {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for !cond() {
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for %s", what)
		case <-time.After(5 * time.Millisecond):
		}
	}
}
