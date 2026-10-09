// Package browser opens a URL in the user's browser, when there is one to
// open it in.
package browser

import (
	"os"
	"os/exec"
	"runtime"
)

// Open starts the user's browser on url, without waiting for it, and says
// whether it tried. It declines — so the caller can print the URL for the
// user to open — where opening would go wrong: over ssh, or on a Linux
// machine with no display, where xdg-open falls back to a text browser that
// would take over the very terminal vibe is running in.
func Open(url string) bool {
	name, args, ok := command(runtime.GOOS, os.Getenv, exec.LookPath, url)
	if !ok {
		return false
	}
	cmd := exec.Command(name, args...)
	// Nothing the opener prints belongs on vibe's terminal.
	cmd.Stdin, cmd.Stdout, cmd.Stderr = nil, nil, nil
	if err := cmd.Start(); err != nil {
		return false
	}
	go cmd.Wait()
	return true
}

// command works out what to run to open url on goos, given the environment
// and a way to find programs; ok is false where nothing should be run.
func command(goos string, getenv func(string) string, lookPath func(string) (string, error), url string) (name string, args []string, ok bool) {
	if getenv("SSH_CONNECTION") != "" || getenv("SSH_TTY") != "" {
		return "", nil, false
	}
	switch goos {
	case "darwin":
		return "open", []string{url}, true
	case "windows":
		return "rundll32", []string{"url.dll,FileProtocolHandler", url}, true
	default:
		if getenv("DISPLAY") == "" && getenv("WAYLAND_DISPLAY") == "" {
			return "", nil, false
		}
		if _, err := lookPath("xdg-open"); err != nil {
			return "", nil, false
		}
		return "xdg-open", []string{url}, true
	}
}
