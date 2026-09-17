// Package homeinit seeds the user's ~/.vibe overlay the first time vibe
// runs: the overlay is where the master copy of the configuration is meant
// to live, so vibe puts the bundle's base config there to be edited, and
// offers the bundled profiles for the user to take a copy of.
package homeinit

import (
	"fmt"
	"os"
	"path/filepath"

	"vibe/internal/fscopy"
	"vibe/internal/layout"
	"vibe/internal/profilegen"
)

// BaseFiles are the bundle-root files seeded into a fresh overlay,
// alongside the whole defaults/ tree.
var BaseFiles = []string{"config.yaml", "settings.yaml"}

// DefaultsDir is the directory of shared tooling seeded into a fresh overlay
// in full, since it overrides the bundle's whole.
const DefaultsDir = "defaults"

// Needed reports whether the overlay still has to be seeded. It is true
// until the overlay holds some configuration of its own — vibe's state
// directories (instances/, memories/) don't count, so an overlay
// created by an older version is seeded on the next run.
func Needed(l layout.Layout) bool {
	if l.Home == "" {
		return false
	}
	for _, rel := range append([]string{"profiles", DefaultsDir}, BaseFiles...) {
		if _, err := os.Stat(filepath.Join(l.Home, rel)); err == nil {
			return false
		}
	}
	return true
}

// Result records what seeding did.
type Result struct {
	Files    []string // base files (and defaults/) copied into the overlay
	Profiles []string // profiles copied into the overlay
}

// Run seeds the overlay: it creates the directory, copies the base files and
// the defaults tree the bundle ships, then offers every bundled profile to
// ask, copying the ones it accepts. A nil ask copies no profiles.
func Run(l layout.Layout, ask func(profile string) bool) (Result, error) {
	var res Result
	if l.Home == "" {
		return res, nil
	}
	if err := os.MkdirAll(filepath.Join(l.Home, "profiles"), 0o755); err != nil {
		return res, fmt.Errorf("creating %s: %w", l.Home, err)
	}
	for _, rel := range BaseFiles {
		src := l.BundlePath(rel)
		if _, err := os.Stat(src); err != nil {
			continue // the bundle doesn't ship it — nothing to seed
		}
		if err := fscopy.File(src, filepath.Join(l.Home, rel)); err != nil {
			return res, fmt.Errorf("copying %s into %s: %w", rel, l.Home, err)
		}
		res.Files = append(res.Files, rel)
	}
	if src := l.BundlePath(DefaultsDir); isDir(src) {
		if err := fscopy.Tree(src, filepath.Join(l.Home, DefaultsDir)); err != nil {
			return res, fmt.Errorf("copying %s/ into %s: %w", DefaultsDir, l.Home, err)
		}
		res.Files = append(res.Files, DefaultsDir+"/")
	}
	if ask == nil {
		return res, nil
	}
	for _, profile := range l.BundleProfiles() {
		if !ask(profile) {
			continue
		}
		if _, err := profilegen.ImportToHome(l, profile); err != nil {
			return res, fmt.Errorf("copying profile '%s' into %s: %w", profile, l.Home, err)
		}
		res.Profiles = append(res.Profiles, profile)
	}
	return res, nil
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
