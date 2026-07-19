package busylight

import (
	"log"
	"sync"
	"time"
)

const heartbeat = 2 * time.Second // firmware watchdog is 5s

// States accepted by the firmware, in display order.
var States = []string{"available", "meeting", "muted", "sharing", "off"}

// Status is a snapshot of the agent for UIs.
type Status struct {
	TeamsConnected bool
	LightConnected bool
	Override       string // "" = auto (follow Teams)
	Shown          string // state currently sent to the device
}

// Agent drives the light from Teams presence, with an optional manual override.
type Agent struct {
	light *Light
	reqID int

	mu         sync.Mutex
	teamsUp    bool
	teamsState string // last state derived from Teams; "off" while disconnected
	override   string // "" = auto
	onChange   func()
}

func NewAgent() *Agent {
	return &Agent{light: NewLight(), teamsState: "off"}
}

// OnChange registers a callback fired whenever Status may have changed.
// Must be set before Run.
func (a *Agent) OnChange(f func()) { a.onChange = f }

// effectiveLocked returns the state the light should show. Caller holds mu.
func (a *Agent) effectiveLocked() string {
	if a.override != "" {
		return a.override
	}
	return a.teamsState
}

func (a *Agent) Status() Status {
	a.mu.Lock()
	defer a.mu.Unlock()
	return Status{
		TeamsConnected: a.teamsUp,
		LightConnected: a.light.Connected(),
		Override:       a.override,
		Shown:          a.effectiveLocked(),
	}
}

// SetOverride forces a state on the light; "" returns to auto (Teams).
func (a *Agent) SetOverride(state string) {
	a.mu.Lock()
	a.override = state
	a.mu.Unlock()
	a.push()
}

func (a *Agent) setTeams(up bool, state string) {
	a.mu.Lock()
	changed := a.teamsUp != up || a.teamsState != state
	a.teamsUp = up
	a.teamsState = state
	auto := a.override == ""
	a.mu.Unlock()
	if changed && auto {
		log.Printf("state -> %s", state)
	}
	a.push()
}

// push sends the effective state to the device and notifies the UI.
func (a *Agent) push() {
	a.mu.Lock()
	state := a.effectiveLocked()
	a.mu.Unlock()
	a.light.Send(state)
	if a.onChange != nil {
		a.onChange()
	}
}

// Run blocks forever: heartbeats the device and maintains the Teams session.
func (a *Agent) Run() {
	go func() {
		for {
			a.push()
			time.Sleep(heartbeat)
		}
	}()
	for {
		err := a.session()
		log.Printf("Teams WS down (%v)", err)
		a.setTeams(false, "off")
		time.Sleep(retryWait)
	}
}
