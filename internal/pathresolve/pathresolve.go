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
