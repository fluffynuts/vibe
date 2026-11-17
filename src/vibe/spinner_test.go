package main

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

// lockedBuffer is a strings.Builder safe to write from the spinner's goroutine.
type lockedBuffer struct {
	mu sync.Mutex
	b  strings.Builder
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

// On a terminal the line turns with every tick while the work runs, and is
// wiped when it ends, leaving the line for what is said next.
func TestSpinWhileTurnsOnATerminalAndWipesItsLine(t *testing.T) {
	var out lockedBuffer
	ticks := make(chan time.Time)
	err := spinWhile(&out, true, ticks, "Backing up ~/.vibe", func() error {
		ticks <- time.Time{} // unbuffered: each is taken before the next is sent
		ticks <- time.Time{}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	want := "\r\x1b[Kvibe: ⠋ Backing up ~/.vibe" +
		"\r\x1b[Kvibe: ⠙ Backing up ~/.vibe" +
		"\r\x1b[Kvibe: ⠹ Backing up ~/.vibe" +
		"\r\x1b[K"
	if got := out.String(); got != want {
		t.Errorf("spinWhile drew %q, want %q", got, want)
	}
}

// Off a terminal there is nothing to animate: the line is said once, and the
// work's error comes back as it was.
func TestSpinWhileSaysItOnceOffATerminal(t *testing.T) {
	var out lockedBuffer
	ticks := make(chan time.Time)
	broke := errors.New("broke")
	err := spinWhile(&out, false, ticks, "Backing up ~/.vibe", func() error {
		select {
		case ticks <- time.Time{}:
			t.Error("a tick was taken off a terminal")
		default:
		}
		return broke
	})
	if err != broke {
		t.Errorf("spinWhile = %v, want the work's error", err)
	}
	if got := out.String(); got != "vibe: Backing up ~/.vibe\n" {
		t.Errorf("spinWhile said %q off a terminal", got)
	}
}
