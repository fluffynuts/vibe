// Package vibe holds what belongs to the repo as a whole rather than to any
// one package: the release version, read from the VERSION file beside it.
// go:embed can't reach above a package's own directory, which is why this
// lives at the root rather than next to main.
package vibe

import (
	_ "embed"
	"runtime/debug"
	"strings"
)

//go:embed VERSION
var versionFile string

// Version is the major.minor part of this build's version (e.g. "1.4"): the
// one place it is set is the VERSION file, bumped by hand. The third part is
// the CI build number (see Build), so every release is a valid semantic
// version that sorts after the last — which GitHub's releases page, among
// others, relies on.
var Version = strings.TrimSpace(versionFile)

// Build is the CI build number a release was made from, appended to Version
// as its third part ("1.4.57") — set with -ldflags "-X vibe.Build=57", and
// empty for any other build. VERSION is bumped by hand and every push to
// master is released, so this is what tells those releases apart.
var Build string

// BuildDate is when this binary was built, in UTC ("2026-09-30T11:22:05Z").
// Go doesn't record it — builds are reproducible on purpose — so the build
// scripts pass it in with -ldflags "-X vibe.BuildDate=..."; a plain go
// build leaves it empty, and --version then leaves it out.
var BuildDate string

// FullVersion is Version with the build number, when there is one.
func FullVersion() string {
	if Build == "" {
		return Version
	}
	return Version + "." + Build
}

// Commit is the git commit this binary was built from, short form, with a
// "-dirty" suffix when the working tree had uncommitted changes — or empty
// when that isn't known (a build outside a git checkout, or with
// -buildvcs=false). go build records it by itself, so nothing needs passing
// in; a release build can still override it with
// -ldflags "-X vibe.Commit=..." — which only works on a variable with no
// initialiser, hence init rather than "= vcsCommit()".
var Commit string

func init() {
	if Commit == "" {
		Commit = vcsCommit()
	}
}

func vcsCommit() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	var revision string
	var modified bool
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			revision = s.Value
		case "vcs.modified":
			modified = s.Value == "true"
		}
	}
	if len(revision) > 12 {
		revision = revision[:12]
	}
	if revision != "" && modified {
		revision += "-dirty"
	}
	return revision
}

// String is how vibe describes itself:
// "vibe 1.4.57 (a1b2c3d4e5f6, built 2026-09-30T11:22:05Z)", leaving out
// whatever this build doesn't know.
func String() string {
	var details []string
	if Commit != "" {
		details = append(details, Commit)
	}
	if BuildDate != "" {
		details = append(details, "built "+BuildDate)
	}
	if len(details) == 0 {
		return "vibe " + FullVersion()
	}
	return "vibe " + FullVersion() + " (" + strings.Join(details, ", ") + ")"
}
