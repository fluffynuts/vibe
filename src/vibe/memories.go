package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"time"

	agentmem "vibe/internal/memory"
	"vibe/internal/state"
)

// saveMemoriesOnExit copies the agent's memories back out to the sandbox's
// host store once a session ends, so the host copy tracks the sandbox rather
// than only being refreshed by a --re-init.
func saveMemoriesOnExit(vibeHome, name string) {
	saveMemories(context.Background(), vibeHome, name)
}

// saveMemories is saveMemoriesOnExit, given up when ctx ends (the companion
// page's Cancel): the store is put back as it was, as for a quit.
func saveMemories(parent context.Context, vibeHome, name string) {
	inst, found, err := state.Load(vibeHome, name)
	if err != nil || !found {
		return
	}
	store := inst.MemoryStore
	if store == "" {
		// Records written before the store was remembered: the default
		// location is the one such a sandbox will have had mounted.
		store = agentmem.StoreFor(vibeHome, "", name)
		if info, err := os.Stat(store); err != nil || !info.IsDir() {
			return
		}
	}
	status := newStatusLine()
	snap, err := agentmem.TakeSnapshot(store)
	if err != nil {
		status.done("Memories not backed up: could not first keep a copy of the ones in %s: %s", store, err)
		return
	}

	// Ctrl-C is how a session is left, so it is easily still being pressed
	// once the backup has started. Rather than dying half-way through the
	// copy, vibe asks; quitting puts back what the store held before.
	interrupts := make(chan os.Signal, 1)
	signal.Notify(interrupts, os.Interrupt)
	defer signal.Stop(interrupts)
	ctx, cancel := context.WithCancel(parent)
	defer cancel()

	type outcome struct {
		saved bool
		err   error
	}
	finished := make(chan outcome, 1)
	status.show("Backing up memories to %s", store)
	session.Report("backing up its agent's memories")
	go func() {
		saved, err := agentmem.Backup(ctx, name, store)
		finished <- outcome{saved, err}
	}()

	for {
		select {
		case o := <-finished:
			if parent.Err() != nil {
				// Cancelled part-way: the store goes back to what it held,
				// as for a quit, rather than staying half-copied.
				if err := snap.Restore(); err != nil {
					note("Memory backup cancelled, and the original local memories could not be restored: %s", err)
					return
				}
				note("Memory backup cancelled; the original local memories are back as they were")
				return
			}
			snap.Discard()
			reportMemoryBackup(status, store, o.saved, o.err)
			return
		case <-interrupts:
			if status.tty {
				fmt.Fprint(os.Stderr, "\r\x1b[2K")
			}
			if confirmQuitDuringBackup(interrupts) {
				cancel()
				<-finished
				if err := snap.Restore(); err != nil {
					note("Could not restore the original local memories: %s", err)
					return
				}
				note("Original local memories restored")
				return
			}
			select {
			case o := <-finished:
				snap.Discard()
				if o.saved {
					note("Memories were saved to %s", store)
				} else {
					reportMemoryBackup(status, store, o.saved, o.err)
				}
				return
			default:
				status.show("Backing up memories to %s", store)
			}
		}
	}
}

func reportMemoryBackup(status statusLine, store string, saved bool, err error) {
	switch {
	case err != nil:
		status.done("Backup of memories failed: %s", err)
	case saved:
		status.done("Backed up memories to %s", store)
	default:
		status.done("No memories to back up to %s", store)
	}
}

// confirmQuitDuringBackup asks whether to abandon the memory backup,
// defaulting to no. Further Ctrl-Cs while it waits only ask again: whoever
// is still pressing the key that left the sandbox hasn't read the question
// yet. With no terminal to ask on, the interrupt came from somewhere that
// meant it, and is taken as a yes.
func confirmQuitDuringBackup(interrupts <-chan os.Signal) bool {
	reader, closeFn, ok := ttyReader()
	if !ok {
		return true
	}
	defer closeFn()
	ask := func() {
		fmt.Fprint(os.Stderr, "vibe: vibe is currently exporting memories from the sandbox - are you sure you want to quit? [y/N] ")
	}
	type answer struct {
		line string
		err  error
	}
	answers := make(chan answer, 1)
	read := func() {
		go func() {
			line, err := reader.ReadString('\n')
			answers <- answer{line, err}
		}()
	}

	ask()
	read()
	for {
		select {
		case <-interrupts:
			fmt.Fprintln(os.Stderr)
			ask()
		case a := <-answers:
			if a.err != nil && a.line == "" {
				// A Windows console read ends on Ctrl-C rather than
				// carrying on; if that is what ended this one, ask again.
				select {
				case <-interrupts:
					fmt.Fprintln(os.Stderr)
					ask()
					read()
					continue
				case <-time.After(100 * time.Millisecond):
					return false
				}
			}
			switch strings.ToLower(strings.TrimSpace(a.line)) {
			case "y", "yes":
				return true
			}
			return false
		}
	}
}
