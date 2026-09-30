package vibe

import (
	"regexp"
	"strings"
	"testing"
)

// The VERSION file is read by CI as well as embedded here, so it has to stay
// a bare semantic version: no "v" prefix, no trailing text.
func TestVersionIsBareSemver(t *testing.T) {
	if !regexp.MustCompile(`^\d+\.\d+\.\d+(-[0-9A-Za-z.-]+)?$`).MatchString(Version) {
		t.Errorf("VERSION = %q, want a bare semantic version like 1.4.0", Version)
	}
}

func TestStringNamesTheVersionAndCommit(t *testing.T) {
	old := Commit
	t.Cleanup(func() { Commit = old })

	Commit = ""
	if got := String(); got != "vibe "+Version {
		t.Errorf("String() without a commit = %q", got)
	}
	Commit = "a1b2c3d4e5f6"
	if got := String(); !strings.HasSuffix(got, " (a1b2c3d4e5f6)") {
		t.Errorf("String() with a commit = %q", got)
	}
}
