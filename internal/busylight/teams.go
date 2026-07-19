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
)

var identity = map[string]string{
	"protocol-version": "2.0.0",
	"manufacturer":     "Sonny",
	"device":           "BusyLight-Round",
	"app":              "teams-busylight",
	"app-version":      "2.0",
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
// and forwarding device commands (taps) up to Teams.
func (a *Agent) session() error {
	params := url.Values{}
	for k, v := range identity {
		params.Set(k, v)
	}
	if tok := loadToken(); tok != "" {
		params.Set("token", tok)
	}

	ws, _, err := websocket.DefaultDialer.Dial(wsURL+"?"+params.Encode(), nil)
	if err != nil {
		return err
	}
	defer ws.Close()
	log.Print("Connected to Teams local API")

	// drop taps that queued up while Teams was unreachable
	for {
		select {
		case <-a.light.Cmds:
			continue
		default:
		}
		break
	}
	a.setTeams(true, "available")

	msgs := make(chan []byte)
	errc := make(chan error, 1)
	go func() {
		for {
			_, data, err := ws.ReadMessage()
			if err != nil {
				errc <- err
				close(msgs)
				return
			}
			msgs <- data
		}
	}()

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
			if msg.TokenRefresh != "" {
				saveToken(msg.TokenRefresh)
			}
			if msg.MeetingUpdate != nil && msg.MeetingUpdate.MeetingState != nil {
				a.setTeams(true, mapState(msg.MeetingUpdate.MeetingState))
			}
		case cmd := <-a.light.Cmds:
			a.reqID++
			err := ws.WriteJSON(map[string]any{
				"requestId":  a.reqID,
				"apiVersion": "2.0.0",
				"action":     cmd, // e.g. toggle-mute
			})
			if err != nil {
				return err
			}
			log.Printf("Sent action: %s (#%d)", cmd, a.reqID)
		}
	}
}
