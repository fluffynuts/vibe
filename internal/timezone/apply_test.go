package timezone

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// tzSandbox puts an `sbx` on PATH whose exec runs the command here, as root
// would (sudo -n is dropped), with /etc and /usr/share/zoneinfo standing for
// folders in the returned directory, and paths in what it prints put back.
// Each command it runs is added to the returned directory's "ran" file.
func tzSandbox(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the fake sbx is a shell script")
	}
	root := t.TempDir()
	for _, dir := range []string{"etc", "zoneinfo"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	bin := t.TempDir()
	script := "#!/bin/sh\n[ \"$1\" = exec ] || exit 1\nshift\n[ \"$1\" = -i ] && shift\nshift 2\n" +
		"[ \"$1\" = sudo ] && shift 2\n" +
		"echo \"$*\" >> " + root + "/ran\n" +
		"for a; do shift; set -- \"$@\" \"$(printf %s \"$a\" | sed -e \"s#^/usr/share/zoneinfo#" + root + "/zoneinfo#\" -e \"s#^/etc#" + root + "/etc#\")\"; done\n" +
		"out=$(mktemp)\n\"$@\" > \"$out\"\nrc=$?\n" +
		"sed -e \"s#" + root + "/zoneinfo#/usr/share/zoneinfo#g\" -e \"s#" + root + "/etc#/etc#g\" \"$out\"\nrm -f \"$out\"\nexit $rc\n"
	if err := os.WriteFile(filepath.Join(bin, "sbx"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	write := func(rel, content string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(filepath.Join(root, rel)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, rel), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("zoneinfo/Africa/Johannesburg", "zone")
	write("zoneinfo/Europe/London", "zone")
	write("etc/sandbox-persistent.sh", "export FOO=1\n")
	return root
}

func ran(t *testing.T, root string) string {
	t.Helper()
	data, _ := os.ReadFile(filepath.Join(root, "ran"))
	os.Remove(filepath.Join(root, "ran"))
	return string(data)
}

func TestApplySetsTheZoneEverywhereItIsKept(t *testing.T) {
	root := tzSandbox(t)
	if err := Apply("box", "Africa/Johannesburg"); err != nil {
		t.Fatal(err)
	}
	if link, _ := os.Readlink(filepath.Join(root, "etc", "localtime")); link != filepath.Join(root, "zoneinfo", "Africa", "Johannesburg") {
		t.Errorf("/etc/localtime links to %q", link)
	}
	if data, _ := os.ReadFile(filepath.Join(root, "etc", "timezone")); string(data) != "Africa/Johannesburg\n" {
		t.Errorf("/etc/timezone = %q", data)
	}
	if data, _ := os.ReadFile(filepath.Join(root, "etc", "sandbox-persistent.sh")); string(data) != "export FOO=1\nexport TZ=Africa/Johannesburg "+tzMarker+"\n" {
		t.Errorf("the persistent environment = %q", data)
	}
}

func TestApplyChangesNothingThatsRightAlready(t *testing.T) {
	root := tzSandbox(t)
	if err := Apply("box", "Africa/Johannesburg"); err != nil {
		t.Fatal(err)
	}
	ran(t, root)
	if err := Apply("box", "Africa/Johannesburg"); err != nil {
		t.Fatal(err)
	}
	if got := ran(t, root); strings.Contains(got, "ln ") || strings.Contains(got, "tee ") {
		t.Errorf("wrote, though all was right already:\n%s", got)
	}
	if err := Apply("box", "Europe/London"); err != nil {
		t.Fatal(err)
	}
	got := ran(t, root)
	for _, want := range []string{"ln -sfn", "tee /etc/timezone", "tee /etc/sandbox-persistent.sh"} {
		if !strings.Contains(got, want) {
			t.Errorf("a new zone didn't run %q:\n%s", want, got)
		}
	}
}

func TestApplySaysWhenTheSandboxHasNoSuchZone(t *testing.T) {
	tzSandbox(t)
	if err := Apply("box", "Mars/Olympus_Mons"); err == nil || !strings.Contains(err.Error(), "has no timezone Mars/Olympus_Mons") {
		t.Errorf("err = %v", err)
	}
	if err := Apply("box", "../etc/passwd"); err == nil || !strings.Contains(err.Error(), "is not a timezone") {
		t.Errorf("err = %v", err)
	}
}

func TestApplyLeavesASandboxWithoutAPersistentEnvironmentWithoutOne(t *testing.T) {
	root := tzSandbox(t)
	os.Remove(filepath.Join(root, "etc", "sandbox-persistent.sh"))
	if err := Apply("box", "Africa/Johannesburg"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "etc", "sandbox-persistent.sh")); err == nil {
		t.Error("a persistent environment file was made")
	}
}
