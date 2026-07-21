package busylight

import (
	"crypto/subtle"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// Remote presence relay: a machine that can sign in to Microsoft Graph (e.g.
// an org-managed work computer behind device-based Conditional Access) runs
// `onitctl -forward` and pushes its presence here, to the onIT that owns the
// light. The receiver treats pushes as a presence source that outranks Graph
// and the legacy Teams API while fresh.

// Overridable in tests.
var (
	remoteStale = 15 * time.Second // sender pushes every graphPoll (5s)
	remoteTick  = time.Second
)

// SetRemote records a state pushed from a remote agent.
func (a *Agent) SetRemote(state string) {
	a.mu.Lock()
	a.remoteState, a.remoteAt = state, time.Now()
	a.mu.Unlock()
}

func (a *Agent) remoteFresh() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.remoteState != "" && time.Since(a.remoteAt) <= remoteStale
}

// remoteSession follows pushed states until they go stale.
func (a *Agent) remoteSession() error {
	for {
		a.mu.Lock()
		state, at := a.remoteState, a.remoteAt
		a.mu.Unlock()
		if time.Since(at) > remoteStale {
			return &sourceSwitch{"remote presence stale"}
		}
		a.setTeams(true, state)
		time.Sleep(remoteTick)
	}
}

// tokenHeader authenticates pushes. A custom header doubles as CSRF/DNS-
// rebinding protection: browsers cannot attach it cross-origin without a
// preflight, which this server never grants.
const tokenHeader = "X-Onit-Token"

func remoteTokenFile() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".onit_remote_token")
}

// loadOrCreateRemoteToken returns the shared relay secret, minting and
// persisting one on first use.
func loadOrCreateRemoteToken() (string, error) {
	if b, err := os.ReadFile(remoteTokenFile()); err == nil {
		if tok := strings.TrimSpace(string(b)); tok != "" {
			return tok, nil
		}
	}
	tok := randToken(16)
	if err := os.WriteFile(remoteTokenFile(), []byte(tok), 0o600); err != nil {
		return "", err
	}
	return tok, nil
}

// RemoteHandler accepts POST /presence with the state as the request body,
// authenticated by the shared token.
func (a *Agent) RemoteHandler(token string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/presence", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}
		if subtle.ConstantTimeCompare([]byte(r.Header.Get(tokenHeader)), []byte(token)) != 1 {
			http.Error(w, "bad token", http.StatusUnauthorized)
			return
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, 64))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		state := strings.TrimSpace(string(body))
		if !slices.Contains(States, state) {
			http.Error(w, "unknown state", http.StatusBadRequest)
			return
		}
		a.SetRemote(state)
		w.WriteHeader(http.StatusNoContent)
	})
	return mux
}

// RemoteServer is a running remote-presence listener.
type RemoteServer struct {
	ln    net.Listener
	srv   *http.Server
	token string
}

func (rs *RemoteServer) Addr() string  { return rs.ln.Addr().String() }
func (rs *RemoteServer) Token() string { return rs.token }
func (rs *RemoteServer) Close()        { rs.srv.Close() }

// ListenRemote starts accepting presence pushes on addr (e.g. ":8125"),
// requiring the persisted shared token (created on first use).
func (a *Agent) ListenRemote(addr string) (*RemoteServer, error) {
	token, err := loadOrCreateRemoteToken()
	if err != nil {
		return nil, err
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	srv := &http.Server{Handler: a.RemoteHandler(token)}
	go srv.Serve(ln)
	return &RemoteServer{ln: ln, srv: srv, token: token}, nil
}

// PushState sends one presence state to a remote onIT at target
// (e.g. "http://hammer-mini:8125"). Plain HTTP: the payload is a one-word
// presence state on a trusted network; the token only gates writes.
func PushState(target, token, state string) error {
	req, err := http.NewRequest(http.MethodPost,
		strings.TrimSuffix(target, "/")+"/presence", strings.NewReader(state))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "text/plain")
	req.Header.Set(tokenHeader, token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		return fmt.Errorf("push %s: %s: %s", state, resp.Status, strings.TrimSpace(string(b)))
	}
	return nil
}

// ForwardPresence blocks forever: polls Graph and pushes each state to
// target. Used by `onitctl -forward` on the machine that can sign in.
func (g *Graph) ForwardPresence(target, token string) error {
	if !g.SignedIn() {
		return errors.New("not signed in - run onitctl -login first")
	}
	last := ""
	for {
		state, err := g.Presence()
		if err != nil {
			return err
		}
		if err := PushState(target, token, state); err != nil {
			return err
		}
		if state != last {
			fmt.Printf("presence -> %s\n", state)
			last = state
		}
		time.Sleep(graphPoll)
	}
}
