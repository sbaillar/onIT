package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// runCLIUpdate is `onit -update`: the update flow without the GUI, for when
// the app won't start (a startup crash) and the tray updater is unreachable.
// Run from a terminal:
//
//	/Applications/onIT.app/Contents/MacOS/onit -update
//
// Returns the process exit code.
func runCLIUpdate(beta bool) int {
	cur := appVersion
	if cur == "dev" {
		if v := bundleVersion(); v != "" {
			cur = v
		}
	}

	tag, err := latestTag(beta)
	if err != nil {
		fmt.Fprintln(os.Stderr, "update:", err)
		return 1
	}
	ver := strings.TrimPrefix(tag, "v")

	if cur != "dev" && !newerVersion(ver, cur) {
		fmt.Printf("up to date: %s is the latest (you have %s)\n", ver, cur)
		return 0
	}
	// cur == "dev" means the version couldn't be determined; the likely
	// reason to be here is a broken install, so install latest anyway.
	fmt.Printf("updating %s -> %s\n", cur, ver)

	path, err := download(tag, assetFor(ver))
	if err != nil {
		fmt.Fprintln(os.Stderr, "update:", err)
		return 1
	}
	fmt.Println("downloaded:", path)

	if runtime.GOOS != "darwin" {
		// Windows installs by unzipping over the old files; hand the zip
		// to Explorer like the GUI flow does.
		if err := openInstaller(path); err != nil {
			fmt.Fprintln(os.Stderr, "update:", err)
			return 1
		}
		fmt.Println("extract the zip over your existing onIT files")
		return 0
	}

	args := []string{"installer", "-pkg", path, "-target", "/"}
	if os.Geteuid() != 0 {
		fmt.Println("running: sudo " + strings.Join(args, " "))
		args = append([]string{"sudo"}, args...)
	}
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "update: installer failed:", err)
		return 1
	}
	fmt.Printf("onIT %s installed - launch it with: open -n /Applications/onIT.app\n", ver)
	return 0
}

// bundleVersion reads CFBundleShortVersionString from the .app bundle this
// binary sits in. fyne-packaged builds don't get the -X ldflag, so outside
// the GUI (which reads fyne metadata) the plist is where the version lives.
func bundleVersion() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	plist := bundlePlistFor(exe)
	if plist == "" {
		return ""
	}
	out, err := exec.Command("/usr/libexec/PlistBuddy", "-c",
		"Print :CFBundleShortVersionString", plist).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// bundlePlistFor maps a binary path to its bundle's Info.plist, or "" when
// the binary isn't inside a .app bundle (plain `go build` output).
func bundlePlistFor(exe string) string {
	dir := filepath.Dir(exe) // .../onIT.app/Contents/MacOS
	if filepath.Base(dir) != "MacOS" {
		return ""
	}
	contents := filepath.Dir(dir)
	if filepath.Base(contents) != "Contents" {
		return ""
	}
	return filepath.Join(contents, "Info.plist")
}
