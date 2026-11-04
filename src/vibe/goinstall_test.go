package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// runGoInstall runs the go feature's install step with a stub sudo that
// records what it is asked and makes the links itself, and, if withGo, a go
// already on the PATH. failing is a fragment of a sudo command that fails.
func runGoInstall(t *testing.T, withGo bool, failing string) (calls string, links string) {
	t.Helper()
	dir := t.TempDir()
	linkDir := filepath.Join(dir, "links")
	if err := os.Mkdir(linkDir, 0o755); err != nil {
		t.Fatal(err)
	}
	callsFile := filepath.Join(dir, "calls")
	stub := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"+body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	stub("sudo", "echo \"$@\" >>"+callsFile+"\ncase \"$*\" in *"+failing+"*) exit 1;; esac\ncase \"$1\" in ln) \"$@\";; esac\n")
	if withGo {
		stub("go", "")
	}
	for _, tool := range []string{"date", "ln"} {
		real, err := exec.LookPath(tool)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(real, filepath.Join(dir, tool)); err != nil {
			t.Fatal(err)
		}
	}
	script := filepath.Join(repoRoot(t), "library", "go", "install-scripts", "01-install-go")
	cmd := exec.Command("/bin/sh", script)
	cmd.Env = []string{
		"PATH=" + dir,
		"GO_INSTALL_LOG=" + filepath.Join(dir, "log"),
		"GO_BIN=/opt/fake-go/bin",
		"GO_LINKS=" + linkDir,
	}
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("install step failed: %v\n%s", err, out)
	}
	recorded, _ := os.ReadFile(callsFile)
	entries, _ := os.ReadDir(linkDir)
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return string(recorded), strings.Join(names, ",")
}

func TestTheGoInstallStepLeavesAGoThatIsThere(t *testing.T) {
	if calls, _ := runGoInstall(t, true, "never-matches"); calls != "" {
		t.Errorf("installed over an existing go: %q", calls)
	}
}

func TestTheGoInstallStepInstallsGoAndLinksItsTools(t *testing.T) {
	calls, links := runGoInstall(t, false, "never-matches")
	if !strings.Contains(calls, "install -y golang-1.26") {
		t.Errorf("sudo was asked: %q, want golang-1.26 installed", calls)
	}
	if links != "go,gofmt" {
		t.Errorf("linked: %q, want go,gofmt", links)
	}
}

func TestTheGoInstallStepLinksNothingWhenTheInstallFails(t *testing.T) {
	calls, links := runGoInstall(t, false, "golang-1.26")
	if !strings.Contains(calls, "update") {
		t.Errorf("sudo was asked: %q, want an apt-get update retry", calls)
	}
	if links != "" {
		t.Errorf("linked %q after a failed install", links)
	}
}
