package plainfile

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestReadGivesAPlainFilesContent(t *testing.T) {
	p := filepath.Join(t.TempDir(), "f")
	write(t, p, "hello")
	got, err := Read(p, 100)
	if err != nil || string(got) != "hello" {
		t.Errorf("got %q, %v", got, err)
	}
}

func TestReadRefusesAFileOverItsLimit(t *testing.T) {
	p := filepath.Join(t.TempDir(), "f")
	write(t, p, strings.Repeat("x", 11))
	if _, err := Read(p, 10); !errors.As(err, &TooLarge{}) {
		t.Errorf("want TooLarge, got %v", err)
	}
	if _, err := Read(p, 11); err != nil {
		t.Errorf("a file exactly at the limit is fine: %v", err)
	}
}

func TestReadRefusesALinkEvenToAPlainFile(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	write(t, target, "secret")
	link := filepath.Join(dir, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("no symlinks here: %v", err)
	}
	if got, err := Read(link, 100); err == nil {
		t.Errorf("followed a link to read %q", got)
	}
}

func TestReadRefusesADirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("a directory can't be opened to read on Windows")
	}
	if _, err := Read(t.TempDir(), 100); err == nil {
		t.Error("read a directory")
	}
}
