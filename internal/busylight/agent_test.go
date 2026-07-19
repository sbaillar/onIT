package busylight

import "testing"

func TestMapState(t *testing.T) {
	cases := []struct {
		ms   meetingState
		want string
	}{
		{meetingState{}, "available"},
		{meetingState{IsInMeeting: true}, "meeting"},
		{meetingState{IsInMeeting: true, IsMuted: true}, "meeting"}, // mute not surfaced
		{meetingState{IsInMeeting: true, IsSharing: true}, "sharing"},
		{meetingState{IsInMeeting: true, IsRecordingOn: true}, "sharing"},
		{meetingState{IsMuted: true}, "available"}, // not in meeting wins
	}
	for _, c := range cases {
		if got := mapState(&c.ms); got != c.want {
			t.Errorf("mapState(%+v) = %q, want %q", c.ms, got, c.want)
		}
	}
}

func TestOverrideResolution(t *testing.T) {
	a := NewAgent()

	if got := a.Status().Shown; got != "off" {
		t.Errorf("initial Shown = %q, want off", got)
	}

	// Teams drives the state in auto mode
	a.mu.Lock()
	a.teamsUp, a.teamsState = true, "meeting"
	a.mu.Unlock()
	if got := a.Status().Shown; got != "meeting" {
		t.Errorf("auto Shown = %q, want meeting", got)
	}

	// manual override wins over Teams
	a.mu.Lock()
	a.override = "available"
	a.mu.Unlock()
	if got := a.Status().Shown; got != "available" {
		t.Errorf("override Shown = %q, want available", got)
	}

	// Teams changes underneath do not disturb the override
	a.mu.Lock()
	a.teamsState = "sharing"
	a.mu.Unlock()
	if got := a.Status().Shown; got != "available" {
		t.Errorf("override Shown after teams change = %q, want available", got)
	}

	// clearing the override returns to the live Teams state
	a.mu.Lock()
	a.override = ""
	a.mu.Unlock()
	if got := a.Status().Shown; got != "sharing" {
		t.Errorf("back-to-auto Shown = %q, want sharing", got)
	}

	// Teams down in auto mode -> off
	a.mu.Lock()
	a.teamsUp, a.teamsState = false, "off"
	a.mu.Unlock()
	if st := a.Status(); st.Shown != "off" || st.TeamsConnected {
		t.Errorf("teams-down Status = %+v, want Shown=off TeamsConnected=false", st)
	}
}
