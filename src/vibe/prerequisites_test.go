package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// The prerequisites step is run as it is, with a stub sudo that records what apt-get is asked for and makes what is
// installed appear (so that the step's closing check for what is still missing is a real one). which says what the
// image has to begin with; failing is a package name that apt says it hasn't got.
type prereqRun struct {
	calls []string // the apt-get installs and updates, one per call
	log   string
}

var prereqPackageCommand = map[string]string{
	"curl": "curl", "gnupg": "gpg", "tar": "tar", "gzip": "gzip", "xz-utils": "xz", "util-linux": "flock", "file": "file",
	"nodejs-24": "node", "nodejs": "node",
}

func runPrerequisites(t *testing.T, have []string, haveCerts bool, failing string) prereqRun {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("the step is for a Linux sandbox")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "bin")
	os.MkdirAll(bin, 0o755)
	calls := filepath.Join(dir, "calls")
	bundle := filepath.Join(dir, "ca-certificates.crt")
	if haveCerts {
		os.WriteFile(bundle, []byte("certs"), 0o644)
	}
	for _, tool := range have {
		writeExecutable(t, filepath.Join(bin, tool), "")
	}
	mysql84Tools(t, bin, "date", "cat")
	var installs strings.Builder
	for pkg, cmd := range prereqPackageCommand {
		installs.WriteString(`    ` + pkg + `) : >"` + bin + `/` + cmd + `"; chmod +x "` + bin + `/` + cmd + `" ;;` + "\n")
	}
	installs.WriteString(`    ca-certificates) echo certs >"` + bundle + `" ;;` + "\n")
	writeExecutable(t, filepath.Join(bin, "sudo"), `echo "$*" >>`+calls+`
case "$*" in
  *"`+failing+`"*) exit 100 ;;
esac
case "$*" in
  *" install "*)
    for arg in "$@"; do
      case "$arg" in
`+installs.String()+`      esac
    done ;;
esac
exit 0
`)
	mysql84Tools(t, bin, "chmod")
	cmd := exec.Command("/bin/sh", filepath.Join(repoRoot(t), "library", "base", "prerequisites", "install-scripts", "01-install-prerequisites"))
	cmd.Env = []string{"PATH=" + bin, "PREREQ_LOG=" + filepath.Join(dir, "log"), "PREREQ_CA_BUNDLE=" + bundle}
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("the step failed: %v\n%s", err, out)
	}
	var run prereqRun
	recorded, _ := os.ReadFile(calls)
	for _, line := range strings.Split(strings.TrimSpace(string(recorded)), "\n") {
		if strings.Contains(line, "apt-get") {
			run.calls = append(run.calls, line)
		}
	}
	logged, _ := os.ReadFile(filepath.Join(dir, "log"))
	run.log = string(logged)
	return run
}

var allPrereqTools = []string{"curl", "gpg", "tar", "gzip", "xz", "flock", "file", "node"}

func TestPrerequisitesAreNotInstalledWhenTheyAreAllThere(t *testing.T) {
	run := runPrerequisites(t, allPrereqTools, true, "never-matches")
	if len(run.calls) != 0 {
		t.Errorf("apt was asked for something although everything is there: %v", run.calls)
	}
}

func TestPrerequisitesTheImageLacksAreInstalledInOneAptRun(t *testing.T) {
	run := runPrerequisites(t, nil, false, "never-matches")
	if len(run.calls) != 1 {
		t.Fatalf("expected one apt run, got %d: %v", len(run.calls), run.calls)
	}
	for _, want := range []string{"curl", "ca-certificates", "gnupg", "tar", "gzip", "xz-utils", "util-linux", "file", "nodejs-24"} {
		if !strings.Contains(run.calls[0], " "+want) {
			t.Errorf("%s isn't in the one apt run: %s", want, run.calls[0])
		}
	}
	if strings.Contains(run.log, "WARN") {
		t.Errorf("warned although everything was installed:\n%s", run.log)
	}
}

func TestOnlyWhatIsMissingIsAskedFor(t *testing.T) {
	run := runPrerequisites(t, []string{"curl", "gpg", "tar", "gzip", "flock", "file", "node"}, true, "never-matches")
	if len(run.calls) != 1 || !strings.HasSuffix(run.calls[0], "install -y xz-utils") {
		t.Errorf("expected xz-utils alone: %v", run.calls)
	}
	run = runPrerequisites(t, []string{"curl", "gpg", "tar", "gzip", "xz", "flock", "file"}, true, "never-matches")
	if len(run.calls) != 1 || !strings.HasSuffix(run.calls[0], "install -y nodejs-24") {
		t.Errorf("expected Node alone: %v", run.calls)
	}
}

// apt installs nothing when one name in the command is unknown, so one the image hasn't got (nodejs-24 is a Docker
// Hardened Images name) must not stop the tools being installed: they are asked for on their own, and Node falls back
// to Debian's.
func TestAPackageTheImageLacksDoesNotKeepTheOthersFromBeingInstalled(t *testing.T) {
	run := runPrerequisites(t, nil, false, "nodejs-24")
	var toolsAlone, debiansNode bool
	for _, call := range run.calls {
		if strings.HasSuffix(call, "install -y curl ca-certificates gnupg tar gzip xz-utils util-linux file") {
			toolsAlone = true
		}
		if strings.HasSuffix(call, "install -y nodejs") {
			debiansNode = true
		}
	}
	if !toolsAlone || !debiansNode {
		t.Errorf("the tools weren't installed on their own, or Node didn't fall back to nodejs: %v", run.calls)
	}
	if strings.Contains(run.log, "WARN") {
		t.Errorf("warned although everything ended up installed:\n%s", run.log)
	}
}

func TestWhatCannotBeInstalledIsSaidByName(t *testing.T) {
	run := runPrerequisites(t, nil, false, "util-linux")
	if !strings.Contains(run.log, "WARN: flock is still missing") {
		t.Errorf("the log doesn't name flock as still missing:\n%s", run.log)
	}
}
