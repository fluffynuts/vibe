package sbxrun

import (
	"context"
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
