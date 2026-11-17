package main

import (
	"fmt"
	"io"
	"os"
	"time"

	"vibe/internal/prompt"
)

// spinning runs work with label shown on stderr, turning a spinner on a
// terminal so a slow step never looks stuck.
func spinning(label string, work func() error) error {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	return spinWhile(os.Stderr, prompt.IsTerminal(os.Stderr), ticker.C, label, work)
}

// spinWhile runs work with label shown on out: on a terminal (tty), as a
// line whose spinner turns with each tick, wiped once work ends; elsewhere,
// said once. It returns work's error.
func spinWhile(out io.Writer, tty bool, ticks <-chan time.Time, label string, work func() error) error {
	if !tty {
		fmt.Fprintf(out, "vibe: %s\n", label)
		return work()
	}
	draw := func(frame int) {
		fmt.Fprintf(out, "\r\x1b[Kvibe: %s %s", spinnerFrames[frame%len(spinnerFrames)], label)
	}
	draw(0)
	stop, stopped := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(stopped)
		for frame := 1; ; frame++ {
			select {
			case <-stop:
				return
			case <-ticks:
				draw(frame)
			}
		}
	}()
	err := work()
	close(stop)
	<-stopped
	fmt.Fprint(out, "\r\x1b[K")
	return err
}
