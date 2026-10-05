package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"fyne.io/fyne/v2/test"
)

func TestWhisperConfigRoundTrip(t *testing.T) {
	p := filepath.Join(t.TempDir(), "meetnote", "config.json")
	if got := loadWhisperConfig(p); got != (whisperConfig{}) {
		t.Fatalf("missing file: got %+v, want zero", got)
	}
	want := whisperConfig{URL: "http://w:9000/v1/audio/transcriptions", Model: "m", APIKey: "k"}
	if err := saveWhisperConfig(p, want); err != nil {
		t.Fatal(err)
	}
	if got := loadWhisperConfig(p); got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	fi, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v, want 0600 (holds the API key)", fi.Mode().Perm())
	}
}

// meetnote may keep other settings in the same file; saving the Whisper
// block must not drop them.
func TestWhisperConfigKeepsOtherKeys(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.json")
	os.WriteFile(p, []byte(`{"other": {"x": 1}, "whisper": {"url": "old"}}`), 0o600)
	if err := saveWhisperConfig(p, whisperConfig{URL: "new"}); err != nil {
		t.Fatal(err)
	}
	var all struct {
		Other struct{ X int } `json:"other"`
	}
	b, _ := os.ReadFile(p)
	if err := json.Unmarshal(b, &all); err != nil {
		t.Fatal(err)
	}
	if all.Other.X != 1 {
		t.Fatalf("other key lost or changed: %s", b)
	}
	if loadWhisperConfig(p).URL != "new" {
		t.Fatal("whisper block not updated")
	}
}

func TestTranscriptionFormLoadsAndSaves(t *testing.T) {
	test.NewApp()
	p := filepath.Join(t.TempDir(), "config.json")
	saveWhisperConfig(p, whisperConfig{URL: "http://old:9000/x", Model: "m-old"})

	var saved error = errors.New("not called")
	tf := newTranscriptionForm(p, func(err error) { saved = err })
	if tf.url.Text != "http://old:9000/x" || tf.model.Text != "m-old" || tf.key.Text != "" {
		t.Fatalf("not loaded: %q %q %q", tf.url.Text, tf.model.Text, tf.key.Text)
	}
	tf.url.SetText(" http://whisper.lan:9000/v1/audio/transcriptions ")
	tf.model.SetText("")
	tf.key.SetText("sk-test")
	tf.form.OnSubmit()
	if saved != nil {
		t.Fatal(saved)
	}
	want := whisperConfig{URL: "http://whisper.lan:9000/v1/audio/transcriptions", APIKey: "sk-test"}
	if got := loadWhisperConfig(p); got != want {
		t.Fatalf("saved %+v, want %+v", got, want)
	}
}

func TestValidWhisperURL(t *testing.T) {
	for s, ok := range map[string]bool{
		"":                          true,
		"http://w:9000/v1/audio":    true,
		"https://api.example.com/x": true,
		"ftp://w/x":                 false,
		"whisper.lan:9000":          false,
		"http://":                   false,
	} {
		if err := validWhisperURL(s); (err == nil) != ok {
			t.Errorf("%q: err=%v, want ok=%v", s, err, ok)
		}
	}
}
