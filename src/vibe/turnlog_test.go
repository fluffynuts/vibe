package main

import (
	"bytes"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"vibe/internal/fscopy"
)

func logFile(t *testing.T) string {
	return filepath.Join(t.TempDir(), "box.turns.log")
}

func loggedTurns(t *testing.T, file string) string {
	t.Helper()
	data, err := fscopy.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestATurnIsLoggedWithWhatTheAgentSaidAndHowItEnded(t *testing.T) {
	file := logFile(t)
	l := newTurnLog(file)
	l.begin("sandbox box")
	l.line([]byte(`{"type":"system"}` + "\n"))
	l.line([]byte(`{"type":"result"}`))
	l.end(nil, false)
	got := loggedTurns(t, file)
	for _, want := range []string{turnMark, "sandbox box", `{"type":"system"}`, `{"type":"result"}`, "[ended]"} {
		if !strings.Contains(got, want) {
			t.Errorf("log lacks %q:\n%s", want, got)
		}
	}
}

func TestAFailedTurnShowsWhatTheAgentSaidOnStderr(t *testing.T) {
	file := logFile(t)
	l := newTurnLog(file)
	l.begin("x")
	l.end(errors.New("exit status 1: ActionRequiredError: Named models unavailable"), false)
	if got := loggedTurns(t, file); !strings.Contains(got, "[failed: exit status 1: ActionRequiredError: Named models unavailable]") {
		t.Errorf("log:\n%s", got)
	}
}

func TestAStoppedTurnSaysSo(t *testing.T) {
	file := logFile(t)
	l := newTurnLog(file)
	l.begin("x")
	l.end(errors.New("signal: killed"), true)
	got := loggedTurns(t, file)
	if !strings.Contains(got, "[stopped from the page]") || strings.Contains(got, "killed") {
		t.Errorf("log:\n%s", got)
	}
}

func TestATurnThatNeverEndsIsLoggedAsFarAsItGot(t *testing.T) {
	file := logFile(t)
	newTurnLog(file).begin("sandbox box")
	if got := loggedTurns(t, file); !strings.Contains(got, "sandbox box") || strings.Contains(got, "[ended]") {
		t.Errorf("log:\n%s", got)
	}
}

func TestTurnsAreKeptTogetherOldestFirst(t *testing.T) {
	file := logFile(t)
	for _, what := range []string{"first", "second"} {
		l := newTurnLog(file)
		l.begin(what)
		l.end(nil, false)
	}
	got := loggedTurns(t, file)
	if strings.Count(got, turnMark) != 2 || strings.Index(got, "first") > strings.Index(got, "second") {
		t.Errorf("log:\n%s", got)
	}
}

func TestALongLineIsCutAndSaysHowMuchWasLeftOut(t *testing.T) {
	file := logFile(t)
	l := newTurnLog(file)
	l.begin("x")
	l.line([]byte(strings.Repeat("a", turnLogLine+500)))
	l.end(nil, false)
	if got := loggedTurns(t, file); !strings.Contains(got, "(+500 more bytes)") || strings.Contains(got, strings.Repeat("a", turnLogLine+1)) {
		t.Errorf("log has a line of the wrong length")
	}
}

func TestTheLogKeepsWithinItsBoundByLosingWholeOldTurnsNeverTheLatest(t *testing.T) {
	file := logFile(t)
	big := strings.Repeat("x", 60*1024)
	for i := 0; i < 10; i++ {
		l := newTurnLog(file)
		l.begin("turn-number-" + string(rune('a'+i)))
		for j := 0; j < 15; j++ {
			l.line([]byte(big[:turnLogLine]))
		}
		l.end(nil, false)
	}
	got := loggedTurns(t, file)
	if len(got) > turnLogKept {
		t.Errorf("log is %d bytes, over %d", len(got), turnLogKept)
	}
	if !strings.Contains(got, "turn-number-j") || strings.Contains(got, "turn-number-a") {
		t.Errorf("wrong turns kept")
	}
	if !strings.HasPrefix(got, turnMark) {
		t.Errorf("starts part-way through a turn: %.60q", got)
	}
}

func TestAReaderNeverSeesAPartLog(t *testing.T) {
	file := logFile(t)
	l := newTurnLog(file)
	l.begin("x")
	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			if data, err := fscopy.ReadFile(file); err == nil && len(data) > 0 && !bytes.HasPrefix(data, []byte(turnMark)) {
				t.Errorf("read a part of the log: %.40q", data)
				return
			}
		}
	}()
	for i := 0; i < 200; i++ {
		l.line([]byte("line"))
		if i%10 == 0 {
			l.end(nil, false)
		}
	}
	close(stop)
	wg.Wait()
}

func TestTurnLogSaysWhatToDoWhenNothingHasBeenLogged(t *testing.T) {
	var out bytes.Buffer
	err := doTurnLog(&out, t.TempDir(), "box")
	if err == nil || !strings.Contains(err.Error(), "send the agent a prompt") || out.Len() != 0 {
		t.Errorf("err = %v, out = %q", err, out.String())
	}
}
