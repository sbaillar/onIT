package busylight

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestRemoteHandlerAcceptsStateAndRejectsJunk(t *testing.T) {
	a := NewAgent()
	srv := httptest.NewServer(a.RemoteHandler("sekrit"))
	defer srv.Close()

	if err := PushState(srv.URL, "sekrit", "meeting"); err != nil {
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

	if err := PushState(srv.URL, "sekrit", "purple"); err == nil {
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

func TestRemoteHandlerRequiresToken(t *testing.T) {
	a := NewAgent()
	srv := httptest.NewServer(a.RemoteHandler("sekrit"))
	defer srv.Close()

	// wrong and missing tokens are rejected without touching state
	if err := PushState(srv.URL, "wrong", "meeting"); err == nil {
		t.Error("PushState succeeded with a wrong token")
	}
	resp, err := srv.Client().Post(srv.URL+"/presence", "text/plain",
		strings.NewReader("meeting")) // plain CSRF-style POST, no header
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 401 {
		t.Errorf("tokenless POST = %d, want 401", resp.StatusCode)
	}
	a.mu.Lock()
	state := a.remoteState
	a.mu.Unlock()
	if state != "" {
		t.Errorf("remoteState = %q after rejected pushes, want empty", state)
	}
}

func TestRemoteSessionFollowsPushesThenGoesStale(t *testing.T) {
	oldStale, oldTick := remoteStale, remoteTick
	remoteStale, remoteTick = 80*time.Millisecond, 10*time.Millisecond
	defer func() { remoteStale, remoteTick = oldStale, oldTick }()

	a := NewAgent()
	srv := httptest.NewServer(a.RemoteHandler("sekrit"))
	defer srv.Close()
	if err := PushState(srv.URL, "sekrit", "sharing"); err != nil {
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
	t.Setenv("HOME", t.TempDir()) // token file lives in $HOME
	a := NewAgent()
	rs, err := a.ListenRemote("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	if rs.Token() == "" {
		t.Fatal("ListenRemote generated no token")
	}
	if err := PushState("http://"+rs.Addr(), rs.Token(), "available"); err != nil {
		t.Fatal(err)
	}
	a.mu.Lock()
	state := a.remoteState
	a.mu.Unlock()
	if state != "available" {
		t.Errorf("remoteState = %q, want available", state)
	}

	// the token persists, so sender config survives receiver restarts
	rs2, err := a.ListenRemote("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	if rs2.Token() != rs.Token() {
		t.Error("token changed between listeners")
	}
	rs2.Close()

	rs.Close()
	if err := PushState("http://"+rs.Addr(), rs.Token(), "off"); err == nil {
		t.Error("PushState succeeded after Close")
	}
}
