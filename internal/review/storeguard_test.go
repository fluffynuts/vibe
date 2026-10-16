package review

import (
	"encoding/json"
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
