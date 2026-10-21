package main

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func fakeSbx(t *testing.T, body string) (sbx, calls string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the fake sbx is a shell script")
	}
	dir := t.TempDir()
	sbx = filepath.Join(dir, "sbx")
	calls = filepath.Join(dir, "calls")
	script := "#!/bin/sh\necho \"$@\" >> '" + calls + "'\n" + body
	if err := os.WriteFile(sbx, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return sbx, calls
}

func TestRestartSbxDaemonRunsDaemonRestart(t *testing.T) {
	sbx, calls := fakeSbx(t, "exit 0\n")

	restartSbxDaemon(sbx)

	got, err := os.ReadFile(calls)
	if err != nil {
		t.Fatalf("sbx wasn't run: %v", err)
	}
	if string(got) != "daemon restart\n" {
		t.Errorf("sbx was run with %q, want \"daemon restart\"", got)
	}
}

func TestFinishSbxInstallRestartsTheDaemonBeforeReportingTheVersion(t *testing.T) {
	sbx, calls := fakeSbx(t, "[ \"$1\" = version ] && echo 'Client Version: v9.9.9'\nexit 0\n")

	if err := finishSbxInstall(sbxMethod{sbx: sbx, binDir: filepath.Dir(sbx)}); err != nil {
		t.Fatal(err)
	}

	got, _ := os.ReadFile(calls)
	if string(got) != "daemon restart\nversion\n" {
		t.Errorf("sbx was run with %q, want the daemon restarted, then the version asked for", got)
	}
}

func TestRestartSbxDaemonFailingDoesntFailTheInstall(t *testing.T) {
	sbx, _ := fakeSbx(t, "[ \"$1\" = version ] && echo 'Client Version: v9.9.9' && exit 0\nexit 1\n")

	if err := finishSbxInstall(sbxMethod{sbx: sbx, binDir: filepath.Dir(sbx)}); err != nil {
		t.Errorf("the install was reported as failed: %v", err)
	}
}
