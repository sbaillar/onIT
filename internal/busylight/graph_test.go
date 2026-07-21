package busylight

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
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
		{"Busy", "Busy", "available"}, // calendar-busy, no call: calls-only red
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

func TestGraphBrowserLoginAndPresence(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	var challenge atomic.Value // code_challenge from the authorize URL
	mux := http.NewServeMux()
	mux.HandleFunc("/organizations/oauth2/v2.0/token", func(w http.ResponseWriter, r *http.Request) {
		if r.FormValue("grant_type") != "authorization_code" {
			t.Errorf("grant_type = %q", r.FormValue("grant_type"))
		}
		if r.FormValue("client_id") != "test-client" {
			t.Errorf("client_id = %q", r.FormValue("client_id"))
		}
		if r.FormValue("code") != "auth-1" {
			t.Errorf("code = %q", r.FormValue("code"))
		}
		if !strings.HasPrefix(r.FormValue("redirect_uri"), "http://localhost:") {
			t.Errorf("redirect_uri = %q", r.FormValue("redirect_uri"))
		}
		sum := sha256.Sum256([]byte(r.FormValue("code_verifier")))
		if got := base64.RawURLEncoding.EncodeToString(sum[:]); got != challenge.Load() {
			t.Errorf("code_verifier does not hash to code_challenge (got %q)", got)
		}
		json.NewEncoder(w).Encode(map[string]any{
			"access_token": "at-1", "refresh_token": "rt-1", "expires_in": 3600,
		})
	})
	mux.HandleFunc("/v1.0/me/presence", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer at-1" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		json.NewEncoder(w).Encode(map[string]string{
			"availability": "Busy", "activity": "InACall",
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	oldLogin, oldGraph := loginBase, graphBase
	loginBase, graphBase = srv.URL, srv.URL
	defer func() { loginBase, graphBase = oldLogin, oldGraph }()

	bl, err := StartBrowserLogin("test-client", "")
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(bl.AuthURL)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	if q.Get("client_id") != "test-client" || q.Get("response_type") != "code" ||
		q.Get("code_challenge_method") != "S256" {
		t.Fatalf("unexpected authorize URL: %s", bl.AuthURL)
	}
	if q.Get("code_challenge") == "" || q.Get("state") == "" {
		t.Fatalf("authorize URL missing challenge or state: %s", bl.AuthURL)
	}
	challenge.Store(q.Get("code_challenge"))
	redirect := q.Get("redirect_uri")
	if !strings.HasPrefix(redirect, "http://localhost:") {
		t.Fatalf("redirect_uri = %q", redirect)
	}

	// act as the browser: a wrong state must be rejected...
	resp, err := http.Get(redirect + "/?code=evil&state=wrong")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("wrong state: status = %d, want 400", resp.StatusCode)
	}
	// ...then the real redirect completes the sign-in
	go http.Get(redirect + "/?code=auth-1&state=" + url.QueryEscape(q.Get("state")))

	g := &Graph{}
	if err := g.WaitForBrowserLogin(bl); err != nil {
		t.Fatal(err)
	}
	if !g.SignedIn() {
		t.Fatal("not signed in after WaitForBrowserLogin")
	}
	state, err := g.Presence()
	if err != nil {
		t.Fatal(err)
	}
	if state != "meeting" {
		t.Errorf("Presence = %q, want meeting", state)
	}
	if !LoadGraph().SignedIn() {
		t.Error("browser login was not persisted to disk")
	}
}

func TestBrowserLoginCancel(t *testing.T) {
	bl, err := StartBrowserLogin("test-client", "")
	if err != nil {
		t.Fatal(err)
	}
	bl.Cancel()
	g := &Graph{}
	if err := g.WaitForBrowserLogin(bl); err == nil {
		t.Fatal("WaitForBrowserLogin returned nil after Cancel")
	}
}
