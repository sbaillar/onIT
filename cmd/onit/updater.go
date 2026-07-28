package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/dialog"
)

// appVersion is stamped by the Makefile (-X main.appVersion=$(VERSION)).
var appVersion = "dev"

const (
	releaseAPI = "https://api.github.com/repos/sbaillar/onIT/releases/latest"
	// the beta channel needs the list: /latest deliberately skips prereleases
	releaseListAPI = "https://api.github.com/repos/sbaillar/onIT/releases?per_page=20"
	// betaKey is the preference: opt in to prerelease builds
	betaKey = "betaUpdates"
	// verboseKey is the preference: log every protocol line
	verboseKey = "verboseLogging"
)

type release struct {
	TagName    string `json:"tag_name"`
	Draft      bool   `json:"draft"`
	Prerelease bool   `json:"prerelease"`
}

func getJSON(url string, v any) error {
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("release check: %s", resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(v)
}

// latestTag returns the newest release tag for the channel. The stable channel
// asks GitHub for the release it marks latest; the beta channel scans the list
// and takes the highest version, prereleases included. Drafts are never
// offered. GitHub orders the list by creation date, which is not version
// order, so the beta pick compares versions rather than trusting position.
func latestTag(beta bool) (string, error) {
	if !beta {
		var r release
		if err := getJSON(releaseAPI, &r); err != nil {
			return "", err
		}
		return r.TagName, nil
	}
	var list []release
	if err := getJSON(releaseListAPI, &list); err != nil {
		return "", err
	}
	best := ""
	for _, r := range list {
		if r.Draft {
			continue
		}
		if best == "" || newerVersion(strings.TrimPrefix(r.TagName, "v"), strings.TrimPrefix(best, "v")) {
			best = r.TagName
		}
	}
	if best == "" {
		return "", fmt.Errorf("release check: no releases found")
	}
	return best, nil
}

func assetFor(ver string) string {
	if runtime.GOOS == "windows" {
		return fmt.Sprintf("onIT-%s-windows-amd64.zip", ver)
	}
	return fmt.Sprintf("onIT-%s-macos-arm64.pkg", ver)
}

func download(tag, name string) (string, error) {
	url := fmt.Sprintf("https://github.com/sbaillar/onIT/releases/download/%s/%s", tag, name)
	resp, err := http.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download %s: %s", name, resp.Status)
	}
	path := filepath.Join(os.TempDir(), name)
	f, err := os.Create(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	if _, err := io.Copy(f, resp.Body); err != nil {
		return "", err
	}
	return path, nil
}

// openInstaller hands the downloaded artifact to the OS: macOS opens the
// pkg in Installer; Windows reveals the zip for the user to extract over
// the old files.
func openInstaller(path string) error {
	if runtime.GOOS == "windows" {
		return exec.Command("explorer", "/select,", path).Start()
	}
	return exec.Command("open", path).Start()
}

// checkForUpdates runs the whole flow; call from the UI goroutine.
// beta selects the channel: prerelease builds included, or stable only.
func checkForUpdates(w fyne.Window, beta bool) {
	go func() {
		tag, err := latestTag(beta)
		fyne.Do(func() {
			if err != nil {
				dialog.ShowError(err, w)
				return
			}
			ver := strings.TrimPrefix(tag, "v")
			// only strictly newer counts: a beta tester sitting on 1.18.0-dev1
			// must not be offered 1.17.2 as an "update" after leaving the channel
			if !newerVersion(ver, appVersion) {
				channel := "latest version"
				if beta {
					channel = "latest beta"
				}
				dialog.ShowInformation("Up to date",
					"You're on the "+channel+" ("+appVersion+").", w)
				return
			}
			msg := fmt.Sprintf("Version %s is available (you have %s).\nDownload and install?", ver, appVersion)
			dialog.ShowConfirm("Update available", msg, func(ok bool) {
				if !ok {
					return
				}
				go func() {
					path, err := download(tag, assetFor(ver))
					fyne.Do(func() {
						if err != nil {
							dialog.ShowError(err, w)
							return
						}
						log.Printf("update downloaded: %s", path)
						if err := openInstaller(path); err != nil {
							dialog.ShowError(err, w)
							return
						}
						relaunchWhenInstalled(ver)
						msg := "Finish the install, then launch onIT -\nthe new version stops this one automatically."
						if runtime.GOOS == "darwin" {
							msg = "Finish the install - onIT switches to\nthe new version by itself when it's done."
						}
						dialog.ShowInformation("Installer opened", msg, w)
					})
				}()
			}, w)
		})
	}()
}
