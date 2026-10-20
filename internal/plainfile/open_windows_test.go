//go:build windows

package plainfile

import (
	"os"
	"path/filepath"
	"testing"
)

// The path is checked, then opened: a plain file swapped for a link in between is not followed.
func TestAFileSwappedForALinkWhileItIsBeingOpenedIsNotFollowed(t *testing.T) {
	dir := t.TempDir()
	secret := filepath.Join(dir, "secret.txt")
	file := filepath.Join(dir, "request.json")
	if err := os.WriteFile(secret, []byte("a host file"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secret, filepath.Join(dir, "link")); err != nil {
		t.Skipf("no symlinks here: %v", err)
	}
	was := afterLstat
	defer func() { afterLstat = was }()
	afterLstat = func(path string) {
		os.Remove(path)
		os.Rename(filepath.Join(dir, "link"), path)
	}
	if data, err := Read(file, 1024); err == nil {
		t.Errorf("read %q through a link swapped in after the check", data)
	}
}
