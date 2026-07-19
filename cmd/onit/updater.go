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

const releaseAPI = "https://api.github.com/repos/sbaillar/onIT/releases/latest"

func latestTag() (string, error) {
	resp, err := http.Get(releaseAPI)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("release check: %s", resp.Status)
	}
	var r struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return "", err
	}
	return r.TagName, nil
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
func checkForUpdates(w fyne.Window) {
	go func() {
		tag, err := latestTag()
		fyne.Do(func() {
			if err != nil {
				dialog.ShowError(err, w)
				return
			}
			ver := strings.TrimPrefix(tag, "v")
			if ver == appVersion {
				dialog.ShowInformation("Up to date",
					"You're on the latest version ("+tag+").", w)
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
						dialog.ShowInformation("Installer opened",
							"Finish the install, then launch onIT -\nthe new version stops this one automatically.", w)
					})
				}()
			}, w)
		})
	}()
}
