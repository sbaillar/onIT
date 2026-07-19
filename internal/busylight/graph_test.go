package busylight

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestMapPresence(t *testing.T) {
	cases := []struct{ avail, activity, want string }{
		{"Available", "Available", "available"},
		{"AvailableIdle", "InactiveStatus", "available"},
		{"Away", "Away", "available"},
		{"BeRightBack", "BeRightBack", "available"},
		{"Busy", "InACall", "meeting"},
		{"Busy", "InAMeeting", "meeting"},
		{"Busy", "Busy", "meeting"},
		{"DoNotDisturb", "Presenting", "sharing"},
		{"DoNotDisturb", "DoNotDisturb", "sharing"},
		{"Offline", "OffWork", "off"},
		{"PresenceUnknown", "PresenceUnknown", "off"},
	}
	for _, c := range cases {
		if got := mapPresence(c.avail, c.activity); got != c.want {
			t.Errorf("mapPresence(%s, %s) = %q, want %q", c.avail, c.activity, got, c.want)
		}
	}
}

// fakeAAD serves the device-code, token, and presence endpoints.
func fakeAAD(t *testing.T, tokenPolls *atomic.Int32) *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/organizations/oauth2/v2.0/devicecode", func(w http.ResponseWriter, r *http.Request) {
		if r.FormValue("client_id") != "test-client" {
			t.Errorf("devicecode client_id = %q", r.FormValue("client_id"))
		}
		json.NewEncoder(w).Encode(map[string]any{
			"user_code": "ABC-123", "device_code": "dev-1",
			"verification_uri": "https://example.test/login",
			"interval":         1, "expires_in": 30,
		})
	})
	mux.HandleFunc("/organizations/oauth2/v2.0/token", func(w http.ResponseWriter, r *http.Request) {
		switch r.FormValue("grant_type") {
		case "urn:ietf:params:oauth:grant-type:device_code":
			if tokenPolls.Add(1) == 1 { // pending on the first poll
				json.NewEncoder(w).Encode(map[string]string{"error": "authorization_pending"})
				return
			}
			json.NewEncoder(w).Encode(map[string]any{
				"access_token": "at-1", "refresh_token": "rt-1", "expires_in": 3600,
			})
		case "refresh_token":
			if r.FormValue("refresh_token") != "rt-1" {
				t.Errorf("refresh_token = %q", r.FormValue("refresh_token"))
			}
			json.NewEncoder(w).Encode(map[string]any{
				"access_token": "at-2", "refresh_token": "rt-2", "expires_in": 3600,
			})
		}
	})
	mux.HandleFunc("/v1.0/me/presence", func(w http.ResponseWriter, r *http.Request) {
		switch r.Header.Get("Authorization") {
		case "Bearer at-1", "Bearer at-2":
			json.NewEncoder(w).Encode(map[string]string{
				"availability": "Busy", "activity": "InACall",
			})
		default:
			w.WriteHeader(http.StatusUnauthorized)
		}
	})
	return httptest.NewServer(mux)
}

func TestGraphDeviceFlowAndPresence(t *testing.T) {
	t.Setenv("HOME", t.TempDir()) // keep ~/.onit_graph.json out of the real home
	var polls atomic.Int32
	srv := fakeAAD(t, &polls)
	defer srv.Close()
	oldLogin, oldGraph := loginBase, graphBase
	loginBase, graphBase = srv.URL, srv.URL
	defer func() { loginBase, graphBase = oldLogin, oldGraph }()

	dc, err := StartDeviceLogin("test-client", "")
	if err != nil {
		t.Fatal(err)
	}
	if dc.UserCode != "ABC-123" || dc.VerificationURI != "https://example.test/login" {
		t.Fatalf("unexpected device code: %+v", dc)
	}

	g := &Graph{}
	if err := g.WaitForLogin(dc); err != nil {
		t.Fatal(err)
	}
	if polls.Load() < 2 {
		t.Errorf("expected pending then success, got %d polls", polls.Load())
	}
	if !g.SignedIn() {
		t.Fatal("not signed in after WaitForLogin")
	}

	// uses the access token from login
	state, err := g.Presence()
	if err != nil {
		t.Fatal(err)
	}
	if state != "meeting" {
		t.Errorf("Presence = %q, want meeting", state)
	}

	// a fresh Graph (as after app restart) must refresh from disk
	g2 := LoadGraph()
	if !g2.SignedIn() {
		t.Fatal("LoadGraph lost the sign-in")
	}
	state, err = g2.Presence()
	if err != nil {
		t.Fatal(err)
	}
	if state != "meeting" {
		t.Errorf("Presence after reload = %q, want meeting", state)
	}

	g2.SignOut()
	if g2.SignedIn() || LoadGraph().SignedIn() {
		t.Error("SignOut did not clear credentials")
	}
}
