package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// The formatting check exists three times (the Makefile, make.sh and make.ps1), and has to check the same files in each:
// every one that CI would see. What is git-ignored scratch (tmp/) or git's own is not that, and a real file is.
// Each is run as it is, on a tree of its own.

const formattedGo = "package ok\n\nfunc F() {}\n"
const unformattedGo = "package   bad\nfunc  F( ) { }\n"

// fmtTree makes a repository-shaped folder with the three scripts copied into it, formatted Go in it, and what the
// check is to leave alone (unformatted Go under tmp/ and .git/). with, if not empty, is one more file: unformatted,
// at that path.
func fmtTree(t *testing.T, with string) string {
	t.Helper()
	dir := t.TempDir()
	root := repoRoot(t)
	for _, name := range []string{"Makefile", "make.sh", "make.ps1"} {
		data, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	files := map[string]string{
		"internal/ok/ok.go":     formattedGo,
		"version.go":            formattedGo,
		"tmp/scratch/bad.go":    unformattedGo,
		".git/hooks/ignored.go": unformattedGo,
	}
	if with != "" {
		files[with] = unformattedGo
	}
	for path, body := range files {
		full := filepath.Join(dir, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

type fmtScript struct {
	name string
	// command is what to run in the tree, and tool what it needs.
	command []string
	tool    string
}

func fmtScripts() []fmtScript {
	return []fmtScript{
		{"make.sh", []string{"bash", "./make.sh", "fmt-check"}, "bash"},
		{"Makefile", []string{"make", "fmt-check"}, "make"},
		{"make.ps1", []string{"pwsh", "-NoProfile", "-File", "./make.ps1", "fmt-check"}, "pwsh"},
	}
}

func runFmtScript(t *testing.T, s fmtScript, dir string) (string, error) {
	t.Helper()
	if _, err := exec.LookPath(s.tool); err != nil {
		t.Skipf("%s isn't installed", s.tool)
	}
	if _, err := exec.LookPath("gofmt"); err != nil {
		t.Skip("no gofmt")
	}
	if runtime.GOOS == "windows" && s.tool != "pwsh" {
		t.Skip("a Unix shell script")
	}
	cmd := exec.Command(s.command[0], s.command[1:]...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func TestTheFormattingCheckLeavesScratchAndGitsOwnFilesAlone(t *testing.T) {
	for _, s := range fmtScripts() {
		t.Run(s.name, func(t *testing.T) {
			if out, err := runFmtScript(t, s, fmtTree(t, "")); err != nil {
				t.Errorf("it failed over files that CI never sees: %v\n%s", err, out)
			}
		})
	}
}

func TestTheFormattingCheckFailsForARealUnformattedFileAndNamesIt(t *testing.T) {
	for _, s := range fmtScripts() {
		for _, where := range []string{"internal/deep/er/bad.go", "bad.go"} {
			t.Run(s.name+" "+where, func(t *testing.T) {
				out, err := runFmtScript(t, s, fmtTree(t, where))
				if err == nil {
					t.Fatalf("it passed with an unformatted %s:\n%s", where, out)
				}
				if !strings.Contains(strings.ReplaceAll(out, "\\", "/"), where) {
					t.Errorf("it doesn't name %s:\n%s", where, out)
				}
			})
		}
	}
}
