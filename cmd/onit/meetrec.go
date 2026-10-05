package main

import (
	"image/color"
	"log"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"

	"onit/internal/busylight"
	"onit/internal/recorder"
)

// recordKey: record Teams meetings for transcription. On unless turned off.
const recordKey = "recordMeetings"

// meetingRecordingsDir is where finished meetings land for meetnote: local,
// not the iCloud vault (a 16 kHz stereo WAV is ~230 MB an hour).
func meetingRecordingsDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".meetnote", "recordings")
}

// meetingRecorder records while agent.InCall() holds (Teams meeting + live
// mic), debounced by recorder.Trigger.
type meetingRecorder struct {
	Check *widget.Check     // the Settings toggle
	Dot   fyne.CanvasObject // red dot shown on the face while recording

	agent   *busylight.Agent
	enabled atomic.Bool
	mu      sync.Mutex
	trig    recorder.Trigger
	rec     *recorder.Recording
	warned  bool // a start failure was already reported this session
	app     fyne.App
	dot     *canvas.Circle
	dotWrap *fyne.Container
}

func newMeetingRecorder(a fyne.App, agent *busylight.Agent) *meetingRecorder {
	m := &meetingRecorder{agent: agent, app: a}
	m.dot = canvas.NewCircle(color.NRGBA{0xE0, 0x20, 0x20, 0xFF})
	m.dotWrap = container.NewPadded(container.NewGridWrap(fyne.NewSize(12, 12), m.dot))
	m.dotWrap.Hide()
	m.Dot = m.dotWrap

	on := a.Preferences().BoolWithFallback(recordKey, true)
	m.enabled.Store(on)
	m.Check = widget.NewCheck("Record Teams meetings for transcription", nil)
	m.Check.SetChecked(on)
	m.Check.OnChanged = func(on bool) {
		a.Preferences().SetBool(recordKey, on)
		m.enabled.Store(on)
		if !on {
			m.StopNow() // turning it off ends a recording in progress
		}
	}
	if !recorder.Supported {
		m.Check.Hide() // no capture backend on this platform
	}
	return m
}

// Run polls the call state; call once on its own goroutine.
func (m *meetingRecorder) Run() {
	if !recorder.Supported {
		return
	}
	for {
		time.Sleep(2 * time.Second)
		live := m.enabled.Load() && m.agent.InCall()
		m.mu.Lock()
		start, stop := m.trig.Update(live, time.Now())
		m.mu.Unlock()
		switch {
		case start:
			m.start()
		case stop:
			m.finish()
		}
	}
}

func (m *meetingRecorder) start() {
	rec, err := recorder.Start(meetingRecordingsDir())
	if err != nil {
		log.Printf("meeting recording: %v", err)
		m.mu.Lock()
		m.trig.Reset()
		first := !m.warned
		m.warned = true
		m.mu.Unlock()
		if first {
			m.app.SendNotification(fyne.NewNotification("onIT can't record this meeting", err.Error()))
		}
		return
	}
	log.Print("meeting recording started")
	m.mu.Lock()
	m.rec = rec
	m.mu.Unlock()
	fyne.Do(m.dotWrap.Show)
}

func (m *meetingRecorder) finish() {
	m.mu.Lock()
	rec := m.rec
	m.rec = nil
	m.mu.Unlock()
	if rec == nil {
		return
	}
	path, err := rec.Stop()
	if err != nil {
		log.Printf("meeting recording %s: %v", path, err)
	} else {
		log.Printf("meeting recording saved: %s", path)
	}
	fyne.Do(m.dotWrap.Hide)
}

// StopNow ends any recording at once (setting turned off, app quitting).
func (m *meetingRecorder) StopNow() {
	m.mu.Lock()
	m.trig.Reset()
	m.mu.Unlock()
	m.finish()
}
