package busylight

import (
	"log"
	"sync"
	"sync/atomic"
	"time"
)

const heartbeat = 2 * time.Second // firmware watchdog is 5s

// States accepted by the firmware, in display order ("off" last).
var States = []string{"available", "meeting", "sharing", "off"}

// Status is a snapshot of the agent for UIs.
type Status struct {
	TeamsConnected bool
	LightConnected bool
	Source         string // "graph", "teams", or "" while down
	Override       string // "" = auto (follow Teams)
	Shown          string // state currently sent to the device
	DeviceFW       string // firmware version the device reported; "" = unknown
}

// Agent drives the light from Teams presence, with an optional manual override.
// All serial writes happen on a single push goroutine, so a state change can
// never be overwritten by a concurrent stale heartbeat.
type Agent struct {
	light     *Light
	Graph     *Graph        // Microsoft Graph presence source (preferred)
	kick      chan struct{} // wakes the push goroutine after a state change
	flashing  atomic.Bool   // suspends serial pushes while esptool owns the port
	micRule   atomic.Bool   // escalate available -> meeting while the mic is live
	micActive atomic.Bool   // last observed microphone state

	mu          sync.Mutex
	teamsUp     bool
	teamsState  string // last state derived from Teams; "off" while disconnected
	source      string // active presence source: "remote", "graph", "teams", ""
	remoteState string // last state pushed by a remote agent (see remote.go)
	remoteAt    time.Time
	override    string // "" = auto
	last        Status // last status delivered to onChange
	onChange    func()
}

func NewAgent() *Agent {
	a := &Agent{
		light:      NewLight(),
		Graph:      LoadGraph(),
		kick:       make(chan struct{}, 1),
		teamsState: "off",
	}
	a.light.SetOnTouch(a.HandleTouch)
	return a
}

// HandleTouch reacts to a touch event from the device: a tap cycles the
// manual override (and dismisses emoji/custom screens), a long press
// toggles do-not-disturb.
func (a *Agent) HandleTouch(kind string) {
	a.mu.Lock()
	cur := a.override
	a.mu.Unlock()
	switch kind {
	case "TAP":
		next, ok := map[string]string{
			"": "available", "available": "meeting",
			"meeting": "sharing", "sharing": "off", "off": "",
		}[cur]
		if !ok {
			next = "" // emoji or custom screen: tap dismisses to auto
		}
		a.SetOverride(next)
	case "LONG":
		if cur == "sharing" {
			a.SetOverride("")
		} else {
			a.SetOverride("sharing")
		}
	}
}

// OnChange registers a callback fired when Status actually changes.
// Must be set before Run.
func (a *Agent) OnChange(f func()) { a.onChange = f }

// SetMicRule turns the "live microphone shows In a call" rule on or off.
func (a *Agent) SetMicRule(on bool) {
	a.micRule.Store(on)
	a.wake()
}

// effectiveLocked returns the state the light should show. Caller holds mu.
func (a *Agent) effectiveLocked() string {
	if a.override != "" {
		return a.override
	}
	if a.teamsState == "available" && a.micRule.Load() && a.micActive.Load() {
		return "meeting" // on a call the presence source doesn't know about
	}
	return a.teamsState
}

func (a *Agent) Status() Status {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.statusLocked()
}

func (a *Agent) statusLocked() Status {
	return Status{
		TeamsConnected: a.teamsUp,
		LightConnected: a.light.Connected(),
		Source:         a.source,
		Override:       a.override,
		Shown:          a.effectiveLocked(),
		DeviceFW:       a.light.Version(),
	}
}

// SetOverride forces a state on the light; "" returns to auto (Teams).
func (a *Agent) SetOverride(state string) {
	a.mu.Lock()
	a.override = state
	a.mu.Unlock()
	a.wake()
}

// ShowEmoji sends a pre-encoded emoji payload (see internal/emoji) and
// overrides the state to "emoji". Blocks during transfer; call from a
// goroutine. The image is not resent on reconnect - pick again if the
// device is replugged.
func (a *Agent) ShowEmoji(payloadB64 string) bool {
	a.mu.Lock()
	a.override = "emoji"
	a.mu.Unlock()
	ok := a.light.SendLine("EMOJI:" + payloadB64)
	a.wake()
	return ok
}

func (a *Agent) setTeams(up bool, state string) {
	a.mu.Lock()
	changed := a.teamsUp != up || a.teamsState != state
	a.teamsUp = up
	a.teamsState = state
	auto := a.override == ""
	a.mu.Unlock()
	if !changed {
		return
	}
	if auto {
		log.Printf("state -> %s", state)
	}
	a.wake()
}

// wake nudges the push goroutine; coalesces if one is already pending.
func (a *Agent) wake() {
	select {
	case a.kick <- struct{}{}:
	default:
	}
}

// notify fires onChange if the status differs from the last one delivered.
func (a *Agent) notify() {
	a.mu.Lock()
	st := a.statusLocked()
	changed := st != a.last
	a.last = st
	cb := a.onChange
	a.mu.Unlock()
	if changed && cb != nil {
		cb()
	}
}

func (a *Agent) setSource(s string) {
	a.mu.Lock()
	a.source = s
	a.mu.Unlock()
	a.notify()
}

const graphPoll = 5 * time.Second

// graphSession polls Microsoft Graph until it errors or the user signs out.
func (a *Agent) graphSession() error {
	for {
		if !a.Graph.SignedIn() {
			return errNotSignedIn
		}
		state, err := a.Graph.Presence()
		if err != nil {
			return err
		}
		a.setTeams(true, state)
		time.Sleep(graphPoll)
	}
}

var errNotSignedIn = &sourceSwitch{"graph signed out"}

type sourceSwitch struct{ msg string }

func (e *sourceSwitch) Error() string { return e.msg }

// Run blocks forever: pushes states to the device and maintains the presence
// session — Microsoft Graph when signed in, the legacy Teams local WebSocket
// otherwise. The ticker doubles as the heartbeat for the firmware watchdog.
func (a *Agent) Run() {
	go func() {
		tick := time.NewTicker(heartbeat)
		for {
			select {
			case <-a.kick:
			case <-tick.C:
			}
			if a.flashing.Load() {
				continue // esptool owns the port; don't reopen it mid-flash
			}
			a.mu.Lock()
			state := a.effectiveLocked()
			a.mu.Unlock()
			a.light.Send(state)
			a.notify()
		}
	}()
	go func() { // watch the microphone for the mic rule
		for {
			if a.micRule.Load() {
				if now := micInUse(); now != a.micActive.Load() {
					a.micActive.Store(now)
					a.wake()
				}
			}
			time.Sleep(3 * time.Second)
		}
	}()
	for {
		var err error
		if a.remoteFresh() {
			a.setSource("remote")
			err = a.remoteSession()
		} else if a.Graph.SignedIn() {
			a.setSource("graph")
			err = a.graphSession()
		} else if teamsLogAvailable() {
			a.setSource("teamslog")
			err = a.teamsLogSession()
		} else {
			a.setSource("teams")
			err = a.session()
		}
		log.Printf("presence source down (%v)", err)
		a.setSource("")
		a.setTeams(false, "off")
		time.Sleep(retryWait)
	}
}
