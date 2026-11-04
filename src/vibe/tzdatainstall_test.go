package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

// runTzdataInstall runs the base timezone feature's tzdata step with a stub sudo that
// records what it is asked, against a zoneinfo folder that exists or not.
func runTzdataInstall(t *testing.T, hasZoneinfo bool) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the install step is for a Linux sandbox: it needs /bin/sh")
	}
	dir := t.TempDir()
	calls := filepath.Join(dir, "calls")
	stub := "#!/bin/sh\necho \"$@\" >>" + calls + "\n"
	if err := os.WriteFile(filepath.Join(dir, "sudo"), []byte(stub), 0o755); err != nil {
		t.Fatal(err)
	}
	date, err := exec.LookPath("date")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(date, filepath.Join(dir, "date")); err != nil {
		t.Fatal(err)
	}
	zoneinfo := filepath.Join(dir, "zoneinfo")
	if hasZoneinfo {
		if err := os.Mkdir(zoneinfo, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("/bin/sh", filepath.Join(repoRoot(t), "library", "base", "timezone", "install-scripts", "01-install-tzdata"))
	cmd.Env = []string{"PATH=" + dir, "TZDATA_INSTALL_LOG=" + filepath.Join(dir, "log"), "TZDATA_ZONEINFO=" + zoneinfo}
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("install step failed: %v\n%s", err, out)
	}
	recorded, _ := os.ReadFile(calls)
	return string(recorded)
}

func TestTheTzdataStepInstallsTzdataWhenThereIsNoZoneinfo(t *testing.T) {
	want := "DEBIAN_FRONTEND=noninteractive apt-get -o DPkg::Lock::Timeout=300 install -y tzdata\n"
	if got := runTzdataInstall(t, false); got != want {
		t.Errorf("sudo was asked %q, want %q", got, want)
	}
}

func TestTheTzdataStepLeavesAZoneinfoThatIsThere(t *testing.T) {
	if got := runTzdataInstall(t, true); got != "" {
		t.Errorf("installed over a zoneinfo: %q", got)
	}
}
