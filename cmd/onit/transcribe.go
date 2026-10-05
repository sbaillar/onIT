package main

import (
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
)

// whisperConfig is the Whisper server meeting transcription uses. It lives in
// meetnote's config.json ("whisper" object) because meetnote is what sends
// the audio; onIT is only where it gets edited.
type whisperConfig struct {
	URL    string `json:"url"`
	Model  string `json:"model"`
	APIKey string `json:"api_key"`
}

// defaultWhisperModel mirrors meetnote's DEFAULT_MODEL, used when Model is blank.
const defaultWhisperModel = "deepdml/faster-whisper-large-v3-turbo-ct2"

func meetnoteConfigFile() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".meetnote", "config.json")
}

// loadWhisperConfig returns the saved settings, or zero values if none.
func loadWhisperConfig(path string) whisperConfig {
	var all struct {
		Whisper whisperConfig `json:"whisper"`
	}
	if b, err := os.ReadFile(path); err == nil {
		json.Unmarshal(b, &all)
	}
	return all.Whisper
}

// saveWhisperConfig replaces the "whisper" object and keeps any other keys.
// The file is 0600: it holds the API key.
func saveWhisperConfig(path string, c whisperConfig) error {
	all := map[string]json.RawMessage{}
	if b, err := os.ReadFile(path); err == nil {
		json.Unmarshal(b, &all)
	}
	w, err := json.Marshal(c)
	if err != nil {
		return err
	}
	all["whisper"] = w
	b, err := json.MarshalIndent(all, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(path, b, 0o600); err != nil {
		return err
	}
	return os.Chmod(path, 0o600) // WriteFile keeps an existing file's mode
}

// showTranscriptionSetup edits the Whisper server used to transcribe
// meetings, in its own window like Presence setup (Settings is too narrow
// for a dialog: Fyne clips dialogs to their parent window).
func showTranscriptionSetup(a fyne.App) {
	w := a.NewWindow("Transcription server")
	tf := newTranscriptionForm(meetnoteConfigFile(), func(err error) {
		if err != nil {
			dialog.ShowError(err, w)
			return
		}
		w.Close()
	})
	tf.form.OnCancel = w.Close
	hint := widget.NewLabel("Any OpenAI-compatible Whisper endpoint. Leave the model " +
		"blank for the default; the API key is sent as a Bearer token.")
	hint.Wrapping = fyne.TextWrapWord
	hint.Importance = widget.LowImportance
	w.SetContent(container.NewPadded(container.NewVBox(tf.form, hint)))
	w.Resize(fyne.NewSize(480, 0))
	w.Show()
}

type transcriptionForm struct {
	form            *widget.Form
	url, model, key *widget.Entry
}

// newTranscriptionForm is the URL / model / API key form, loaded from path.
// Save writes path and reports the result to done.
func newTranscriptionForm(path string, done func(error)) *transcriptionForm {
	c := loadWhisperConfig(path)
	tf := &transcriptionForm{
		url:   widget.NewEntry(),
		model: widget.NewEntry(),
		key:   widget.NewPasswordEntry(),
	}
	tf.url.SetPlaceHolder("http://host:9000/v1/audio/transcriptions")
	tf.url.SetText(c.URL)
	tf.url.Validator = validWhisperURL
	tf.model.SetPlaceHolder(defaultWhisperModel)
	tf.model.SetText(c.Model)
	tf.key.SetPlaceHolder("optional")
	tf.key.SetText(c.APIKey)
	tf.form = &widget.Form{
		Items: []*widget.FormItem{
			widget.NewFormItem("URL", tf.url),
			widget.NewFormItem("Model", tf.model),
			widget.NewFormItem("API key", tf.key),
		},
		SubmitText: "Save",
		OnSubmit: func() {
			done(saveWhisperConfig(path, whisperConfig{
				URL:    strings.TrimSpace(tf.url.Text),
				Model:  strings.TrimSpace(tf.model.Text),
				APIKey: strings.TrimSpace(tf.key.Text),
			}))
		},
	}
	return tf
}

// validWhisperURL accepts blank (no transcription, speakers only) or an
// http(s) URL with a host.
func validWhisperURL(s string) error {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	if u, err := url.Parse(s); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return errors.New("needs an http:// or https:// URL")
	}
	return nil
}
