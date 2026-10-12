package sbxrun

import (
	"context"
	"os/exec"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"
)

// TestStartGivesUpOnASandboxItCannotBoot checks Start's failure path: with no
// such sandbox (or no sbx at all) it reports false promptly rather than
// waiting out startTimeout on something that is never going to answer.
func TestStartGivesUpOnASandboxItCannotBoot(t *testing.T) {
	done := make(chan bool, 1)
	go func() { done <- Start("vibe-test-no-such-sandbox-3f9c1") }()
	select {
	case started := <-done:
		if started {
			t.Error("Start reported a nonexistent sandbox as running")
		}
	case <-time.After(30 * time.Second):
		t.Fatal("Start hung on a sandbox that cannot boot")
	}
}

func TestParseAgents(t *testing.T) {
	help := "Omit the path to mount the current directory.\n\n" +
		"Available agents: claude, codex, copilot, cursor, devin, docker-agent, droid, gemini, kiro, opencode, shell\n\n" +
		"With --cloud:\n"
	want := []string{"claude", "codex", "copilot", "cursor", "devin", "docker-agent", "droid", "gemini", "kiro", "opencode", "shell"}
	if got := ParseAgents(help); !reflect.DeepEqual(got, want) {
		t.Errorf("ParseAgents = %v, want %v", got, want)
	}
	if got := ParseAgents("no agents here\n"); got != nil {
		t.Errorf("ParseAgents of help without the line = %v", got)
	}
}

func TestStreamLinesHandsOverEachLineAndFeedsStdin(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("wants sh")
	}
	var got []string
	cmd := exec.Command("sh", "-c", "read first; echo \"one $first\"; echo two; printf three")
	err := streamLines(cmd, "typed\n", func(l []byte) { got = append(got, string(l)) })
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"one typed", "two", "three"}; !reflect.DeepEqual(got, want) {
		t.Errorf("lines = %q, want %q", got, want)
	}
}

func TestStreamLinesReportsWhatAFailedCommandSaid(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("wants sh")
	}
	var got []string
	cmd := exec.Command("sh", "-c", "echo before; echo oops >&2; exit 3")
	err := streamLines(cmd, "", func(l []byte) { got = append(got, string(l)) })
	if err == nil || !strings.Contains(err.Error(), "oops") || !strings.Contains(err.Error(), "exit status 3") {
		t.Errorf("err = %v", err)
	}
	if !reflect.DeepEqual(got, []string{"before"}) {
		t.Errorf("lines = %q", got)
	}
}

func TestStreamLinesDropsWhatIsPastTheLimitButKeepsGoing(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("wants sh")
	}
	var lens []int
	cmd := exec.Command("sh", "-c", "head -c 1100000 /dev/zero | tr '\\0' a; echo; echo next")
	if err := streamLines(cmd, "", func(l []byte) { lens = append(lens, len(l)) }); err != nil {
		t.Fatal(err)
	}
	if len(lens) != 2 || lens[0] > maxStreamLine || lens[1] != 4 {
		t.Errorf("line lengths = %v", lens)
	}
}

func TestStreamLinesEndsWhenItsContextIsCancelled(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("wants sh")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, "sh", "-c", "echo started; sleep 30")
	done := make(chan error, 1)
	started := make(chan struct{})
	go func() {
		done <- streamLines(cmd, "", func([]byte) { close(started) })
	}()
	<-started
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Error("a killed command reported success")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("cancelling did not end the stream")
	}
}

func TestTailKeepsTheEnd(t *testing.T) {
	var tl tail
	tl.Write([]byte(strings.Repeat("a", tailSize)))
	tl.Write([]byte("end"))
	if got := tl.String(); len(got) != tailSize || !strings.HasSuffix(got, "aend") {
		t.Errorf("tail = %d bytes ending %q", len(got), got[len(got)-4:])
	}
}
