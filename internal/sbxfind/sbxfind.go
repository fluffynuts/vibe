// Package sbxfind says which sbx to run: the one on PATH, else one in a
// place sbx is known to be installed — as it is straight after vibe
// --install-sbx, before the user has put its folder on PATH.
package sbxfind

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
)

// Warn is told what the user should know about the sbx in use, once per
// process. The default prints it; vibe points it at its own notes.
var Warn = func(msg string) { fmt.Fprintf(os.Stderr, "vibe: %s\n", msg) }

// Found is where sbx was found.
type Found struct {
	// Path is the sbx to run; "" when there's none.
	Path string
	// OnPath says Path is what PATH runs, not a fallback.
	OnPath bool
	// Others are known locations holding a different sbx from Path.
	Others []string
}

// KnownLocations lists where sbx is installed on goos, the one to prefer
// first. Homebrew's two prefixes are listed rather than asked for, as
// asking means running brew.
func KnownLocations(goos, home string, env func(string) string) []string {
	switch goos {
	case "windows":
		return []string{
			filepath.Join(env("LOCALAPPDATA"), "DockerSandboxes", "bin", "sbx.exe"),
			filepath.Join(env("ProgramFiles"), "DockerSandboxes", "bin", "sbx.exe"),
		}
	case "darwin":
		return []string{
			filepath.Join(home, ".docker", "sbx", "bin", "sbx"),
			"/Applications/Sbx.app/Contents/MacOS/sbx",
			"/opt/homebrew/bin/sbx",
			"/usr/local/bin/sbx",
		}
	}
	return []string{filepath.Join(home, ".docker", "sbx", "bin", "sbx")}
}

// Find looks for sbx with lookPath, then in known. Files that are the same
// one, as a link and what it points to are, count once.
func Find(lookPath func(string) (string, error), known []string) Found {
	var found Found
	if p, err := lookPath("sbx"); err == nil {
		found = Found{Path: p, OnPath: true}
	}
	for _, k := range known {
		if !isFile(k) {
			continue
		}
		switch {
		case found.Path == "":
			found.Path = k
		case !sameFile(found.Path, k) && !containsSame(found.Others, k):
			found.Others = append(found.Others, k)
		}
	}
	return found
}

func isFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func sameFile(a, b string) bool {
	ai, err := os.Stat(a)
	if err != nil {
		return false
	}
	bi, err := os.Stat(b)
	return err == nil && os.SameFile(ai, bi)
}

func containsSame(paths []string, path string) bool {
	for _, p := range paths {
		if sameFile(p, path) {
			return true
		}
	}
	return false
}

// Describe says what the user should know about f, or "" when there's
// nothing: sbx isn't on PATH, or another sbx is installed beside the one
// that is.
func (f Found) Describe() string {
	switch {
	case f.Path == "":
		return ""
	case !f.OnPath:
		dir := filepath.Dir(f.Path)
		return fmt.Sprintf("using sbx at %s, which isn't on your PATH — add %s to it, e.g. export PATH=\"%s:$PATH\"", f.Path, dir, dir)
	case len(f.Others) > 0:
		return fmt.Sprintf("sbx is being used from %s, which is on your PATH, but sbx also exists at %s, a default installation location — check that the right sbx is on your PATH", f.Path, joinOr(f.Others))
	}
	return ""
}

func joinOr(paths []string) string {
	out := paths[0]
	for _, p := range paths[1:] {
		out += " and " + p
	}
	return out
}

var warnOnce sync.Once

// Look finds sbx on this machine, and warns about it the first time.
func Look() Found {
	home, _ := os.UserHomeDir()
	f := Find(exec.LookPath, KnownLocations(runtime.GOOS, home, os.Getenv))
	if msg := f.Describe(); msg != "" {
		warnOnce.Do(func() { Warn(msg) })
	}
	return f
}

// Path is the sbx to run: where Look finds it, or plain "sbx" when it's
// nowhere, so what the user sees is the error for a missing sbx.
func Path() string {
	if p := Look().Path; p != "" {
		return p
	}
	return "sbx"
}

// Available says whether there's an sbx to run. It doesn't warn: asking
// isn't using.
func Available() bool {
	home, _ := os.UserHomeDir()
	return Find(exec.LookPath, KnownLocations(runtime.GOOS, home, os.Getenv)).Path != ""
}
