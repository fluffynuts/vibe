// Package layout resolves vibe's data files across two layers: the user's
// ~/.vibe overlay, which wins, and the bundle unpacked next to the binary,
// which it falls back to.
//
// Single files override one at a time — a settings.yaml in the overlay
// replaces the bundle's. The two directories of many files, defaults/ and
// profiles/<name>/, override whole instead: when the overlay has one, the
// bundle's is ignored entirely, so each is always read from exactly one
// place and deleting a file from a copy actually deletes it.
package layout

import (
	"os"
	"path/filepath"
	"sort"
)

// Layout is the pair of layers vibe reads its data from.
type Layout struct {
	Home   string // the user's ~/.vibe overlay ("" when there is none)
	Bundle string // the directory the vibe binary was unpacked into
}

// New builds a Layout from the user's vibe home and the bundle root.
func New(home, bundle string) Layout {
	return Layout{Home: home, Bundle: bundle}
}

// HomePath joins rel onto the overlay, and is empty when there is no overlay.
func (l Layout) HomePath(rel ...string) string {
	if l.Home == "" {
		return ""
	}
	return filepath.Join(append([]string{l.Home}, rel...)...)
}

// BundlePath joins rel onto the bundle.
func (l Layout) BundlePath(rel ...string) string {
	return filepath.Join(append([]string{l.Bundle}, rel...)...)
}

// File returns the path to read a bundle-relative file from: the overlay's
// copy when it exists, else the bundle's (which may itself be missing —
// callers treat that as "not configured").
func (l Layout) File(rel ...string) string {
	if p := l.HomePath(rel...); p != "" && exists(p) {
		return p
	}
	return l.BundlePath(rel...)
}

// DefaultsDir returns the single directory the shared defaults are read
// from: the overlay's when it has one, else the bundle's. Like a profile it
// overrides whole, so a script the user deletes from their copy is gone
// rather than falling back to the bundle's.
func (l Layout) DefaultsDir() string {
	if p := l.HomePath("defaults"); p != "" && isDir(p) {
		return p
	}
	return l.BundlePath("defaults")
}

// ProfileDir returns the single directory a profile is read from: the
// overlay's when that directory exists at all, else the bundle's. The path
// is returned even when nothing is there, so it can be reported or created.
func (l Layout) ProfileDir(profile string) string {
	if p := l.HomePath("profiles", profile); p != "" && isDir(p) {
		return p
	}
	return l.BundlePath("profiles", profile)
}

// NewProfileDir returns where a profile vibe creates should be written: the
// overlay when there is one, so the bundle stays as shipped.
func (l Layout) NewProfileDir(profile string) string {
	if p := l.HomePath("profiles", profile); p != "" {
		return p
	}
	return l.BundlePath("profiles", profile)
}

// ProfileExists reports whether a usable profile — a directory with a
// config.yaml — is present in either layer.
func (l Layout) ProfileExists(profile string) bool {
	return exists(filepath.Join(l.ProfileDir(profile), "config.yaml"))
}

// Profiles returns the names of every usable profile across both layers,
// sorted and de-duplicated.
func (l Layout) Profiles() []string {
	seen := map[string]bool{}
	for _, dir := range []string{l.BundlePath("profiles"), l.HomePath("profiles")} {
		if dir == "" {
			continue
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() && l.ProfileExists(e.Name()) {
				seen[e.Name()] = true
			}
		}
	}
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// BundleProfiles returns the names of the usable profiles the bundle ships,
// whether or not the overlay also has one of that name.
func (l Layout) BundleProfiles() []string {
	entries, err := os.ReadDir(l.BundlePath("profiles"))
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() && exists(l.BundlePath("profiles", e.Name(), "config.yaml")) {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
