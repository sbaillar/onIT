package main

import (
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

// logTailLines caps what the viewer holds. The log grows for the life of the
// install, so it is read from the end rather than shown whole.
const logTailLines = 500

// tailLog returns the last logTailLines lines of the app log.
func tailLog() string {
	b, err := os.ReadFile(logPath())
	if err != nil {
		return "log unavailable: " + err.Error()
	}
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	if len(lines) > logTailLines {
		lines = lines[len(lines)-logTailLines:]
	}
	return strings.Join(lines, "\n")
}

// the viewer is built once and hidden on close, so the tail goroutine can be
// stopped while it isn't being looked at
var (
	logWin  fyne.Window
	logBody *widget.Label
	logView *container.Scroll
	logStop chan struct{}
)

// revealInFileManager shows the log in Finder / Explorer, for handing the
// whole file to someone rather than the tail on screen.
func revealInFileManager(path string) error {
	if runtime.GOOS == "windows" {
		return exec.Command("explorer", "/select,", path).Start()
	}
	return exec.Command("open", "-R", path).Start()
}

// showLog opens the log viewer and follows the file while it's open.
func showLog(a fyne.App) {
	if logWin == nil {
		logBody = widget.NewLabel("")
		logBody.TextStyle = fyne.TextStyle{Monospace: true}
		logView = container.NewScroll(logBody)

		copyBtn := widget.NewButton("Copy", func() { a.Clipboard().SetContent(logBody.Text) })
		revealBtn := widget.NewButton("Reveal in Finder", func() {
			if err := revealInFileManager(logPath()); err != nil {
				logBody.SetText("could not reveal the log: " + err.Error())
			}
		})
		path := widget.NewLabel(logPath())
		path.Importance = widget.LowImportance
		bar := container.NewBorder(nil, nil, nil, container.NewHBox(copyBtn, revealBtn), path)

		logWin = a.NewWindow("onIT log")
		logWin.SetContent(container.NewBorder(nil, bar, nil, nil, logView))
		logWin.Resize(fyne.NewSize(760, 460))
		logWin.SetCloseIntercept(func() {
			stopLogTail()
			logWin.Hide()
		})
	}
	refreshLog()
	startLogTail()
	logWin.Show()
	logWin.RequestFocus()
}

// refreshLog reloads the tail and pins the view to the newest line. Called on
// the UI thread.
func refreshLog() {
	s := tailLog()
	if s == logBody.Text {
		return // nothing new; leave the scroll where the reader put it
	}
	logBody.SetText(s)
	logView.ScrollToBottom()
}

func startLogTail() {
	if logStop != nil {
		return // already following
	}
	stop := make(chan struct{})
	logStop = stop
	go func() {
		for {
			select {
			case <-stop:
				return
			case <-time.After(time.Second):
			}
			fyne.Do(refreshLog)
		}
	}()
}

func stopLogTail() {
	if logStop != nil {
		close(logStop)
		logStop = nil
	}
}
