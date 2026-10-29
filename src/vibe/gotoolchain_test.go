package main

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"testing"
)

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
	if want, got := "go"+string(m[1]), runtime.Version(); got != want {
		t.Fatalf("go.mod says %s but the toolchain is %s: change the `go` line in go.mod to %s (CI follows it), run `make check`, and commit it",
			want, got, got[2:])
	}
}
