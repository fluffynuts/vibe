package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"vibe/internal/fscopy"
)

func planAt(t *testing.T, file string, now *time.Time) *cursorPlan {
	t.Helper()
	p := newCursorPlan(file)
	p.now = func() time.Time { return *now }
	return p
}

func TestNothingIsKnownOfThePlanUntilAModelIsRefused(t *testing.T) {
	now := time.Date(2026, 10, 27, 9, 0, 0, 0, time.UTC)
	if _, only := planAt(t, filepath.Join(t.TempDir(), "plan.json"), &now).onlyAuto(); only {
		t.Error("the plan was said to run only Auto before anything was learned")
	}
}

func TestAModelRefusedIsRememberedAcrossRunsOfVibeWithWhenItWas(t *testing.T) {
	file := filepath.Join(t.TempDir(), "plan.json")
	now := time.Date(2026, 10, 27, 9, 0, 0, 0, time.UTC)
	if err := planAt(t, file, &now).learnedOnlyAuto(); err != nil {
		t.Fatal(err)
	}
	now = now.Add(10 * time.Minute)
	since, only := planAt(t, file, &now).onlyAuto()
	if !only || !since.Equal(time.Date(2026, 10, 27, 9, 0, 0, 0, time.UTC)) {
		t.Errorf("since %v, only %v", since, only)
	}
}

func TestWhatWasLearnedIsNotBelievedForeverAfterAnUpgrade(t *testing.T) {
	file := filepath.Join(t.TempDir(), "plan.json")
	now := time.Date(2026, 10, 27, 9, 0, 0, 0, time.UTC)
	p := planAt(t, file, &now)
	p.learnedOnlyAuto()
	now = now.Add(cursorPlanFreshFor - time.Second)
	if _, only := p.onlyAuto(); !only {
		t.Error("forgotten before the hour was up")
	}
	now = now.Add(2 * time.Second)
	if _, only := p.onlyAuto(); only {
		t.Error("still believed after the hour")
	}
}

func TestForgettingTheLimitIsRememberedToo(t *testing.T) {
	file := filepath.Join(t.TempDir(), "plan.json")
	now := time.Date(2026, 10, 27, 9, 0, 0, 0, time.UTC)
	p := planAt(t, file, &now)
	p.learnedOnlyAuto()
	if err := p.forget(); err != nil {
		t.Fatal(err)
	}
	if _, only := planAt(t, file, &now).onlyAuto(); only {
		t.Error("the limit came back from the file")
	}
	if err := p.forget(); err != nil {
		t.Errorf("forgetting what was already forgotten is an error: %v", err)
	}
}

func TestAFileThatCantBeReadIsNotTakenForALimit(t *testing.T) {
	file := filepath.Join(t.TempDir(), "plan.json")
	os.WriteFile(file, []byte("{not json"), 0o600)
	now := time.Date(2026, 10, 27, 9, 0, 0, 0, time.UTC)
	if _, only := planAt(t, file, &now).onlyAuto(); only {
		t.Error("a broken file was taken for a limit")
	}
}

func TestAReaderOfTheFileNeverSeesAPartOfIt(t *testing.T) {
	file := filepath.Join(t.TempDir(), "plan.json")
	now := time.Date(2026, 10, 27, 9, 0, 0, 0, time.UTC)
	p := planAt(t, file, &now)
	p.learnedOnlyAuto()
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
			if data, err := fscopy.ReadFile(file); err == nil && len(data) > 0 && !json.Valid(data) {
				t.Errorf("read a part of the file: %q", data)
				return
			}
		}
	}()
	for i := 0; i < 200; i++ {
		p.learnedOnlyAuto()
		if i%7 == 0 {
			p.forget()
		}
	}
	close(stop)
	wg.Wait()
}
