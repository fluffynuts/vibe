package review

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAFilesLinesAreReadForExpandingItsContext(t *testing.T) {
	dir := repo(t, map[string]string{"src/a.txt": "one\ntwo\nthree\n"})
	write(t, dir, "src/a.txt", "one\nTWO\nthree\nfour\n")
	lines, err := FileLines(context.Background(), dir, Review{}, "src/a.txt")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(lines, "|") != "one|TWO|three|four" {
		t.Errorf("lines: %q", lines)
	}
}

func TestACommitsFileIsReadAsTheCommitHasIt(t *testing.T) {
	dir := repo(t, map[string]string{"a.txt": "committed\n"})
	write(t, dir, "a.txt", "changed since\n")
	lines, err := FileLines(context.Background(), dir, Review{Commit: "HEAD"}, "a.txt")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(lines, "|") != "committed" {
		t.Errorf("lines: %q", lines)
	}
}

// The page asks for files by path: nothing outside the sandbox's folder can
// be read through it.
func TestOnlyFilesInTheFolderCanBeRead(t *testing.T) {
	dir := repo(t, map[string]string{"a.txt": "a\n"})
	outside := filepath.Join(t.TempDir(), "secret.txt")
	os.WriteFile(outside, []byte("secret\n"), 0o600)
	os.Symlink(outside, filepath.Join(dir, "link.txt"))
	for _, path := range []string{"../secret.txt", outside, "link.txt", "a.txt/../../x", ""} {
		if lines, err := FileLines(context.Background(), dir, Review{}, path); err == nil {
			t.Errorf("%q was read: %q", path, lines)
		}
	}
}
