package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// Features that download over https and unpack what they get make sure of curl, tar and the certificates, from the
// image's apt, rather than assume the image has them (the one before this one's did not have everything). Their
// install steps are run as they are with a stub sudo that records what it is asked, and only the tools the image is
// to have on the PATH.
func runToolEnsure(t *testing.T, script string, env map[string]string, have ...string) string {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("the step is for a Linux sandbox")
	}
	dir := t.TempDir()
	calls := filepath.Join(dir, "calls")
	if err := os.WriteFile(filepath.Join(dir, "sudo"), []byte("#!/bin/sh\necho \"$@\" >>"+calls+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, tool := range append([]string{"date"}, have...) {
		real, err := exec.LookPath(tool)
		if err != nil {
			t.Skipf("no %s on this machine", tool)
		}
		if err := os.Symlink(real, filepath.Join(dir, tool)); err != nil {
			t.Fatal(err)
		}
	}
	bundle := filepath.Join(dir, "certs.crt")
	if err := os.WriteFile(bundle, []byte("certs"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("/bin/sh", filepath.Join(repoRoot(t), script))
	cmd.Env = []string{"PATH=" + dir, "PWSH_LOG=" + filepath.Join(dir, "log"), "DOTNET_INSTALL_LOG=" + filepath.Join(dir, "log"),
		"PWSH_CA_BUNDLE=" + bundle, "DOTNET_CA_BUNDLE=" + bundle}
	for k, v := range env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	cmd.Run() // what comes after (the download itself) can't work here: only what was asked of apt matters
	got, _ := os.ReadFile(calls)
	return string(got)
}

func TestThePwshStepInstallsCurlTarAndGzipTheImageLacks(t *testing.T) {
	calls := runToolEnsure(t, "library/pwsh/install-scripts/01-install-pwsh", nil)
	if !strings.Contains(calls, "install -y curl tar gzip\n") {
		t.Errorf("sudo was asked: %q, want curl, tar and gzip installed", calls)
	}
}

func TestThePwshStepInstallsNothingWhenItsToolsAreThere(t *testing.T) {
	calls := runToolEnsure(t, "library/pwsh/install-scripts/01-install-pwsh", nil, "curl", "tar", "gzip", "uname")
	if strings.Contains(calls, "install -y") {
		t.Errorf("installed tools the image has: %q", calls)
	}
}

func TestTheDotnetRuntimeStepInstallsCurlAndTarTheImageLacks(t *testing.T) {
	calls := runToolEnsure(t, "library/dotnet/install-scripts/02-install-aspnet8-runtime", nil)
	if !strings.Contains(calls, "install -y curl tar\n") {
		t.Errorf("sudo was asked: %q, want curl and tar installed", calls)
	}
}

func TestTheDotnetRuntimeStepInstallsNothingWhenItsToolsAreThere(t *testing.T) {
	calls := runToolEnsure(t, "library/dotnet/install-scripts/02-install-aspnet8-runtime", nil, "curl", "tar")
	if strings.Contains(calls, "install -y") {
		t.Errorf("installed tools the image has: %q", calls)
	}
}
