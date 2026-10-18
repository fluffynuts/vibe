// Package pathresolve locates the vibe bundle (relative to the running
// executable) and derives sandbox/profile names from filesystem paths.
package pathresolve

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// BundleRoot returns the directory containing the vibe binary — the root of
// the unpacked bundle (defaults/, profiles/, settings.yaml, config.yaml).
func BundleRoot() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("locating vibe executable: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(exe)
	if err != nil {
		return "", fmt.Errorf("resolving vibe executable path: %w", err)
	}
	return filepath.Dir(resolved), nil
}

var nonAlnum = regexp.MustCompile(`[^a-z0-9]+`)
var leadingNonAlnum = regexp.MustCompile(`^[^a-z0-9]+`)
var trailingDash = regexp.MustCompile(`-+$`)

// DeriveName derives a valid sandbox/profile name from a filesystem path's
// leaf component: lowercase alphanumerics and dashes, starting alphanumeric,
// at least 2 characters — mirroring vibe.sh's derive_name.
func DeriveName(path string) string {
	raw := strings.ToLower(filepath.Base(path))
	raw = nonAlnum.ReplaceAllString(raw, "-")
	raw = leadingNonAlnum.ReplaceAllString(raw, "")
	raw = trailingDash.ReplaceAllString(raw, "")
	if raw == "" {
		raw = "sandbox"
	}
	if len(raw) < 2 {
		raw += "0"
	}
	return raw
}

// Within reports whether path is dir or somewhere under it, as the two are
// written: links aren't followed. A name that only starts with ".." (a
// folder "..cache") is under it.
func Within(dir, path string) bool {
	rel, err := filepath.Rel(dir, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// Tilde is path with ~ in place of the user's home folder, when it's in
// it: the way the user thinks of it.
func Tilde(path string) string {
	home, _ := os.UserHomeDir()
	return tilde(path, home)
}

func tilde(path, home string) string {
	if home == "" || !Within(home, path) {
		return path
	}
	rel, _ := filepath.Rel(home, path)
	return filepath.Join("~", rel)
}

// ExpandHome expands a leading "~", "~/" or "~\" in path to the user's
// home folder: a settings.yaml value, or a folder given on the command line
// (Windows PowerShell hands "~\project" over as it is).
func ExpandHome(path string) string {
	if path == "" || path[0] != '~' {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	if path == "~" {
		return home
	}
	if strings.HasPrefix(path, "~/") || strings.HasPrefix(path, `~\`) {
		return filepath.Join(home, path[2:])
	}
	return path
}
