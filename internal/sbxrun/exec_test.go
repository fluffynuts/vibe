package sbxrun

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// echoingSbx puts an sbx on PATH that prints its arguments on stdout,
// anything it's given on stdin after them, and a line on stderr; it fails,
// with exit code 3, when one of its arguments is "fail".
func echoingSbx(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in sbx is a shell script")
	}
	dir := t.TempDir()
	script := "#!/bin/sh\n" +
		"echo \"args: $*\"\n" +
		"[ \"$2\" = -i ] && { printf 'in: '; cat; }\n" +
		"echo 'on stderr' >&2\n" +
		"for a in \"$@\"; do [ \"$a\" = fail ] && exit 3; done\n" +
		"exit 0\n"
	if err := os.WriteFile(filepath.Join(dir, "sbx"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestExecStdoutIsStdoutAlone(t *testing.T) {
	echoingSbx(t)
	out, err := ExecStdout(context.Background(), "box", "cat", "/f")
	if err != nil {
		t.Fatal(err)
	}
	if out != "args: exec box -- cat /f\n" {
		t.Errorf("out = %q", out)
	}
}

func TestAFailedExecStdoutSaysWhatSbxSaidAndKeepsWhatItPrinted(t *testing.T) {
	echoingSbx(t)
	out, err := ExecStdout(context.Background(), "box", "fail")
	if err == nil || !strings.Contains(err.Error(), "exit status 3") || !strings.Contains(err.Error(), "on stderr") {
		t.Errorf("err = %v, want the exit status and what sbx said", err)
	}
	if out != "args: exec box -- fail\n" {
		t.Errorf("out = %q", out)
	}
}

func TestExecInputStdoutGivesTheCommandItsInput(t *testing.T) {
	echoingSbx(t)
	out, err := ExecInputStdout(context.Background(), "box", "typed", "tee", "/f")
	if err != nil {
		t.Fatal(err)
	}
	if out != "args: exec -i box -- tee /f\nin: typed" {
		t.Errorf("out = %q", out)
	}
	if _, err := ExecInputStdout(context.Background(), "box", "", "fail"); err == nil || !strings.Contains(err.Error(), "on stderr") {
		t.Errorf("failed: err = %v, want what sbx said", err)
	}
}

func TestExecInputGivesTheCommandItsInputAndSaysWhyItFailed(t *testing.T) {
	echoingSbx(t)
	if err := ExecInput("box", "typed", "tee", "/f"); err != nil {
		t.Fatal(err)
	}
	err := ExecInput("box", "typed", "fail")
	if err == nil || !strings.Contains(err.Error(), "exit status 3") || !strings.Contains(err.Error(), "in: typed") || !strings.Contains(err.Error(), "on stderr") {
		t.Errorf("err = %v, want the exit status and all sbx printed", err)
	}
}

func TestExecCaptureIsAllSbxPrinted(t *testing.T) {
	echoingSbx(t)
	out, err := ExecCapture("box", "ls")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "args: exec box -- ls\n") || !strings.Contains(out, "on stderr") {
		t.Errorf("out = %q", out)
	}
	if out, err := ExecCapture("box", "fail"); err == nil || !strings.Contains(out, "on stderr") {
		t.Errorf("failed: out = %q, err = %v", out, err)
	}
}

func TestExecSilentSaysOnlyWhetherItWorked(t *testing.T) {
	echoingSbx(t)
	if !ExecSilent("box", "true") {
		t.Error("a command that worked was reported failed")
	}
	if ExecSilentContext(context.Background(), "box", "fail") {
		t.Error("a command that failed was reported working")
	}
}

func TestStopQuietSaysWhatSbxSaidWhenItFails(t *testing.T) {
	echoingSbx(t)
	if err := StopQuiet("box"); err != nil {
		t.Fatal(err)
	}
	if err := StopQuiet("fail"); err == nil || !strings.Contains(err.Error(), "args: stop fail") {
		t.Errorf("err = %v, want what sbx printed", err)
	}
}

func TestWriteFileSaysWhyItCouldNotWrite(t *testing.T) {
	echoingSbx(t)
	if err := WriteFile("box", "/home/agent/f", "content"); err != nil {
		t.Fatal(err)
	}
	err := WriteFile("box", "fail", "content")
	if err == nil || !strings.Contains(err.Error(), "writing fail in the sandbox") || !strings.Contains(err.Error(), "on stderr") {
		t.Errorf("err = %v, want which file and what sbx said", err)
	}
}

func TestPublishedPortsSaysWhatSbxSaidWhenItFails(t *testing.T) {
	echoingSbx(t)
	_, err := PublishedPorts("fail")
	if err == nil || !strings.Contains(err.Error(), "sbx ports fail --json") || !strings.Contains(err.Error(), "on stderr") {
		t.Errorf("err = %v, want the command and what sbx said", err)
	}
}
