package main

import (
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"vibe/internal/prompt"
	"vibe/internal/running"
	"vibe/internal/sbxrun"
	"vibe/internal/state"
)

// maxParallelStops bounds how many "sbx stop" run at once: stopping is
// mostly waiting, but a machine with dozens of sandboxes shouldn't be asked
// to shut them all down in the same instant.
const maxParallelStops = 8

// --- stop all ---------------------------------------------------------------

func doStopAll(vibeHome string, force bool) error {
	statuses, err := sbxrun.List()
	if err != nil {
		return err
	}
	records, err := state.List(vibeHome)
	if err != nil {
		return err
	}
	plan := planStopAll(statuses, records, func(target string) []running.Claim { return running.Claims(vibeHome, target) })
	if len(plan.names) == 0 {
		note("no running sandboxes — nothing to stop")
		return nil
	}
	if !force {
		listed := make([]string, len(plan.names))
		for i, n := range plan.names {
			listed[i] = n
			if plan.sleeping[n] {
				listed[i] += " (sleeping, with a vibe attached)"
			}
		}
		note("running sandboxes: %s", strings.Join(listed, ", "))
	}
	if !confirm(force, "Are you sure you want to stop all running sandboxes, and the vibes attached to them?") {
		note("aborted — nothing stopped")
		return nil
	}
	var failed []string
	ended := map[string]bool{}
	for _, name := range plan.names {
		if err := endVibes(vibeHome, plan.vibes[name], "sandbox '"+name+"' was left running"); err != nil {
			note("%s", err)
			failed = append(failed, name)
			continue
		}
		ended[name] = true
	}
	// A sleeping sandbox's vibe wakes it to save its agent's memories and
	// stops it again, unless it was killed part-way: sbx says which are
	// running now.
	if len(failed) < len(plan.names) {
		if statuses, err = sbxrun.List(); err != nil {
			return err
		}
	}
	var names []string
	for _, s := range statuses {
		if s.Running && ended[s.Name] {
			names = append(names, s.Name)
		}
	}
	sort.Strings(names)
	failed = append(failed, stopSandboxes(os.Stderr, prompt.IsTerminal(os.Stderr), names, sbxrun.StopQuiet)...)
	if len(failed) > 0 {
		sort.Strings(failed)
		return fmt.Errorf("could not stop: %s", strings.Join(failed, ", "))
	}
	return nil
}

// stopAllPlan is what --stop-all stops: the names of the sandboxes, sorted,
// which of them are sleeping, and the vibes attached to each.
type stopAllPlan struct {
	names    []string
	sleeping map[string]bool
	vibes    map[string][]running.Claim
}

// planStopAll is every running sandbox, and every sleeping one: one sbx
// says is stopped (as it does of one left idle) with a vibe still
// attached, waiting for it. claims are the vibes' claims on a folder.
func planStopAll(statuses []sbxrun.Status, records []state.Instance, claims func(target string) []running.Claim) stopAllPlan {
	plan := stopAllPlan{sleeping: map[string]bool{}, vibes: map[string][]running.Claim{}}
	targets := map[string]string{}
	for _, r := range records {
		targets[r.Name] = r.Target
	}
	for _, s := range statuses {
		var vibes []running.Claim
		if target, ok := targets[s.Name]; ok {
			vibes = sessions(claims(target))
		}
		if !s.Running && len(vibes) == 0 {
			continue
		}
		plan.names = append(plan.names, s.Name)
		plan.sleeping[s.Name] = s.State == "stopped"
		if len(vibes) > 0 {
			plan.vibes[s.Name] = vibes
		}
	}
	sort.Strings(plan.names)
	return plan
}

type stopPhase int

const (
	stopQueued stopPhase = iota
	stopStopping
	stopStopped
	stopFailed
)

type stopRow struct {
	name  string
	phase stopPhase
	err   error
}

// stopSandboxes stops every named sandbox, up to maxParallelStops at a time,
// showing each one's state as it changes, and returns the names of those that
// failed. On a terminal the rows are redrawn in place (with a spinner while
// any is stopping); anywhere else each change is printed as a line of its own.
//
// stop is sbxrun.StopQuiet in production, and a stub under test.
func stopSandboxes(w io.Writer, tty bool, names []string, stop func(name string) error) []string {
	rows := make([]*stopRow, len(names))
	width := 0
	for i, n := range names {
		rows[i] = &stopRow{name: n}
		width = max(width, len(n))
	}

	var mu sync.Mutex
	frame := 0
	drawn := false
	spinner := []string{"|", "/", "-", "\\"}

	text := func(r *stopRow) string {
		switch r.phase {
		case stopStopping:
			if tty {
				return "Stopping " + spinner[frame%len(spinner)]
			}
			return "Stopping"
		case stopStopped:
			return "Stopped"
		case stopFailed:
			return "FAILED: " + oneLine(r.err.Error())
		}
		return "Waiting"
	}
	// render needs mu held.
	render := func() {
		if !tty {
			return
		}
		if drawn {
			fmt.Fprintf(w, "\x1b[%dA", len(rows))
		}
		for _, r := range rows {
			fmt.Fprintf(w, "\r\x1b[2Kvibe: %-*s  %s\n", width, r.name, text(r))
		}
		drawn = true
	}
	// set needs mu held.
	set := func(r *stopRow, phase stopPhase, err error) {
		r.phase, r.err = phase, err
		if tty {
			render()
		} else if phase != stopQueued {
			fmt.Fprintf(w, "vibe: %-*s  %s\n", width, r.name, text(r))
		}
	}

	mu.Lock()
	render()
	mu.Unlock()

	done := make(chan struct{})
	var ticker sync.WaitGroup
	if tty {
		ticker.Add(1)
		go func() {
			defer ticker.Done()
			t := time.NewTicker(120 * time.Millisecond)
			defer t.Stop()
			for {
				select {
				case <-done:
					return
				case <-t.C:
					mu.Lock()
					frame++
					render()
					mu.Unlock()
				}
			}
		}()
	}

	sem := make(chan struct{}, maxParallelStops)
	var wg sync.WaitGroup
	for _, r := range rows {
		wg.Add(1)
		go func(r *stopRow) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			mu.Lock()
			set(r, stopStopping, nil)
			mu.Unlock()
			err := stop(r.name)
			mu.Lock()
			if err != nil {
				set(r, stopFailed, err)
			} else {
				set(r, stopStopped, nil)
			}
			mu.Unlock()
		}(r)
	}
	wg.Wait()
	close(done)
	ticker.Wait()

	var failed []string
	for _, r := range rows {
		if r.phase == stopFailed {
			failed = append(failed, r.name)
		}
	}
	return failed
}

// oneLine squeezes sbx's possibly multi-line error onto a single row.
func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
