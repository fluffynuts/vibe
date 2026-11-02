package main

import (
	"go/version"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

// sameGo is whether go.mod's `go` line (1.28.0) and a toolchain's version (go1.28: the first release of a Go is
// written without its .0) name the same Go.
func sameGo(modLine, toolchain string) bool {
	return version.Compare(firstReleaseAsDotZero("go"+modLine), firstReleaseAsDotZero(toolchain)) == 0
}

// firstReleaseAsDotZero spells go1.28 as go1.28.0: go/version orders the first as the language version, before every
// release of it, including the one it means.
func firstReleaseAsDotZero(v string) string {
	if strings.Count(v, ".") == 1 && version.IsValid(v) && version.Lang(v) == v {
		return v + ".0"
	}
	return v
}

func TestGoModsLineAndTheToolchainNameTheSameGoWhateverTheSpelling(t *testing.T) {
	for _, c := range []struct {
		line, toolchain string
		same            bool
	}{
		{"1.27.3", "go1.27.3", true},
		{"1.28.0", "go1.28", true},
		{"1.28", "go1.28", true},
		{"1.27.3", "go1.28", false},
		{"1.27.3", "go1.27.4", false},
	} {
		if got := sameGo(c.line, c.toolchain); got != c.same {
			t.Errorf("sameGo(%q, %q) = %v, want %v", c.line, c.toolchain, got, c.same)
		}
	}
}

// CI installs the Go that go.mod names, and gofmt's output differs between Go versions, so a toolchain that has moved
// on from go.mod's line formats files CI then rejects. This fails the moment the two part, wherever the toolchain came
// from, so the bump is made on purpose rather than found in CI.
func TestGoModNamesTheGoThatBuildsIt(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(repoRoot(t), "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`(?m)^go (\S+)$`).FindSubmatch(data)
	if m == nil {
		t.Fatal("go.mod has no `go` line")
	}
	if want, got := "go"+string(m[1]), runtime.Version(); !sameGo(string(m[1]), got) {
		t.Fatalf("go.mod says %s but the toolchain is %s: change the `go` line in go.mod to %s (CI follows it), run `make check`, and commit it",
			want, got, got[2:])
	}
}
