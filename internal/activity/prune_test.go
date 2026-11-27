package activity

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPruningTheLogRemovesTheDaysBeforeTheFirstKeptAndNothingElse(t *testing.T) {
	l := Log{Dir: t.TempDir()}
	for _, d := range []string{"2026-11-01", "2026-11-13", "2026-11-14", "2026-11-27"} {
		if err := os.MkdirAll(filepath.Join(l.Dir, d), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(l.Dir, d, "s.jsonl"), []byte("{}\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for _, other := range []string{"tickets.json", "summary-rules.md", "summaries/k.json", "2026-01-01-notes/x"} {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(l.Dir, other)), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(l.Dir, other), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := l.Prune(time.Date(2026, 11, 14, 0, 0, 0, 0, time.Local)); err != nil {
		t.Fatal(err)
	}
	for _, gone := range []string{"2026-11-01", "2026-11-13"} {
		if _, err := os.Stat(filepath.Join(l.Dir, gone)); !os.IsNotExist(err) {
			t.Fatalf("want %s pruned, got %v", gone, err)
		}
	}
	for _, kept := range []string{"2026-11-14/s.jsonl", "2026-11-27/s.jsonl", "tickets.json", "summary-rules.md", "summaries/k.json", "2026-01-01-notes/x"} {
		if _, err := os.Stat(filepath.Join(l.Dir, kept)); err != nil {
			t.Fatalf("want %s kept: %v", kept, err)
		}
	}
}

func TestPruningALogNotStartedYetIsNothingToDo(t *testing.T) {
	if err := (Log{Dir: filepath.Join(t.TempDir(), "none")}).Prune(time.Now()); err != nil {
		t.Fatalf("want no log nothing to prune, got %v", err)
	}
}

func TestPruningTheSummariesRemovesThoseMadeBeforeTheFirstKeptDayAndKeepsTheLock(t *testing.T) {
	s := &Summariser{Dir: t.TempDir()}
	first := time.Date(2026, 11, 14, 0, 0, 0, 0, time.Local)
	files := map[string]time.Time{
		"old.json":         first.Add(-time.Minute),
		"new.json":         first,
		"summarising.lock": first.AddDate(0, -1, 0),
	}
	for name, made := range files {
		path := filepath.Join(s.Dir, name)
		if err := os.WriteFile(path, []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, made, made); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Prune(first); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(s.Dir, "old.json")); !os.IsNotExist(err) {
		t.Fatalf("want the old summary pruned, got %v", err)
	}
	for _, kept := range []string{"new.json", "summarising.lock"} {
		if _, err := os.Stat(filepath.Join(s.Dir, kept)); err != nil {
			t.Fatalf("want %s kept: %v", kept, err)
		}
	}
	if err := (&Summariser{Dir: filepath.Join(t.TempDir(), "none")}).Prune(first); err != nil {
		t.Fatalf("want no summaries nothing to prune, got %v", err)
	}
}

func TestTheFirstDayKeptIsKeepDaysBackCountingToday(t *testing.T) {
	today := time.Date(2026, 11, 27, 15, 4, 0, 0, time.Local)
	if got, want := FirstKept(today), time.Date(2026, 11, 14, 0, 0, 0, 0, time.Local); !got.Equal(want) {
		t.Fatalf("want %v, got %v", want, got)
	}
}
