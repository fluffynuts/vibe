package fscopy

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestTreeMergeAddsNewFilesWithoutOverwritingExisting(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()
	write(t, filepath.Join(src, "a"), "from src\n")
	write(t, filepath.Join(src, "sub", "b"), "from src\n")
	write(t, filepath.Join(dst, "a"), "already mine\n")

	if err := TreeMerge(src, dst); err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(filepath.Join(dst, "a"))
	if err != nil || string(got) != "already mine\n" {
		t.Errorf("existing file was overwritten: %q, %v", got, err)
	}
	got, err = os.ReadFile(filepath.Join(dst, "sub", "b"))
	if err != nil || string(got) != "from src\n" {
		t.Errorf("new file was not merged in: %q, %v", got, err)
	}
}

func TestTreeMergePreservesExecutableBitOnNewFiles(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows files have no executable bit")
	}
	src := t.TempDir()
	dst := t.TempDir()
	write(t, filepath.Join(src, "run"), "#!/bin/sh\necho hi\n")
	if err := os.Chmod(filepath.Join(src, "run"), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := TreeMerge(src, dst); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(dst, "run"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o100 == 0 {
		t.Errorf("expected the executable bit preserved, got %v", info.Mode())
	}
}

func TestTreeKeepTimesCopiesModificationTimes(t *testing.T) {
	src, dst := t.TempDir(), filepath.Join(t.TempDir(), "copy")
	file := filepath.Join(src, "sub", "file.md")
	write(t, file, "content")
	then := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	for _, p := range []string{file, filepath.Dir(file), src} {
		if err := os.Chtimes(p, then, then); err != nil {
			t.Fatal(err)
		}
	}
	if err := TreeKeepTimes(src, dst); err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{".", "sub", filepath.Join("sub", "file.md")} {
		info, err := os.Stat(filepath.Join(dst, rel))
		if err != nil {
			t.Fatal(err)
		}
		if !info.ModTime().Equal(then) {
			t.Errorf("%s modified %s, want %s", rel, info.ModTime(), then)
		}
	}
}
