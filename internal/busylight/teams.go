package busylight

import (
	"encoding/json"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

const (
	wsURL     = "ws://127.0.0.1:8124"
	retryWait = 5 * time.Second

	maxWSMessage = 1 << 20
	pingEvery    = 30 * time.Second
	readTimeout  = 75 * time.Second // > pingEvery; catches half-open sockets

	// The only device command forwarded to Teams; anything else from the
	// serial port (buggy or foreign firmware) is dropped.
	allowedCmd = "toggle-mute"
)

func identityParams() url.Values {
	return url.Values{
		"protocol-version": {"2.0.0"},
		"manufacturer":     {"Sonny"},
		"device":           {"BusyLight-Round"},
		"app":              {"teams-busylight"},
		"app-version":      {"2.0"},
	}
}

func tokenFile() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".teams_busylight_token")
}

func loadToken() string {
	b, err := os.ReadFile(tokenFile())
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func saveToken(tok string) {
	if err := os.WriteFile(tokenFile(), []byte(tok), 0o600); err != nil {
		log.Printf("token save failed: %v", err)
		return
	}
	log.Print("Token refreshed")
}

type meetingState struct {
	IsMuted       bool `json:"isMuted"`
	IsInMeeting   bool `json:"isInMeeting"`
	IsSharing     bool `json:"isSharing"`
	IsRecordingOn bool `json:"isRecordingOn"`
}

type teamsMsg struct {
	TokenRefresh  string `json:"tokenRefresh"`
	MeetingUpdate *struct {
		MeetingState *meetingState `json:"meetingState"`
	} `json:"meetingUpdate"`
}

func mapState(ms *meetingState) string {
	switch {
	case !ms.IsInMeeting:
		return "available"
	case ms.IsSharing || ms.IsRecordingOn:
		return "sharing"
	case ms.IsMuted:
		return "muted"
	default:
		return "meeting"
	}
}

// session runs one WebSocket connection until it fails, feeding a.setTeams
// and forwarding device taps up to Teams.
func (a *Agent) session() error {
	params := identityParams()
	token := loadToken()
	if token != "" {
		params.Set("token", token)
	}

	ws, _, err := websocket.DefaultDialer.Dial(wsURL+"?"+params.Encode(), nil)
	if err != nil {
		return err
	}
	defer ws.Close()
	log.Print("Connected to Teams local API")

	// drop taps that queued up while Teams was unreachable
drain:
	for {
		select {
		case <-a.light.Cmds:
		default:
			break drain
		}
	}
	a.setTeams(true, mapState(&meetingState{}))

	ws.SetReadLimit(maxWSMessage)
	ws.SetReadDeadline(time.Now().Add(readTimeout))
	ws.SetPongHandler(func(string) error {
		return ws.SetReadDeadline(time.Now().Add(readTimeout))
	})

	msgs := make(chan []byte, 1)
	errc := make(chan error, 1)
	done := make(chan struct{})
	defer close(done)
	go func() {
		for {
			_, data, err := ws.ReadMessage()
			if err != nil {
				errc <- err
				close(msgs)
				return
			}
			ws.SetReadDeadline(time.Now().Add(readTimeout))
			select {
			case msgs <- data:
			case <-done:
				return
			}
		}
	}()

	reqID := 0
	send := func(action string) error {
		reqID++
		return ws.WriteJSON(map[string]any{
			"requestId":  reqID,
			"apiVersion": "2.0.0",
			"action":     action,
		})
	}
	// ask for the real meeting state so a mid-meeting (re)connect is correct
	if err := send("query-meeting-state"); err != nil {
		return err
	}

	ping := time.NewTicker(pingEvery)
	defer ping.Stop()
	for {
		select {
		case data, ok := <-msgs:
			if !ok {
				return <-errc
			}
			var msg teamsMsg
			if err := json.Unmarshal(data, &msg); err != nil {
				continue
			}
			if msg.TokenRefresh != "" && msg.TokenRefresh != token {
				token = msg.TokenRefresh
				saveToken(token)
			}
			if msg.MeetingUpdate != nil && msg.MeetingUpdate.MeetingState != nil {
				a.setTeams(true, mapState(msg.MeetingUpdate.MeetingState))
			}
		case cmd := <-a.light.Cmds:
			if cmd != allowedCmd {
				log.Printf("ignoring device command %q", cmd)
				continue
			}
			if err := send(cmd); err != nil {
				return err
			}
			log.Printf("Sent action: %s (#%d)", cmd, reqID)
		case <-ping.C:
			err := ws.WriteControl(websocket.PingMessage, nil,
				time.Now().Add(5*time.Second))
			if err != nil {
				return err
			}
		}
	}
}
