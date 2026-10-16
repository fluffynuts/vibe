package review

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// The agent reads the review from the view folder, mounted read-only into
// the sandbox: the review itself is the store's alone.
func TestTheReviewIsCopiedToTheViewForTheAgent(t *testing.T) {
	s := NewStore(t.TempDir())
	if _, err := s.Do(Request{Op: "start", Reviewing: true, Note: "seen"}, User); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(s.View(), "review.json"))
	if err != nil {
		t.Fatal(err)
	}
	var seen Review
	if err := json.Unmarshal(data, &seen); err != nil || seen.Note != "seen" {
		t.Errorf("the view has %+v (%v)", seen, err)
	}
}

func TestAReviewKeptBeforeTheViewIsCopiedThereWhenTakenIn(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "review.json"), []byte(`{"note": "older", "threads": []}`), 0o600); err != nil {
		t.Fatal(err)
	}
	s := NewStore(dir)
	if _, err := s.Take(); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(filepath.Join(s.View(), "review.json")); err != nil || !strings.Contains(string(data), "older") {
		t.Errorf("view = %s (%v)", data, err)
	}
}

func TestSavingDoesntFollowALinkLeftInTheStore(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symbolic links need privileges on Windows")
	}
	dir := t.TempDir()
	victim := filepath.Join(t.TempDir(), "victim")
	if err := os.WriteFile(victim, []byte("mine"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"review.json.tmp", filepath.Join("view", "review.json.tmp")} {
		os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0o755)
		if err := os.Symlink(victim, filepath.Join(dir, name)); err != nil {
			t.Fatal(err)
		}
	}
	s := NewStore(dir)
	if _, err := s.Do(Request{Op: "start"}, User); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(victim); string(data) != "mine" {
		t.Errorf("the linked file was written: %q", data)
	}
}

func TestAnInboxRequestTooLargeIsntRead(t *testing.T) {
	s := NewStore(t.TempDir())
	drop(t, s, "1.json", Request{Op: "general", Body: strings.Repeat("x", maxRequest)})
	r, err := s.Take()
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Threads) != 0 || len(r.Problems) != 1 || !strings.Contains(r.Problems[0], "too large") {
		t.Errorf("threads %d, problems %q", len(r.Threads), r.Problems)
	}
}

// A start whose old review can't be kept is a disk problem, not the
// request's: it isn't Refused.
func TestAStartThatCantKeepTheLastReviewIsntRefused(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)
	s.Do(Request{Op: "general", Body: "kept"}, Agent)
	os.WriteFile(filepath.Join(dir, "history"), []byte("not a folder"), 0o600)
	_, err := s.Do(Request{Op: "start"}, User)
	if err == nil || errors.As(err, &Refused{}) {
		t.Errorf("err = %v", err)
	}
}

// The agent's requests are taken in a batch at a time: the rest wait for
// the next time.
func TestTheInboxIsTakenInABatchAtATime(t *testing.T) {
	s := NewStore(t.TempDir())
	for i := 0; i < maxTakeAtOnce+5; i++ {
		drop(t, s, fmt.Sprintf("%04d.json", i), Request{Op: "general", Body: "x"})
	}
	r, err := s.Take()
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Threads) != maxTakeAtOnce {
		t.Errorf("took %d", len(r.Threads))
	}
	if r, _ = s.Take(); len(r.Threads) != maxTakeAtOnce+5 {
		t.Errorf("then %d", len(r.Threads))
	}
}

func TestACommentTooLongIsRefused(t *testing.T) {
	s := NewStore(t.TempDir())
	drop(t, s, "1.json", Request{Op: "general", Body: strings.Repeat("x", maxBody+1)})
	r, _ := s.Take()
	if len(r.Threads) != 0 || len(r.Problems) != 1 || !strings.Contains(r.Problems[0], "too long") {
		t.Errorf("threads %d, problems %q", len(r.Threads), r.Problems)
	}
}

// A review too large to load says which file, and what to do.
func TestAReviewTooLargeToLoadSaysWhatToDo(t *testing.T) {
	was := maxReview
	maxReview = 100
	defer func() { maxReview = was }()
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "review.json"), []byte(`{"note": "`+strings.Repeat("x", 200)+`", "threads": []}`), 0o600)
	_, err := NewStore(dir).Load()
	if err == nil || !strings.Contains(err.Error(), "review.json") || !strings.Contains(err.Error(), "move it") {
		t.Errorf("err = %v", err)
	}
}
