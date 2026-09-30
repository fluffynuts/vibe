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

// Version is this build's release version (e.g. "1.4.0"): the one place it
// is set is the VERSION file, which is bumped by hand for a release.
var Version = strings.TrimSpace(versionFile)

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

// String is how vibe describes itself: "vibe 1.4.0 (a1b2c3d4e5f6)".
func String() string {
	if Commit == "" {
		return "vibe " + Version
	}
	return "vibe " + Version + " (" + Commit + ")"
}
