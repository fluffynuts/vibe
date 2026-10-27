package main

import (
	"encoding/json"
	"errors"
	"os"
	"sync"
	"time"

	"vibe/internal/fscopy"
)

// cursorPlanFreshFor is how long what was learned of the plan is believed: a
// user may upgrade, and vibe can't read the plan when Cursor is given an API
// key, as a sandbox's is. After this the models are offered again, and the
// next one chosen finds out afresh.
const cursorPlanFreshFor = time.Hour

// cursorPlan is what vibe has learned of what a sandbox's Cursor plan can run:
// that it refused a model other than Auto (a free plan only runs Auto), and
// when, kept in file so the page still knows after vibe is restarted. It is
// learned from the refusal, which is the only place Cursor says so; it is
// forgotten when it is old, when the user says they have upgraded, and when a
// model other than Auto runs.
type cursorPlan struct {
	file string
	// now is the clock (time.Now when nil), for tests to stand in for.
	now func() time.Time
	mu  sync.Mutex
}

func newCursorPlan(file string) *cursorPlan {
	return &cursorPlan{file: file}
}

type cursorPlanFile struct {
	OnlyAuto bool      `json:"onlyAuto"`
	At       time.Time `json:"at"`
}

func (p *cursorPlan) clock() time.Time {
	if p.now != nil {
		return p.now()
	}
	return time.Now()
}

// learnedOnlyAuto records that a model other than Auto was just refused.
func (p *cursorPlan) learnedOnlyAuto() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	data, err := json.Marshal(cursorPlanFile{OnlyAuto: true, At: p.clock()})
	if err != nil {
		return err
	}
	return fscopy.WriteWhole(p.file, data)
}

// forget takes away what was learned, if anything was.
func (p *cursorPlan) forget() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := os.Remove(p.file); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// onlyAuto says the plan is believed to run only Auto, and since when. A
// file that can't be read is said (it is only a memory) and is not a limit.
func (p *cursorPlan) onlyAuto() (since time.Time, only bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	data, err := fscopy.ReadFile(p.file)
	if errors.Is(err, os.ErrNotExist) {
		return time.Time{}, false
	}
	var kept cursorPlanFile
	if err == nil {
		err = json.Unmarshal(data, &kept)
	}
	if err != nil {
		note("what was learned of Cursor's plan can't be read from %s (%v): taken as not known", p.file, err)
		return time.Time{}, false
	}
	if !kept.OnlyAuto || p.clock().Sub(kept.At) >= cursorPlanFreshFor {
		return time.Time{}, false
	}
	return kept.At, true
}
