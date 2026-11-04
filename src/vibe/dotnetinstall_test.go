package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

// The package is named as the image's apt knows it: dotnet-sdk-10.0 is
// Microsoft's name for it, and sbx's image has no Microsoft repository.
func TestTheDotnetStepInstallsThePackageTheImagesAptHas(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the install step is for a Linux sandbox: it needs /bin/sh")
	}
	dir := t.TempDir()
	calls := filepath.Join(dir, "calls")
	if err := os.WriteFile(filepath.Join(dir, "sudo"), []byte("#!/bin/sh\necho \"$@\" >>"+calls+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	date, err := exec.LookPath("date")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(date, filepath.Join(dir, "date")); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("/bin/sh", filepath.Join(repoRoot(t), "library", "dotnet", "install-scripts", "01-install-dotnet-sdk"))
	cmd.Env = []string{"PATH=" + dir, "DOTNET_INSTALL_LOG=" + filepath.Join(dir, "log")}
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	got, _ := os.ReadFile(calls)
	want := "DEBIAN_FRONTEND=noninteractive apt-get -o DPkg::Lock::Timeout=300 install -y dotnet-sdk-10\n"
	if string(got) != want {
		t.Errorf("sudo was asked %q, want %q", got, want)
	}
}
