package main

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"vibe/internal/fscopy"
	"vibe/internal/state"
)

const (
	// turnLogKept bounds the log: the last turns that fit, whole.
	turnLogKept = 384 * 1024
	// turnLogLine bounds one line of the agent's output in the log; a tool's
	// result can be huge, and what is wrong is nearly always in the first of it.
	turnLogLine = 4000
	turnMark    = "=== turn "
)

// turnLog keeps what an agent said in its last turns, as plain text in file,
// so a person can say what happened without remembering to copy anything:
// 'vibe --turn-log' shows it. It is written whole at the start of a turn and
// at its end (never appended to: see fscopy.WriteWhole), so a turn that never
// ends shows as far as it had got when it began.
type turnLog struct {
	file string
	mu   sync.Mutex
	// before is the turns already kept, now their own.
	before string
	turn   strings.Builder
	began  time.Time
}

func newTurnLog(file string) *turnLog {
	return &turnLog{file: file}
}

// begin starts a turn's entry, saying how the agent was run.
func (l *turnLog) begin(what string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if kept, err := fscopy.ReadFile(l.file); err == nil {
		l.before = string(kept)
	}
	l.began = time.Now()
	l.turn.Reset()
	fmt.Fprintf(&l.turn, "%s%s\n%s\n", turnMark, l.began.Format(time.RFC3339), what)
	l.write()
}

// line adds a line of the agent's output, as it came.
func (l *turnLog) line(raw []byte) {
	l.mu.Lock()
	defer l.mu.Unlock()
	text := strings.TrimRight(string(raw), "\r\n")
	if len(text) > turnLogLine {
		text = fmt.Sprintf("%s … (+%d more bytes)", text[:turnLogLine], len(text)-turnLogLine)
	}
	fmt.Fprintf(&l.turn, "+%.1fs  %s\n", time.Since(l.began).Seconds(), text)
}

// end says how the turn ended: err is what sbx exec gave, which carries the
// agent's stderr when it failed; stopped is the page's Stop, or the next
// prompt.
func (l *turnLog) end(err error, stopped bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	switch {
	case stopped:
		fmt.Fprintf(&l.turn, "+%.1fs  [stopped from the page]\n", time.Since(l.began).Seconds())
	case err != nil:
		fmt.Fprintf(&l.turn, "+%.1fs  [failed: %v]\n", time.Since(l.began).Seconds(), err)
	default:
		fmt.Fprintf(&l.turn, "+%.1fs  [ended]\n", time.Since(l.began).Seconds())
	}
	l.write()
}

// write puts the log, with this turn, in file: the oldest turns go to stay
// within turnLogKept, but this one never does.
func (l *turnLog) write() {
	before := l.before
	for room := turnLogKept - l.turn.Len(); len(before) > room; {
		next := strings.Index(before[1:], "\n"+turnMark)
		if next < 0 {
			before = ""
			break
		}
		before = before[next+2:]
	}
	fscopy.WriteWhole(l.file, []byte(before+l.turn.String()))
}

// readTurnLog is the log in file, or what to do when there is none.
func readTurnLog(file string) (string, error) {
	data, err := fscopy.ReadFile(file)
	if os.IsNotExist(err) {
		return "", fmt.Errorf("nothing has been logged yet: send the agent a prompt from the page's Conversation tab first")
	}
	return string(data), err
}

// doTurnLog is --turn-log: the log of sandbox name's agent's last turns.
func doTurnLog(out io.Writer, vibeHome, name string) error {
	log, err := readTurnLog(state.TurnLogPath(vibeHome, name))
	if err != nil {
		return err
	}
	_, err = io.WriteString(out, log)
	return err
}
