package sbxrun

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"vibe/internal/sbxtest"
)

// processSbx puts an sbx on PATH for the process tests: it prints a line on stdout and one on stderr; asked to "ask" it
// then reads a line and says what it got; as login it then waits to be ended; asked to "fail" it exits 3.
func processSbx(t *testing.T) {
	sbxtest.OnPath(t, "echo \"args: $*\"\n"+
		"echo 'on stderr' >&2\n"+
		"case \"$*\" in\n"+
		"  *\"-- ask\") read -r typed; echo \"got: $typed\" ;;\n"+
		"  login) exec sleep 30 ;;\n"+
		"  *\"-- fail\") exit 3 ;;\n"+
		"esac\n")
}

const within = 10 * time.Second

func collect(t *testing.T, p *Process) ([]string, error) {
	t.Helper()
	var lines []string
	for l := range p.Lines() {
		lines = append(lines, l)
	}
	select {
	case err := <-p.Done():
		return lines, err
	case <-time.After(within):
		t.Fatal("the process never ended")
		return nil, nil
	}
}

func TestAProcessGivesWhatItPrintsOnEitherStreamLineByLine(t *testing.T) {
	processSbx(t)
	p, err := StartExec("box", "plain")
	if err != nil {
		t.Fatal(err)
	}
	lines, err := collect(t, p)
	joined := strings.Join(lines, "|")
	if !strings.Contains(joined, "args: exec -i box -- plain") || !strings.Contains(joined, "on stderr") {
		t.Errorf("lines = %q", lines)
	}
	if err != nil {
		t.Errorf("err = %v", err)
	}
}

func TestAnExecProcessTakesWhatIsSentToIt(t *testing.T) {
	processSbx(t)
	p, err := StartExec("box", "ask")
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Send("my-code"); err != nil {
		t.Fatal(err)
	}
	lines, err := collect(t, p)
	if err != nil {
		t.Fatal(err)
	}
	if got := lines[len(lines)-1]; got != "got: my-code" {
		t.Errorf("lines = %q", lines)
	}
}

func TestALoginProcessTakesNoInput(t *testing.T) {
	processSbx(t)
	p, err := StartLogin()
	if err != nil {
		t.Fatal(err)
	}
	defer p.Stop()
	if err := p.Send("anything"); err == nil {
		t.Error("sbx login was handed input it doesn't read")
	}
}

func TestAProcessThatFailsSaysHowInDone(t *testing.T) {
	sbxtest.OnPath(t, "echo nope >&2\nexit 3\n")
	p, err := StartLogin()
	if err != nil {
		t.Fatal(err)
	}
	lines, err := collect(t, p)
	if err == nil || !strings.Contains(err.Error(), "exit status 3") {
		t.Errorf("err = %v", err)
	}
	if strings.Join(lines, "|") != "nope" {
		t.Errorf("lines = %q", lines)
	}
}

func TestAProcessIsEndedWhenStopped(t *testing.T) {
	processSbx(t)
	p, err := StartLogin()
	if err != nil {
		t.Fatal(err)
	}
	<-p.Lines() // it is running: it has printed
	p.Stop()
	if _, err := collect(t, p); err == nil {
		t.Error("a stopped process ended as if it had finished")
	}
}

// A sandbox that did not start says why: what sbx said, which is the only place the reason is.
func TestAFailedStartSaysWhatSbxSaid(t *testing.T) {
	sbxtest.OnPath(t, "case \"$1\" in\n"+
		"  start) echo 'error: the daemon is not running' >&2; exit 1 ;;\n"+
		"  exec) exit 1 ;;\n"+
		"esac\n")
	started, why := StartExplained(context.Background(), "box")
	if started {
		t.Fatal("it started")
	}
	if !strings.Contains(why, "the daemon is not running") {
		t.Errorf("why = %q", why)
	}
}

func TestAStartThatSbxAcceptedButTheSandboxNeverAnsweredSaysSo(t *testing.T) {
	old := startTimeout
	startTimeout = 1
	defer func() { startTimeout = old }()
	sbxtest.OnPath(t, "case \"$1\" in\n  start) exit 0 ;;\n  exec) exit 1 ;;\nesac\n")
	started, why := StartExplained(context.Background(), "box")
	if started || !strings.Contains(why, "didn't accept a command") {
		t.Errorf("started %v, why = %q", started, why)
	}
}

func TestAStartThatWorksNeedsNoReason(t *testing.T) {
	sbxtest.OnPath(t, "exit 0\n")
	if started, why := StartExplained(context.Background(), "box"); !started || why != "" {
		t.Errorf("started %v, why %q", started, why)
	}
}

// The write end of a process's stdin is closed when it ends, or is stopped, not left to the garbage collector.
func TestAnExecProcessesStdinIsClosedOnceItHasEnded(t *testing.T) {
	processSbx(t)
	p, err := StartExec("box", "plain")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := collect(t, p); err != nil {
		t.Fatal(err)
	}
	if err := p.stdin.Close(); err == nil {
		t.Error("the stdin pipe was still open after the process ended")
	}
}

func TestAStoppedExecProcessesStdinIsClosed(t *testing.T) {
	sbxtest.OnPath(t, "echo up\ncat >/dev/null\n")
	p, err := StartExec("box", "wait")
	if err != nil {
		t.Fatal(err)
	}
	<-p.Lines()
	p.Stop()
	collect(t, p)
	if err := p.stdin.Close(); err == nil {
		t.Error("the stdin pipe was still open after the process was stopped")
	}
}

// The Anthropic key goes to sbx's secret store on stdin, never on a command line (where anyone on the machine could read it),
// and the key sbx held before is removed first, since sbx only overwrites without asking when given the key with -t.
func TestTheAnthropicKeyIsGivenToSbxOnStdinAfterTheOldOneIsRemoved(t *testing.T) {
	dir := t.TempDir()
	log := filepath.Join(dir, "log")
	sbxtest.OnPath(t, "echo \"$*\" >>"+log+"\n"+
		"case \"$1 $2\" in\n  \"secret set\") printf 'stdin: ' >>"+log+"; cat >>"+log+"; echo >>"+log+" ;;\nesac\n")
	if err := SetAnthropicKey(context.Background(), "sk-ant-the-key"); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(log)
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	want := []string{"secret rm anthropic -f", "secret set anthropic", "stdin: sk-ant-the-key"}
	if strings.Join(lines, "|") != strings.Join(want, "|") {
		t.Errorf("sbx was asked:\n%s\nwant\n%s", strings.Join(lines, "\n"), strings.Join(want, "\n"))
	}
	for _, l := range lines[:2] {
		if strings.Contains(l, "sk-ant") {
			t.Errorf("the key is on a command line: %q", l)
		}
	}
}

func TestThereBeingNoOldKeyToRemoveIsFine(t *testing.T) {
	sbxtest.OnPath(t, "case \"$2\" in rm) echo 'no such secret' >&2; exit 1 ;; esac\ncat >/dev/null\n")
	if err := SetAnthropicKey(context.Background(), "k"); err != nil {
		t.Errorf("err = %v", err)
	}
}

func TestAKeySbxRefusesIsAnErrorThatDoesNotCarryTheKey(t *testing.T) {
	sbxtest.OnPath(t, "case \"$2\" in set) cat >/dev/null; echo 'error: invalid key sk-ant-secret-value' >&2; exit 1 ;; esac\n")
	err := SetAnthropicKey(context.Background(), "sk-ant-secret-value")
	if err == nil {
		t.Fatal("it said it worked")
	}
	if strings.Contains(err.Error(), "sk-ant-secret-value") || !strings.Contains(err.Error(), "invalid key") {
		t.Errorf("err = %v", err)
	}
}
