package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// runGoInstall runs the go feature's install step with a stub sudo that
// records what it is asked. goAfterInstall says whether the stub's
// "apt-get install" puts a go on the PATH, as the real package does, and
// withGo whether there is one before. failing is a fragment of a sudo
// command that fails. It returns what sudo was asked and the setup log.
func runGoInstall(t *testing.T, withGo, goAfterInstall bool, failing string) (calls, log string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the install step is for a Linux sandbox: it needs /bin/sh")
	}
	dir := t.TempDir()
	callsFile := filepath.Join(dir, "calls")
	logFile := filepath.Join(dir, "log")
	stub := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"+body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	installGo := ""
	if goAfterInstall {
		installGo = "printf '#!/bin/sh\\n' >" + filepath.Join(dir, "go") + "; chmod +x " + filepath.Join(dir, "go") + "\n"
	}
	stub("sudo", "echo \"$@\" >>"+callsFile+"\ncase \"$*\" in *"+failing+"*) exit 1;; esac\ncase \"$*\" in *'install -y golang-1.26'*) "+installGo+";; esac\n")
	if withGo {
		stub("go", "")
	}
	for _, tool := range []string{"date", "chmod"} {
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
	cmd.Env = []string{"PATH=" + dir, "GO_INSTALL_LOG=" + logFile}
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("install step failed: %v\n%s", err, out)
	}
	recorded, _ := os.ReadFile(callsFile)
	logged, _ := os.ReadFile(logFile)
	return string(recorded), string(logged)
}

func TestTheGoInstallStepLeavesAGoThatIsThere(t *testing.T) {
	if calls, _ := runGoInstall(t, true, false, "never-matches"); calls != "" {
		t.Errorf("installed over an existing go: %q", calls)
	}
}

func TestTheGoInstallStepInstallsGoAndSaysNothingWhenItIsOnThePath(t *testing.T) {
	calls, log := runGoInstall(t, false, true, "never-matches")
	if !strings.Contains(calls, "install -y golang-1.26") {
		t.Errorf("sudo was asked: %q, want golang-1.26 installed", calls)
	}
	if strings.Contains(log, "WARN") {
		t.Errorf("warned although go is on the PATH: %s", log)
	}
}

// The package once looked installed while go wasn't reachable (links made to a
// folder the package doesn't have): an install that leaves no go is said.
func TestTheGoInstallStepWarnsWhenTheInstallLeavesNoGoOnThePath(t *testing.T) {
	_, log := runGoInstall(t, false, false, "never-matches")
	if !strings.Contains(log, "WARN: Go installed but go is not on the PATH") {
		t.Errorf("log: %q, want a warning that go isn't on the PATH", log)
	}
}

func TestTheGoInstallStepRetriesAfterAnAptUpdateAndWarnsWhenItStillFails(t *testing.T) {
	calls, log := runGoInstall(t, false, false, "golang-1.26")
	if !strings.Contains(calls, "update") {
		t.Errorf("sudo was asked: %q, want an apt-get update retry", calls)
	}
	if !strings.Contains(log, "WARN: Go install failed") {
		t.Errorf("log: %q, want the failed install said", log)
	}
}
