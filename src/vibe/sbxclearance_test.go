package main

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

// fakeRunning stands in for the sandboxes and vibes running, and the user
// asked whether to stop them.
type fakeRunning struct {
	plan    stopAllPlan
	listErr error
	stopErr error
	answer  bool
	asked   []string
	stopped []string
}

func (f *fakeRunning) work() runningWork {
	return runningWork{
		find: func() (stopAllPlan, error) { return f.plan, f.listErr },
		stop: func(p stopAllPlan) error {
			f.stopped = append(f.stopped, p.names...)
			return f.stopErr
		},
		ask: func(question string) bool {
			f.asked = append(f.asked, question)
			return f.answer
		},
	}
}

func runningPlan(names ...string) stopAllPlan {
	return stopAllPlan{names: names, sleeping: map[string]bool{}}
}

func TestClearToChangeWithNothingRunningAsksNothing(t *testing.T) {
	f := &fakeRunning{}
	ok, err := clearToChange(f.work(), "Docker SBX", false, true)
	if !ok || err != nil || len(f.asked) != 0 || len(f.stopped) != 0 {
		t.Errorf("clearToChange = %v, %v; asked %q, stopped %q; want clear, nothing asked or stopped", ok, err, f.asked, f.stopped)
	}
}

func TestClearToChangeStopsWhatRunsWhenTheUserSaysSo(t *testing.T) {
	f := &fakeRunning{plan: runningPlan("a", "b"), answer: true}
	var ok bool
	var err error
	said := stderrOf(t, func() { ok, err = clearToChange(f.work(), "Docker SBX", false, true) })
	if !ok || err != nil || len(f.asked) != 1 || !reflect.DeepEqual(f.stopped, []string{"a", "b"}) {
		t.Errorf("clearToChange = %v, %v; asked %q, stopped %q; want a and b stopped once asked", ok, err, f.asked, f.stopped)
	}
	if !strings.Contains(said, "a, b") {
		t.Errorf("said %q; want it to name what runs", said)
	}
}

func TestClearToChangeStopsNothingWhenTheUserSaysNo(t *testing.T) {
	f := &fakeRunning{plan: runningPlan("a"), answer: false}
	var ok bool
	var err error
	said := stderrOf(t, func() { ok, err = clearToChange(f.work(), "Docker SBX", false, true) })
	if ok || err != nil || len(f.stopped) != 0 {
		t.Errorf("clearToChange = %v, %v, stopped %q; want not clear, nothing stopped", ok, err, f.stopped)
	}
	if !strings.Contains(said, "vibe --stop-all") {
		t.Errorf("said %q; want it to say how to stop them and go on", said)
	}
}

// -f stops them unasked: it is only asked about when an sbx change is coming.
func TestClearToChangeWithForceStopsUnasked(t *testing.T) {
	f := &fakeRunning{plan: runningPlan("a")}
	ok, err := clearToChange(f.work(), "Docker SBX", true, false)
	if !ok || err != nil || len(f.asked) != 0 || !reflect.DeepEqual(f.stopped, []string{"a"}) {
		t.Errorf("clearToChange -f = %v, %v; asked %q, stopped %q; want a stopped unasked", ok, err, f.asked, f.stopped)
	}
}

// With no terminal to ask on, and no -f, nobody's sandboxes are stopped.
func TestClearToChangeWithNoTerminalStopsNothing(t *testing.T) {
	f := &fakeRunning{plan: runningPlan("a"), answer: true}
	var ok bool
	var err error
	said := stderrOf(t, func() { ok, err = clearToChange(f.work(), "Docker SBX", false, false) })
	if ok || err != nil || len(f.asked) != 0 || len(f.stopped) != 0 {
		t.Errorf("clearToChange with no terminal = %v, %v; asked %q, stopped %q; want not clear, nothing asked or stopped", ok, err, f.asked, f.stopped)
	}
	if !strings.Contains(said, "-f") {
		t.Errorf("said %q; want it to say -f would stop them", said)
	}
}

func TestClearToChangeSaysWhenStoppingFails(t *testing.T) {
	f := &fakeRunning{plan: runningPlan("a"), answer: true, stopErr: errors.New("could not stop: a")}
	ok, err := clearToChange(f.work(), "Docker SBX", false, true)
	if ok || err == nil || !strings.Contains(err.Error(), "could not stop: a") {
		t.Errorf("clearToChange = %v, %v; want not clear, and why", ok, err)
	}
}

func TestClearToChangeSaysWhenSbxCantBeAsked(t *testing.T) {
	f := &fakeRunning{listErr: errors.New("sbx ls broke")}
	ok, err := clearToChange(f.work(), "Docker SBX", true, true)
	if ok || err == nil || !strings.Contains(err.Error(), "sbx ls broke") || len(f.stopped) != 0 {
		t.Errorf("clearToChange = %v, %v, stopped %q; want not clear, and why", ok, err, f.stopped)
	}
}
