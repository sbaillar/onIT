package busylight

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestRemoteHandlerAcceptsStateAndRejectsJunk(t *testing.T) {
	a := NewAgent()
	srv := httptest.NewServer(a.RemoteHandler())
	defer srv.Close()

	if err := PushState(srv.URL, "meeting"); err != nil {
		t.Fatal(err)
	}
	a.mu.Lock()
	state, at := a.remoteState, a.remoteAt
	a.mu.Unlock()
	if state != "meeting" {
		t.Errorf("remoteState = %q, want meeting", state)
	}
	if time.Since(at) > time.Minute {
		t.Error("remoteAt not stamped")
	}

	if err := PushState(srv.URL, "purple"); err == nil {
		t.Error("PushState accepted an unknown state")
	}
	resp, err := srv.Client().Get(srv.URL + "/presence")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 405 {
		t.Errorf("GET /presence = %d, want 405", resp.StatusCode)
	}
}

func TestRemoteSessionFollowsPushesThenGoesStale(t *testing.T) {
	oldStale, oldTick := remoteStale, remoteTick
	remoteStale, remoteTick = 80*time.Millisecond, 10*time.Millisecond
	defer func() { remoteStale, remoteTick = oldStale, oldTick }()

	a := NewAgent()
	srv := httptest.NewServer(a.RemoteHandler())
	defer srv.Close()
	if err := PushState(srv.URL, "sharing"); err != nil {
		t.Fatal(err)
	}
	if !a.remoteFresh() {
		t.Fatal("remoteFresh = false right after a push")
	}

	done := make(chan error, 1)
	go func() { done <- a.remoteSession() }()

	deadline := time.After(2 * time.Second)
	for a.Status().Shown != "sharing" {
		select {
		case <-deadline:
			t.Fatalf("Shown = %q, want sharing", a.Status().Shown)
		case <-time.After(5 * time.Millisecond):
		}
	}

	// no more pushes: the session must end once the data is stale
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "stale") {
			t.Errorf("remoteSession err = %v, want stale", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("remoteSession did not end after pushes stopped")
	}
	if a.remoteFresh() {
		t.Error("remoteFresh = true after staleness")
	}
}

func TestListenRemote(t *testing.T) {
	a := NewAgent()
	rs, err := a.ListenRemote("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	if err := PushState("http://"+rs.Addr(), "available"); err != nil {
		t.Fatal(err)
	}
	a.mu.Lock()
	state := a.remoteState
	a.mu.Unlock()
	if state != "available" {
		t.Errorf("remoteState = %q, want available", state)
	}
	rs.Close()
	if err := PushState("http://"+rs.Addr(), "off"); err == nil {
		t.Error("PushState succeeded after Close")
	}
}
