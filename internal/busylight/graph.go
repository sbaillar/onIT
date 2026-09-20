package busylight

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
)

// Overridable in tests.
var (
	loginBase = "https://login.microsoftonline.com"
	graphBase = "https://graph.microsoft.com"
)

const graphScope = "offline_access https://graph.microsoft.com/Presence.Read"

// DefaultClientID is the built-in public client used when no custom
// registration is configured. Currently Microsoft Graph PowerShell's
// well-known first-party ID: present in every tenant, so sign-in needs no
// app registration at all (the consent screen shows "Microsoft Graph
// Command Line Tools"). Swap for onIT's own multi-tenant registration
// when available; Presence setup -> Advanced overrides it either way.
const DefaultClientID = "14d82eec-204b-4c2f-b7e8-296a70dab67e"

type graphCreds struct {
	ClientID     string `json:"client_id"`
	Tenant       string `json:"tenant"`
	RefreshToken string `json:"refresh_token"`
}

func graphTokenFile() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".onit_graph.json")
}

// Graph polls Microsoft Graph for the signed-in user's Teams presence.
type Graph struct {
	mu         sync.Mutex
	creds      graphCreds
	access     string
	expiry     time.Time
	reason     string    // why the last refresh was rejected; "" while signed in
	lastSilent time.Time // last SilentReauth attempt (success or not)
}

// SignInRequiredError means the refresh token was rejected and the user (or
// SilentReauth) has to sign in again. Reason is Entra's own explanation,
// usually an AADSTS code naming the policy behind it.
type SignInRequiredError struct{ Reason string }

func (e *SignInRequiredError) Error() string { return "sign-in required: " + e.Reason }

// SignInReason is Entra's explanation for the last rejected refresh, or ""
// while signed in.
func (g *Graph) SignInReason() string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.reason
}

// Overridable in tests.
var (
	silentReauthEvery   = 30 * time.Minute
	silentReauthTimeout = 90 * time.Second
)

// SilentReauth tries to sign back in without the user: it opens the browser
// on the authorize URL with prompt=none, which completes on its own when the
// browser holds a work session (Platform SSO on a managed Mac) and fails fast
// with login_required otherwise. open receives the URL to launch. At most one
// attempt per silentReauthEvery so a policy that rejects every refresh cannot
// keep opening tabs.
func (g *Graph) SilentReauth(open func(authURL string) error) error {
	g.mu.Lock()
	id, tenant, last := g.creds.ClientID, g.creds.Tenant, g.lastSilent
	g.mu.Unlock()
	if id == "" {
		return errors.New("no app registration to sign into")
	}
	if time.Since(last) < silentReauthEvery {
		return errors.New("silent sign-in already attempted recently")
	}
	g.mu.Lock()
	g.lastSilent = time.Now()
	g.mu.Unlock()
	bl, err := StartBrowserLoginPrompt(id, tenant, "none")
	if err != nil {
		return err
	}
	bl.timeout = silentReauthTimeout
	if err := open(bl.AuthURL); err != nil {
		bl.Cancel()
		return err
	}
	if err := g.WaitForBrowserLogin(bl); err != nil {
		g.mu.Lock()
		g.reason = err.Error()
		g.mu.Unlock()
		return err
	}
	return nil
}

// LoadGraph restores a previous sign-in from disk (empty Graph if none).
func LoadGraph() *Graph {
	g := &Graph{}
	if b, err := os.ReadFile(graphTokenFile()); err == nil {
		json.Unmarshal(b, &g.creds)
	}
	return g
}

func (g *Graph) SignedIn() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.creds.RefreshToken != ""
}

// SignOut forgets the tokens and deletes the credential file.
func (g *Graph) SignOut() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.creds = graphCreds{}
	g.access = ""
	g.reason = ""
	os.Remove(graphTokenFile())
}

func (g *Graph) saveLocked() {
	b, _ := json.Marshal(g.creds)
	if err := os.WriteFile(graphTokenFile(), b, 0o600); err != nil {
		return
	}
}

type oauthError struct {
	Error       string `json:"error"`
	Description string `json:"error_description"`
}

func (e *oauthError) err() error {
	if e.Description != "" {
		return errors.New(e.Description)
	}
	return errors.New(e.Error)
}

// DeviceCode is an in-progress device-code sign-in.
type DeviceCode struct {
	UserCode        string `json:"user_code"`
	VerificationURI string `json:"verification_uri"`
	DeviceCode      string `json:"device_code"`
	Interval        int    `json:"interval"`
	ExpiresIn       int    `json:"expires_in"`

	clientID  string
	tenant    string
	cancelled atomic.Bool
}

// Cancel stops WaitForLogin at its next poll.
func (dc *DeviceCode) Cancel() { dc.cancelled.Store(true) }

// StartDeviceLogin asks Azure AD for a device code the user enters at
// microsoft.com/devicelogin. Tenant defaults to "organizations".
func StartDeviceLogin(clientID, tenant string) (*DeviceCode, error) {
	if tenant == "" {
		tenant = "organizations"
	}
	resp, err := http.PostForm(loginBase+"/"+tenant+"/oauth2/v2.0/devicecode", url.Values{
		"client_id": {clientID},
		"scope":     {graphScope},
	})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var dc DeviceCode
	var oe oauthError
	body := json.NewDecoder(resp.Body)
	if resp.StatusCode != http.StatusOK {
		body.Decode(&oe)
		return nil, oe.err()
	}
	if err := body.Decode(&dc); err != nil {
		return nil, err
	}
	dc.clientID, dc.tenant = clientID, tenant
	return &dc, nil
}

// WaitForLogin polls until the user approves the sign-in, then stores the
// refresh token. Blocks; call from a goroutine.
func (g *Graph) WaitForLogin(dc *DeviceCode) error {
	interval := dc.Interval
	if interval <= 0 {
		interval = 5
	}
	deadline := time.Now().Add(time.Duration(dc.ExpiresIn) * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(time.Duration(interval) * time.Second)
		if dc.cancelled.Load() {
			return errors.New("sign-in cancelled")
		}
		resp, err := http.PostForm(loginBase+"/"+dc.tenant+"/oauth2/v2.0/token", url.Values{
			"grant_type":  {"urn:ietf:params:oauth:grant-type:device_code"},
			"client_id":   {dc.clientID},
			"device_code": {dc.DeviceCode},
		})
		if err != nil {
			return err
		}
		var tr struct {
			oauthError
			AccessToken  string `json:"access_token"`
			RefreshToken string `json:"refresh_token"`
			ExpiresIn    int    `json:"expires_in"`
		}
		err = json.NewDecoder(resp.Body).Decode(&tr)
		resp.Body.Close()
		if err != nil {
			return err
		}
		switch tr.Error {
		case "":
			g.mu.Lock()
			g.creds = graphCreds{ClientID: dc.clientID, Tenant: dc.tenant, RefreshToken: tr.RefreshToken}
			g.access = tr.AccessToken
			g.expiry = time.Now().Add(time.Duration(tr.ExpiresIn) * time.Second)
			g.reason = ""
			g.saveLocked()
			g.mu.Unlock()
			return nil
		case "authorization_pending":
		case "slow_down":
			interval += 5
		default:
			return tr.err()
		}
	}
	return errors.New("sign-in timed out")
}

// token returns a valid access token, refreshing if needed.
func (g *Graph) token() (string, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.access != "" && time.Until(g.expiry) > 2*time.Minute {
		return g.access, nil
	}
	if g.creds.RefreshToken == "" {
		return "", errors.New("not signed in")
	}
	resp, err := http.PostForm(loginBase+"/"+g.creds.Tenant+"/oauth2/v2.0/token", url.Values{
		"grant_type":    {"refresh_token"},
		"client_id":     {g.creds.ClientID},
		"refresh_token": {g.creds.RefreshToken},
		"scope":         {graphScope},
	})
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var tr struct {
		oauthError
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tr); err != nil {
		return "", err
	}
	if tr.Error != "" {
		switch tr.Error {
		case "invalid_grant", "interaction_required":
			// Revoked, expired, or a Conditional Access policy demanding a
			// fresh sign-in: the refresh token is dead. Keep the client ID
			// and tenant so SilentReauth knows what to sign back into.
			g.creds.RefreshToken = ""
			g.access = ""
			g.reason = tr.err().Error()
			os.Remove(graphTokenFile())
			return "", &SignInRequiredError{Reason: g.reason}
		}
		return "", tr.err()
	}
	g.access = tr.AccessToken
	g.expiry = time.Now().Add(time.Duration(tr.ExpiresIn) * time.Second)
	if tr.RefreshToken != "" {
		g.creds.RefreshToken = tr.RefreshToken
		g.saveLocked()
	}
	return g.access, nil
}

// Presence fetches the user's Teams presence and maps it to a light state.
func (g *Graph) Presence() (string, error) {
	state, err := g.fetchPresence()
	if err == errUnauthorized { // stale access token: refresh once and retry
		g.mu.Lock()
		g.access = ""
		g.mu.Unlock()
		state, err = g.fetchPresence()
	}
	return state, err
}

var errUnauthorized = errors.New("unauthorized")

func (g *Graph) fetchPresence() (string, error) {
	tok, err := g.token()
	if err != nil {
		return "", err
	}
	req, _ := http.NewRequest("GET", graphBase+"/v1.0/me/presence", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		return "", errUnauthorized
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("graph: %s", resp.Status)
	}
	var pr struct {
		Availability string `json:"availability"`
		Activity     string `json:"activity"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&pr); err != nil {
		return "", err
	}
	return mapPresence(pr.Availability, pr.Activity), nil
}

// mapPresence converts Graph availability/activity to a firmware state.
// Anything busy-shaped is "meeting"; the agent promotes it to "call" while
// the microphone is live (see Agent.effectiveLocked).
func mapPresence(availability, activity string) string {
	switch activity {
	case "Presenting":
		return "sharing"
	case "InACall", "InAMeeting":
		return "meeting"
	}
	switch availability {
	case "DoNotDisturb":
		return "sharing" // device screen reads "Do not disturb"
	case "Busy", "BusyIdle":
		return "meeting" // busy is busy, call or not
	case "Available", "AvailableIdle", "Away", "BeRightBack":
		return "available"
	}
	return "off" // Offline, PresenceUnknown
}
