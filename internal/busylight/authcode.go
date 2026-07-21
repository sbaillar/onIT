package busylight

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"
)

// BrowserLogin is an in-progress authorization-code + PKCE sign-in. Unlike
// the device-code flow, the whole sign-in runs in the system browser, so it
// carries the device identity that device-based Conditional Access policies
// require. Needs "http://localhost" registered as a Mobile and desktop
// redirect URI on the app registration.
type BrowserLogin struct {
	AuthURL string // open this in the browser

	clientID string
	tenant   string
	verifier string
	state    string
	redirect string
	ln       net.Listener
	result   chan browserResult
	once     sync.Once
}

type browserResult struct {
	code string
	err  error
}

func randToken(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

// StartBrowserLogin opens a loopback listener and builds the authorize URL
// for an auth-code + PKCE sign-in. Tenant defaults to "organizations".
func StartBrowserLogin(clientID, tenant string) (*BrowserLogin, error) {
	if tenant == "" {
		tenant = "organizations"
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	bl := &BrowserLogin{
		clientID: clientID,
		tenant:   tenant,
		verifier: randToken(32),
		state:    randToken(16),
		redirect: fmt.Sprintf("http://localhost:%d", ln.Addr().(*net.TCPAddr).Port),
		ln:       ln,
		result:   make(chan browserResult, 1),
	}
	sum := sha256.Sum256([]byte(bl.verifier))
	bl.AuthURL = loginBase + "/" + tenant + "/oauth2/v2.0/authorize?" + url.Values{
		"client_id":             {clientID},
		"response_type":         {"code"},
		"redirect_uri":          {bl.redirect},
		"scope":                 {graphScope},
		"code_challenge":        {base64.RawURLEncoding.EncodeToString(sum[:])},
		"code_challenge_method": {"S256"},
		"state":                 {bl.state},
	}.Encode()
	go http.Serve(ln, http.HandlerFunc(bl.handle))
	return bl, nil
}

// Cancel aborts WaitForBrowserLogin and closes the loopback listener.
func (bl *BrowserLogin) Cancel() {
	bl.once.Do(func() { bl.result <- browserResult{err: errors.New("sign-in cancelled")} })
}

func (bl *BrowserLogin) handle(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if q.Get("code") == "" && q.Get("error") == "" {
		http.NotFound(w, r) // favicon etc.
		return
	}
	if q.Get("state") != bl.state {
		http.Error(w, "state mismatch", http.StatusBadRequest)
		return
	}
	if e := q.Get("error"); e != "" {
		msg := q.Get("error_description")
		if msg == "" {
			msg = e
		}
		http.Error(w, "Sign-in failed: "+msg, http.StatusBadRequest)
		bl.once.Do(func() { bl.result <- browserResult{err: errors.New(msg)} })
		return
	}
	w.Header().Set("Content-Type", "text/html")
	fmt.Fprint(w, "<html><body><h2>onIT is signed in</h2>You can close this tab.</body></html>")
	bl.once.Do(func() { bl.result <- browserResult{code: q.Get("code")} })
}

// WaitForBrowserLogin waits for the browser redirect, exchanges the code for
// tokens, and stores the refresh token. Blocks; call from a goroutine.
func (g *Graph) WaitForBrowserLogin(bl *BrowserLogin) error {
	defer bl.ln.Close()
	var res browserResult
	select {
	case res = <-bl.result:
	case <-time.After(15 * time.Minute):
		return errors.New("sign-in timed out")
	}
	if res.err != nil {
		return res.err
	}
	resp, err := http.PostForm(loginBase+"/"+bl.tenant+"/oauth2/v2.0/token", url.Values{
		"grant_type":    {"authorization_code"},
		"client_id":     {bl.clientID},
		"code":          {res.code},
		"redirect_uri":  {bl.redirect},
		"code_verifier": {bl.verifier},
		"scope":         {graphScope},
	})
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	var tr struct {
		oauthError
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tr); err != nil {
		return err
	}
	if tr.Error != "" {
		return tr.err()
	}
	g.mu.Lock()
	g.creds = graphCreds{ClientID: bl.clientID, Tenant: bl.tenant, RefreshToken: tr.RefreshToken}
	g.access = tr.AccessToken
	g.expiry = time.Now().Add(time.Duration(tr.ExpiresIn) * time.Second)
	g.saveLocked()
	g.mu.Unlock()
	return nil
}
