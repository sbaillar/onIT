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

func TestMicRule(t *testing.T) {
	a := NewAgent()
	a.mu.Lock()
	a.teamsUp, a.teamsState = true, "available"
	a.mu.Unlock()

	// rule off: mic activity is ignored
	a.micActive.Store(true)
	if got := a.Status().Shown; got != "available" {
		t.Fatalf("rule off: Shown = %q, want available", got)
	}

	// rule on: a live mic escalates available to meeting
	a.SetMicRule(true)
	if got := a.Status().Shown; got != "meeting" {
		t.Fatalf("mic active: Shown = %q, want meeting", got)
	}

	// a real call state is left alone, and overrides still win
	a.mu.Lock()
	a.teamsState = "sharing"
	a.mu.Unlock()
	if got := a.Status().Shown; got != "sharing" {
		t.Errorf("mic + sharing: Shown = %q, want sharing", got)
	}
	a.SetOverride("off")
	if got := a.Status().Shown; got != "off" {
		t.Errorf("mic + override: Shown = %q, want off", got)
	}
	a.SetOverride("")

	// mic released: back to the real state
	a.mu.Lock()
	a.teamsState = "available"
	a.mu.Unlock()
	a.micActive.Store(false)
	if got := a.Status().Shown; got != "available" {
		t.Errorf("mic released: Shown = %q, want available", got)
	}
}

func TestHandleTouch(t *testing.T) {
	a := NewAgent()

	// tap cycles auto -> available -> meeting -> sharing -> off -> auto
	for _, want := range []string{"available", "meeting", "sharing", "off", ""} {
		a.HandleTouch("TAP")
		if got := a.Status().Override; got != want {
			t.Fatalf("tap cycle: Override = %q, want %q", got, want)
		}
	}

	// tap dismisses an emoji or custom override straight back to auto
	for _, ov := range []string{"emoji", "custom:Back in 5"} {
		a.SetOverride(ov)
		a.HandleTouch("TAP")
		if got := a.Status().Override; got != "" {
			t.Errorf("tap on %q: Override = %q, want auto", ov, got)
		}
	}

	// long press toggles do-not-disturb (sharing)
	a.HandleTouch("LONG")
	if got := a.Status().Override; got != "sharing" {
		t.Fatalf("long press: Override = %q, want sharing", got)
	}
	a.HandleTouch("LONG")
	if got := a.Status().Override; got != "" {
		t.Fatalf("second long press: Override = %q, want auto", got)
	}

	// unknown kinds are ignored
	a.SetOverride("meeting")
	a.HandleTouch("SWIPE")
	if got := a.Status().Override; got != "meeting" {
		t.Errorf("unknown touch changed Override to %q", got)
	}
}
