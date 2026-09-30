package vibe

import (
	"regexp"
	"testing"
)

// The VERSION file is read by CI as well as embedded here, so it has to stay
// a bare major.minor: no "v" prefix, no trailing text, and no third part —
// that's the build number, and a release with four parts isn't a semantic
// version, which GitHub then sorts as text (v0.1.0.6 above v0.1.0.10).
func TestVersionIsMajorMinor(t *testing.T) {
	if !regexp.MustCompile(`^\d+\.\d+$`).MatchString(Version) {
		t.Errorf("VERSION = %q, want major.minor, like 1.4", Version)
	}
}

func TestStringNamesTheVersionCommitAndBuildDate(t *testing.T) {
	oldCommit, oldDate := Commit, BuildDate
	t.Cleanup(func() { Commit, BuildDate = oldCommit, oldDate })

	for _, tt := range []struct{ commit, date, want string }{
		{"", "", "vibe " + Version},
		{"a1b2c3d4e5f6", "", "vibe " + Version + " (a1b2c3d4e5f6)"},
		{"", "2026-09-30T11:22:05Z", "vibe " + Version + " (built 2026-09-30T11:22:05Z)"},
		{"a1b2c3d4e5f6", "2026-09-30T11:22:05Z", "vibe " + Version + " (a1b2c3d4e5f6, built 2026-09-30T11:22:05Z)"},
	} {
		Commit, BuildDate = tt.commit, tt.date
		if got := String(); got != tt.want {
			t.Errorf("String() with commit %q, date %q = %q, want %q", tt.commit, tt.date, got, tt.want)
		}
	}
}

func TestFullVersionAddsTheBuildNumber(t *testing.T) {
	old := Build
	t.Cleanup(func() { Build = old })

	Build = ""
	if got := FullVersion(); got != Version {
		t.Errorf("FullVersion() without a build = %q, want %q", got, Version)
	}
	Build = "57"
	if got := FullVersion(); got != Version+".57" {
		t.Errorf("FullVersion() with build 57 = %q", got)
	}
}
