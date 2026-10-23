package fscopy

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
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

func TestFileCopiesIntoAFolderItMakesAndKeepsTheMode(t *testing.T) {
	src := filepath.Join(t.TempDir(), "on-start")
	write(t, src, "#!/bin/sh\n")
	if err := os.Chmod(src, 0o750); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(t.TempDir(), "agent-files", ".local", "bin", "on-start")
	if err := File(src, dst); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(dst); err != nil || string(data) != "#!/bin/sh\n" {
		t.Errorf("copy = %q, %v", data, err)
	}
	if runtime.GOOS != "windows" {
		if info, _ := os.Stat(dst); info.Mode().Perm() != 0o750 {
			t.Errorf("mode = %v, want 0750", info.Mode().Perm())
		}
	}
	if err := File(filepath.Join(t.TempDir(), "missing"), dst); err == nil {
		t.Error("copying a file that isn't there succeeded")
	}
}

func TestWriteWholeLeavesOnlyTheFileReadableByTheUserAlone(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "new")
	path := filepath.Join(dir, "token")
	write(t, path, "old")
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := WriteWhole(path, []byte("new")); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(path); string(data) != "new" {
		t.Errorf("content = %q", data)
	}
	if runtime.GOOS != "windows" {
		if info, _ := os.Stat(path); info.Mode().Perm() != 0o600 {
			t.Errorf("mode = %v, want 0600 (not the old file's)", info.Mode().Perm())
		}
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Errorf("left beside it: %v", entries)
	}
}

func TestWriteWholeModeGivesTheFileThatModeAndLeavesNoTemporaryBehind(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.yaml")
	if err := WriteWholeMode(path, []byte("a: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	// Windows has no modes beyond read-only: every file it makes reads as 0666.
	if err != nil || (runtime.GOOS != "windows" && info.Mode().Perm() != 0o644) {
		t.Fatalf("mode = %v, %v; want 0644", info.Mode().Perm(), err)
	}
	if err := WriteWholeMode(path, []byte("a: 2\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	if info, _ = os.Stat(path); runtime.GOOS != "windows" && info.Mode().Perm() != 0o640 {
		t.Errorf("after a rewrite, mode = %v; want 0640", info.Mode().Perm())
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Errorf("the folder holds %d entries, want only the file: %v", len(entries), entries)
	}
}

func TestWriteWholeStillMakesAFileForTheUserAlone(t *testing.T) {
	path := filepath.Join(t.TempDir(), "kept.json")
	if err := WriteWhole(path, []byte("{}")); err != nil {
		t.Fatal(err)
	}
	if info, _ := os.Stat(path); runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v, want 0600", info.Mode().Perm())
	}
}

func TestAReaderNeverSeesWhatWriteWholeModeIsPartWayThrough(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.yaml")
	long := []byte("model: " + strings.Repeat("x", 4096) + "\n")
	short := []byte("model: y\n")
	if err := WriteWholeMode(path, short, 0o644); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 300; i++ {
			data := long
			if i%2 == 0 {
				data = short
			}
			if err := WriteWholeMode(path, data, 0o644); err != nil {
				t.Error(err)
				return
			}
		}
	}()
	for reading := true; reading; {
		select {
		case <-done:
			reading = false
		default:
		}
		got, err := ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, long) && !bytes.Equal(got, short) {
			t.Fatalf("read %d bytes that are neither version of the file", len(got))
		}
		// A reader that opens the file again the instant it has closed it leaves a
		// Windows rename no moment in which nobody has it open, so the writer's
		// bounded wait (see retryInUse) can run out: that is a reader no real one
		// is (a page polling, a turn starting), and not what this test is about,
		// which is that what is read is always one whole version or the other.
		// Reading about once a millisecond still races every write.
		time.Sleep(time.Millisecond)
	}
}

func TestAnErrorThatIsNotAFileInUseIsNotOne(t *testing.T) {
	_, err := os.ReadFile(filepath.Join(t.TempDir(), "nope"))
	if InUse(err) || InUse(nil) {
		t.Errorf("a missing file, or no error, was taken for a file in use")
	}
}
