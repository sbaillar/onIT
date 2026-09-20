package busylight

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// fakeLogin stands in for login.microsoftonline.com. refresh answers the
// refresh_token grant, code the authorization_code grant.
func fakeLogin(t *testing.T, refresh, code func(w http.ResponseWriter, r *http.Request)) {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/organizations/oauth2/v2.0/token", func(w http.ResponseWriter, r *http.Request) {
		switch r.FormValue("grant_type") {
		case "refresh_token":
			refresh(w, r)
		case "authorization_code":
			code(w, r)
		default:
			t.Errorf("unexpected grant_type %q", r.FormValue("grant_type"))
		}
	})
	mux.HandleFunc("/v1.0/me/presence", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{"availability": "Available", "activity": "Available"})
	})
	srv := httptest.NewServer(mux)
	oldLogin, oldGraph := loginBase, graphBase
	loginBase, graphBase = srv.URL, srv.URL
	t.Cleanup(func() { loginBase, graphBase = oldLogin, oldGraph; srv.Close() })
}

func oauthFail(code, desc string) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": code, "error_description": desc})
	}
}

func signedInGraph(t *testing.T) *Graph {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	g := &Graph{creds: graphCreds{ClientID: "test-client", Tenant: "organizations", RefreshToken: "rt-old"}}
	g.mu.Lock()
	g.saveLocked()
	g.mu.Unlock()
	return g
}

func TestRefreshRejectedReportsSignInRequired(t *testing.T) {
	for _, code := range []string{"invalid_grant", "interaction_required"} {
		t.Run(code, func(t *testing.T) {
			fakeLogin(t, oauthFail(code, "AADSTS50076: sign-in frequency policy"), nil)
			g := signedInGraph(t)

			_, err := g.Presence()
			var sir *SignInRequiredError
			if !errors.As(err, &sir) {
				t.Fatalf("Presence err = %v, want *SignInRequiredError", err)
			}
			if !strings.Contains(sir.Reason, "AADSTS50076") {
				t.Errorf("Reason = %q, want the AADSTS text", sir.Reason)
			}
			if g.SignedIn() {
				t.Error("still SignedIn after the refresh token was rejected")
			}
			if g.SignInReason() != sir.Reason {
				t.Errorf("SignInReason = %q, want %q", g.SignInReason(), sir.Reason)
			}
			if _, err := os.Stat(graphTokenFile()); !os.IsNotExist(err) {
				t.Error("token file survived the rejection")
			}
		})
	}
}

func TestBrowserLoginPromptParam(t *testing.T) {
	bl, err := StartBrowserLoginPrompt("c", "", "none")
	if err != nil {
		t.Fatal(err)
	}
	bl.Cancel()
	u, _ := url.Parse(bl.AuthURL)
	if got := u.Query().Get("prompt"); got != "none" {
		t.Errorf("prompt = %q, want none", got)
	}
	plain, err := StartBrowserLogin("c", "")
	if err != nil {
		t.Fatal(err)
	}
	plain.Cancel()
	u, _ = url.Parse(plain.AuthURL)
	if u.Query().Has("prompt") {
		t.Errorf("StartBrowserLogin must not set prompt: %s", plain.AuthURL)
	}
}

// browserThatRedirects acts as a browser holding a work session: it never
// shows a page, it just follows the authorize URL straight to the redirect.
func browserThatRedirects(t *testing.T, query string) func(string) error {
	return func(authURL string) error {
		u, err := url.Parse(authURL)
		if err != nil {
			return err
		}
		q := u.Query()
		if q.Get("prompt") != "none" {
			t.Errorf("silent re-auth must use prompt=none, got %q", q.Get("prompt"))
		}
		go http.Get(q.Get("redirect_uri") + "/?" + query + "&state=" + url.QueryEscape(q.Get("state")))
		return nil
	}
}

func TestSilentReauthRestoresSignIn(t *testing.T) {
	var exchanged atomic.Bool
	fakeLogin(t, oauthFail("invalid_grant", "AADSTS50076: expired"), func(w http.ResponseWriter, r *http.Request) {
		if r.FormValue("client_id") != "test-client" || r.FormValue("code") != "auth-2" {
			t.Errorf("code exchange got client=%q code=%q", r.FormValue("client_id"), r.FormValue("code"))
		}
		exchanged.Store(true)
		json.NewEncoder(w).Encode(map[string]any{"access_token": "at-2", "refresh_token": "rt-2", "expires_in": 3600})
	})
	g := signedInGraph(t)
	g.Presence() // rejected: signs out, remembers the reason

	if err := g.SilentReauth(browserThatRedirects(t, "code=auth-2")); err != nil {
		t.Fatalf("SilentReauth: %v", err)
	}
	if !exchanged.Load() || !g.SignedIn() {
		t.Fatal("not signed back in after silent re-auth")
	}
	if g.SignInReason() != "" {
		t.Errorf("SignInReason = %q after success, want empty", g.SignInReason())
	}
	if !LoadGraph().SignedIn() {
		t.Error("re-auth was not persisted")
	}
	if _, err := g.Presence(); err != nil {
		t.Errorf("Presence after re-auth: %v", err)
	}
}

func TestSilentReauthInteractionRequiredKeepsReason(t *testing.T) {
	fakeLogin(t, oauthFail("invalid_grant", "AADSTS50076: expired"), func(w http.ResponseWriter, r *http.Request) {
		t.Error("code exchange must not happen when the browser reports login_required")
	})
	g := signedInGraph(t)
	g.Presence()

	err := g.SilentReauth(browserThatRedirects(t, "error=login_required&error_description=AADSTS50058%3A+silent+sign-in+failed"))
	if err == nil || !strings.Contains(err.Error(), "AADSTS50058") {
		t.Fatalf("SilentReauth err = %v, want the AADSTS50058 description", err)
	}
	if g.SignedIn() {
		t.Error("SignedIn after a failed silent re-auth")
	}
	if !strings.Contains(g.SignInReason(), "AADSTS50058") {
		t.Errorf("SignInReason = %q, want the browser's reason", g.SignInReason())
	}
}

func TestSilentReauthRateLimited(t *testing.T) {
	fakeLogin(t, oauthFail("invalid_grant", "x"), func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"access_token": "at", "refresh_token": "rt", "expires_in": 3600})
	})
	g := signedInGraph(t)
	g.Presence()
	opens := 0
	open := func(authURL string) error {
		opens++
		return browserThatRedirects(t, "code=c")(authURL)
	}
	if err := g.SilentReauth(open); err != nil {
		t.Fatal(err)
	}
	g.Presence() // rejected again straight away (policy loop)
	if err := g.SilentReauth(open); err == nil {
		t.Fatal("second silent re-auth within the window must be refused")
	}
	if opens != 1 {
		t.Errorf("browser opened %d times, want 1", opens)
	}
}

func TestSilentReauthWithoutCredsFails(t *testing.T) {
	g := &Graph{}
	if err := g.SilentReauth(func(string) error { t.Error("opened a browser with no app to sign into"); return nil }); err == nil {
		t.Fatal("SilentReauth with no client ID must fail")
	}
}

func TestGraphSessionCallsSignInRequiredHook(t *testing.T) {
	fakeLogin(t, oauthFail("invalid_grant", "AADSTS50076: policy"), nil)
	a := NewAgent()
	a.Graph = signedInGraph(t)
	got := make(chan string, 1)
	a.SetOnSignInRequired(func(reason string) { got <- reason })

	err := a.graphSession()
	var handover *sourceSwitch
	if !errors.As(err, &handover) {
		t.Fatalf("graphSession err = %v, want a sourceSwitch handover", err)
	}
	select {
	case r := <-got:
		if !strings.Contains(r, "AADSTS50076") {
			t.Errorf("hook reason = %q", r)
		}
	case <-time.After(time.Second):
		t.Fatal("sign-in-required hook was not called")
	}
	if st := a.Status(); st.SignInNeeded == "" {
		t.Error("Status.SignInNeeded empty after the refresh token was rejected")
	}
}

func TestTeamsLogSessionHandsOverWhenGraphSignsIn(t *testing.T) {
	dir := t.TempDir()
	oldGlobs, oldTick, oldRunning := teamsLogGlobs, teamsLogTick, teamsClientRunning
	teamsLogGlobs = []string{filepath.Join(dir, "MSTeams_*.log")}
	teamsLogTick = 10 * time.Millisecond
	teamsClientRunning = func() bool { return true }
	defer func() { teamsLogGlobs, teamsLogTick, teamsClientRunning = oldGlobs, oldTick, oldRunning }()
	path := filepath.Join(dir, "MSTeams_2026-09-20.log")
	os.WriteFile(path, []byte("OnAvailabilityUpdate Received availability update: Busy\n"), 0o644)

	t.Setenv("HOME", t.TempDir())
	a := NewAgent()
	done := make(chan error, 1)
	go func() { done <- a.teamsLogSession() }()
	waitFor(t, func() bool { return a.Status().Shown == "meeting" }, "seed state")

	a.Graph.mu.Lock()
	a.Graph.creds.RefreshToken = "rt-new" // silent re-auth just landed
	a.Graph.mu.Unlock()
	select {
	case err := <-done:
		var handover *sourceSwitch
		if !errors.As(err, &handover) {
			t.Fatalf("teamsLogSession err = %v, want a handover", err)
		}
	case <-time.After(time.Second):
		t.Fatal("teams log session kept running after Graph signed in")
	}
}
