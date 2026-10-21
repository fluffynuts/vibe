package sbxrun

import (
	"context"
	"strings"
	"testing"

	"vibe/internal/sbxtest"
)

// echoingSbx puts an sbx on PATH that prints its arguments on stdout,
// anything it's given on stdin after them, and a line on stderr; it fails,
// with exit code 3, when one of its arguments is "fail".
func echoingSbx(t *testing.T) {
	sbxtest.OnPath(t, "echo \"args: $*\"\n"+
		"[ \"$2\" = -i ] && { printf 'in: '; cat; }\n"+
		"echo 'on stderr' >&2\n"+
		"for a in \"$@\"; do [ \"$a\" = fail ] && exit 3; done\n"+
		"exit 0\n")
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
	out, err := ExecCapture(context.Background(), "box", "ls")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "args: exec box -- ls\n") || !strings.Contains(out, "on stderr") {
		t.Errorf("out = %q", out)
	}
	if out, err := ExecCapture(context.Background(), "box", "fail"); err == nil || !strings.Contains(out, "on stderr") {
		t.Errorf("failed: out = %q, err = %v", out, err)
	}
}

func TestExecSilentSaysOnlyWhetherItWorked(t *testing.T) {
	echoingSbx(t)
	if !ExecSilent(context.Background(), "box", "true") {
		t.Error("a command that worked was reported failed")
	}
	if ExecSilent(context.Background(), "box", "fail") {
		t.Error("a command that failed was reported working")
	}
}

func TestStopQuietSaysWhatSbxSaidWhenItFails(t *testing.T) {
	echoingSbx(t)
	if err := StopQuiet(context.Background(), "box"); err != nil {
		t.Fatal(err)
	}
	if err := StopQuiet(context.Background(), "fail"); err == nil || !strings.Contains(err.Error(), "args: stop fail") {
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

// What's read from a sandbox is the agent's to make, so there's a most
// vibe takes in: past it, the read fails, saying so, rather than vibe
// holding whatever it's given.
func TestWhatIsReadFromASandboxHasALimit(t *testing.T) {
	sbxtest.OnPath(t, "exec yes\n")
	old := maxCaptured
	maxCaptured = 1 << 20
	t.Cleanup(func() { maxCaptured = old })

	for name, read := range map[string]func() (string, error){
		"ExecStdout":      func() (string, error) { return ExecStdout(context.Background(), "box", "cat", "/f") },
		"ExecCapture":     func() (string, error) { return ExecCapture(context.Background(), "box", "cat", "/f") },
		"ExecInputStdout": func() (string, error) { return ExecInputStdout(context.Background(), "box", "", "cat") },
		"ExecInput":       func() (string, error) { return "", ExecInput("box", "", "cat") },
	} {
		out, err := read()
		if err == nil || !strings.Contains(err.Error(), "more than vibe takes in") {
			t.Errorf("%s: err = %v, want the limit", name, err)
		}
		if len(out) > maxCaptured {
			t.Errorf("%s: kept %d bytes, past the limit of %d", name, len(out), maxCaptured)
		}
	}
}

func TestSbxIsAvailableWhenItIsOnPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("PATH", t.TempDir())
	if Available() {
		t.Error("sbx was available with no sbx on PATH")
	}
	echoingSbx(t)
	if !Available() {
		t.Error("sbx wasn't available, though it's on PATH")
	}
}
