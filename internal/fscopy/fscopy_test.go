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

func TestTreeKeepsTheExecutableBit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows files have no executable bit")
	}
	src := t.TempDir()
	dst := filepath.Join(t.TempDir(), "copy")
	write(t, filepath.Join(src, "install-scripts", "run"), "#!/bin/sh\necho hi\n")
	if err := os.Chmod(filepath.Join(src, "install-scripts", "run"), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := Tree(src, dst); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(dst, "install-scripts", "run"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o100 == 0 {
		t.Errorf("expected the executable bit kept, got %v", info.Mode())
	}
}

func TestTreeRecreatesSymlinksAsSymlinks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on Windows")
	}
	src := t.TempDir()
	dst := filepath.Join(t.TempDir(), "copy")
	write(t, filepath.Join(src, "real"), "x")
	if err := os.Symlink("real", filepath.Join(src, "link")); err != nil {
		t.Fatal(err)
	}

	if err := Tree(src, dst); err != nil {
		t.Fatal(err)
	}
	if got, err := os.Readlink(filepath.Join(dst, "link")); err != nil || got != "real" {
		t.Errorf("link = %q, %v; want a symlink to %q", got, err, "real")
	}
}

// The destination may be a folder a sandbox can write to (a memory store):
// a link it puts where the copy is about to write must not take the copy
// out of the folder.
func TestTreeKeepTimesNeverWritesThroughALinkOutOfTheDestination(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on Windows")
	}
	src, dst, outside := t.TempDir(), t.TempDir(), t.TempDir()
	write(t, filepath.Join(src, "sub", "file.md"), "content")
	write(t, filepath.Join(src, "top.md"), "content")
	if err := os.Symlink(outside, filepath.Join(dst, "sub")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "top.md"), filepath.Join(dst, "top.md")); err != nil {
		t.Fatal(err)
	}

	TreeKeepTimes(src, dst)

	for _, name := range []string{"file.md", "top.md"} {
		if _, err := os.Lstat(filepath.Join(outside, name)); err == nil {
			t.Errorf("%s was written outside the destination", name)
		}
	}
}

// The source may be such a folder too (a memory store being snapshotted):
// a link in it is copied as a link, never followed out of it.
func TestTreeKeepTimesNeverReadsThroughALinkOutOfTheSource(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on Windows")
	}
	src, dst, outside := t.TempDir(), filepath.Join(t.TempDir(), "copy"), t.TempDir()
	write(t, filepath.Join(outside, "secret"), "host only")
	if err := os.Symlink(outside, filepath.Join(src, "dir")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "secret"), filepath.Join(src, "file")); err != nil {
		t.Fatal(err)
	}

	if err := TreeKeepTimes(src, dst); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"dir", "file"} {
		if info, err := os.Lstat(filepath.Join(dst, name)); err != nil || info.Mode()&os.ModeSymlink == 0 {
			t.Errorf("%s: %v, %v; want it copied as a link", name, info, err)
		}
	}
}
