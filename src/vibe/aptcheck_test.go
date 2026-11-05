package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// tools/check-apt-packages.sh is run with a stand-in apt-cache that knows every package but those it is told it
// doesn't, since the real check is run inside a sandbox, against the image's apt.
func runAptCheck(t *testing.T, folder string, unknown ...string) (out string, exit int) {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("the check is for a sandbox's apt")
	}
	if probe := exec.Command("sh", "-c", "echo x | grep -oP 'x'"); probe.Run() != nil {
		t.Skip("this grep has no -P")
	}
	dir := t.TempDir()
	body := "#!/bin/sh\ncase \"$2\" in\n"
	for _, name := range unknown {
		body += "  " + name + ") echo \"" + name + ":\"; echo \"  Candidate: (none)\" ;;\n"
	}
	body += "  *) echo \"$2:\"; echo \"  Candidate: 1.0\" ;;\nesac\n"
	writeExecutable(t, filepath.Join(dir, "apt-cache"), body)
	args := []string{filepath.Join(repoRoot(t), "tools", "check-apt-packages.sh")}
	if folder != "" {
		args = append(args, folder)
	}
	cmd := exec.Command("bash", args...)
	cmd.Env = append(os.Environ(), "PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	got, err := cmd.CombinedOutput()
	if ee, ok := err.(*exec.ExitError); ok {
		exit = ee.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	return string(got), exit
}

func TestTheAptCheckNamesAPackageTheImageDoesNotHave(t *testing.T) {
	dir := t.TempDir()
	writeExecutable(t, filepath.Join(dir, "feature", "install-scripts", "01-install"),
		"sudo DEBIAN_FRONTEND=noninteractive apt-get -o DPkg::Lock::Timeout=300 install -y mysql-server file </dev/null >>/tmp/log 2>&1 \\\n"+
			"  || echo WARN\n"+
			"tools=\"\"\ncommand -v curl >/dev/null 2>&1 || tools=\"$tools curl\"\n"+
			"apt_install libnuma1 || apt_install libaio1 || true\n"+
			"apt_install install -y nodejs-24 || apt_install update\n"+
			"# install -y in-a-comment-only\n")
	out, exit := runAptCheck(t, dir, "mysql-server", "libaio1")
	if exit != 1 {
		t.Errorf("exit %d, want 1:\n%s", exit, out)
	}
	for _, want := range []string{"MISSING  mysql-server", "MISSING  libaio1", "ok       file", "ok       curl", "ok       libnuma1", "ok       nodejs-24"} {
		if !strings.Contains(out, want) {
			t.Errorf("the report lacks %q:\n%s", want, out)
		}
	}
	for _, not := range []string{"in-a-comment-only", "update ", "install ", "sudo"} {
		if strings.Contains(out, "ok       "+not) || strings.Contains(out, "MISSING  "+not) {
			t.Errorf("it took %q for a package:\n%s", not, out)
		}
	}
}

// What it reads has to stay what the library's scripts do: these are packages the library asks for, and a change to
// how a script asks for one that hides it from the check shows here.
func TestTheAptCheckSeesThePackagesTheLibraryAsksFor(t *testing.T) {
	out, exit := runAptCheck(t, "")
	if exit != 0 {
		t.Errorf("exit %d with every package known:\n%s", exit, out)
	}
	for _, want := range []string{"redis-server", "rabbitmq-server", "dotnet-sdk-10", "golang-1.26", "nodejs-24", "npm",
		"default-mysql-server", "elasticsearch", "libnuma1", "libncurses6", "libaio1t64", "xz-utils", "util-linux", "gnupg", "curl", "tzdata"} {
		if !strings.Contains(out, "ok       "+want+" ") {
			t.Errorf("the check never saw %s:\n%s", want, out)
		}
	}
}
