package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// The elasticsearch feature's repository step is run as it is, with a stub sudo that records what it is asked, and
// curl and gpg only when the image is to have them: it installs what it needs from the image's apt, or nothing.
func runElasticRepoStep(t *testing.T, haveCurl, haveGpg, haveCerts bool) (calls, log string) {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("the step is for a Linux sandbox")
	}
	dir := t.TempDir()
	callsFile, logFile := filepath.Join(dir, "calls"), filepath.Join(dir, "log")
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"+body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	write("sudo", "echo \"$@\" >>"+callsFile+"\ncat >/dev/null\n")
	if haveCurl {
		write("curl", "echo key\n")
	}
	if haveGpg {
		write("gpg", "")
	}
	bundle := filepath.Join(dir, "no-bundle")
	if haveCerts {
		bundle = filepath.Join(dir, "ca-certificates.crt")
		if err := os.WriteFile(bundle, []byte("certs"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, tool := range []string{"date", "cat"} {
		real, err := exec.LookPath(tool)
		if err != nil {
			t.Skipf("no %s", tool)
		}
		if err := os.Symlink(real, filepath.Join(dir, tool)); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("/bin/sh", filepath.Join(repoRoot(t), "library", "elasticsearch", "install-scripts", "01-enable-elasticsearch-repo"))
	cmd.Env = []string{"PATH=" + dir, "ELASTIC_SETUP_LOG=" + logFile, "ELASTIC_CA_BUNDLE=" + bundle}
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	recorded, _ := os.ReadFile(callsFile)
	logged, _ := os.ReadFile(logFile)
	return string(recorded), string(logged)
}

func TestTheElasticRepoStepInstallsCurlGnupgAndCertificatesTheImageLacks(t *testing.T) {
	calls, _ := runElasticRepoStep(t, false, false, false)
	if !strings.Contains(calls, "install -y curl gnupg ca-certificates") {
		t.Errorf("sudo was asked: %q, want curl, gnupg and ca-certificates installed", calls)
	}
}

func TestTheElasticRepoStepInstallsOnlyWhatIsMissing(t *testing.T) {
	calls, _ := runElasticRepoStep(t, true, false, true)
	if !strings.Contains(calls, "install -y gnupg\n") || strings.Contains(calls, "install -y curl") {
		t.Errorf("sudo was asked: %q, want gnupg alone installed", calls)
	}
}

func TestTheElasticRepoStepInstallsNothingWhenTheToolsAreThereAndAddsTheRepository(t *testing.T) {
	calls, log := runElasticRepoStep(t, true, true, true)
	if strings.Contains(calls, "install -y") {
		t.Errorf("installed tools the image has: %q", calls)
	}
	for _, want := range []string{"gpg --batch --yes --dearmor -o /usr/share/keyrings/elasticsearch-keyring.gpg", "tee /etc/apt/sources.list.d/elastic-9.x.list", "apt-get -o DPkg::Lock::Timeout=300 update"} {
		if !strings.Contains(calls, want) {
			t.Errorf("sudo was never asked %q:\n%s", want, calls)
		}
	}
	if strings.Contains(log, "WARN") {
		t.Errorf("warned although the repository was added: %s", log)
	}
}
