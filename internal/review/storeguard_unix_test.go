//go:build !windows

package review

import (
	"encoding/json"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestAnInboxEntryThatIsntAPlainFileIsntRead(t *testing.T) {
	s := NewStore(t.TempDir())
	secret := filepath.Join(t.TempDir(), "secret.json")
	data, _ := json.Marshal(Request{Op: "general", Body: "a secret of the host's"})
	if err := os.WriteFile(secret, data, 0o600); err != nil {
		t.Fatal(err)
	}
	os.MkdirAll(s.Inbox(), 0o755)
	if err := os.Symlink(secret, filepath.Join(s.Inbox(), "1.json")); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(filepath.Join(s.Inbox(), "2.json"), 0o600); err != nil {
		t.Fatal(err)
	}
	done := make(chan Review, 1)
	go func() {
		r, err := s.Take()
		if err != nil {
			t.Error(err)
		}
		done <- r
	}()
	select {
	case r := <-done:
		if len(r.Threads) != 0 {
			t.Errorf("a linked file was taken in: %+v", r.Threads)
		}
		if len(r.Problems) != 2 {
			t.Errorf("problems = %q", r.Problems)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("taking in the inbox hung on a FIFO")
	}
}
