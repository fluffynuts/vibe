package review

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// lengths is each file's line count, by path.
func lengths(files []File) string {
	var out []string
	for _, f := range files {
		out = append(out, fmt.Sprintf("%s=%d", f.Path, f.Lines))
	}
	return strings.Join(out, " ")
}

// Each file in a diff says how long it is: as it is now, or, deleted, as
// it was; a last line without a newline counts.
func TestTheWorkingTreesFilesSayHowLongTheyAre(t *testing.T) {
	dir := repo(t, map[string]string{"a.txt": "one\n", "gone.txt": "1\n2\n3\n"})
	write(t, dir, "a.txt", "one\ntwo\nthree")
	write(t, dir, "new.txt", "fresh\n")
	git(t, dir, "rm", "-q", "gone.txt")
	files, err := WorkingTree(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := lengths(files); got != "a.txt=3 gone.txt=3 new.txt=1" {
		t.Errorf("lengths: %s", got)
	}
}

func TestACommitsFilesAreAsLongAsTheCommitHasThem(t *testing.T) {
	dir := repo(t, map[string]string{"a.txt": "one\n"})
	write(t, dir, "a.txt", "one\ntwo\n")
	git(t, dir, "commit", "-q", "-am", "two")
	write(t, dir, "a.txt", "changed since, and longer\n\n\n\n")
	files, err := Commit(context.Background(), dir, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if got := lengths(files); got != "a.txt=2" {
		t.Errorf("lengths: %s", got)
	}
}

func TestUnpushedFilesAreAsLongAsTheLastCommitHasThem(t *testing.T) {
	dir := cloned(t)
	write(t, dir, "a.txt", "one\ntwo\n")
	git(t, dir, "commit", "-q", "-am", "two")
	write(t, dir, "a.txt", "not committed\n\n\n\n\n")
	files, err := Unpushed(context.Background(), dir, "origin/main")
	if err != nil {
		t.Fatal(err)
	}
	if got := lengths(files); got != "a.txt=2" {
		t.Errorf("lengths: %s", got)
	}
}

// Each file in a diff has a fingerprint of its change, the same while the
// change is, and different once it isn't: a file ticked as viewed is
// known to have changed since.
func TestAFilesFingerprintChangesWithItsChange(t *testing.T) {
	dir := repo(t, map[string]string{"a.txt": "one\n", "b.txt": "b\n"})
	write(t, dir, "a.txt", "one\ntwo\n")
	write(t, dir, "b.txt", "b\nb\n")
	print := func() map[string]string {
		files, err := WorkingTree(context.Background(), dir)
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]string{}
		for _, f := range files {
			if f.Fingerprint == "" {
				t.Fatalf("%s has no fingerprint", f.Path)
			}
			out[f.Path] = f.Fingerprint
		}
		return out
	}
	before := print()
	if again := print(); again["a.txt"] != before["a.txt"] {
		t.Error("the same change, a different fingerprint")
	}
	write(t, dir, "a.txt", "one\ntwo\nthree\n")
	after := print()
	if after["a.txt"] == before["a.txt"] {
		t.Error("a's change changed, its fingerprint didn't")
	}
	if after["b.txt"] != before["b.txt"] {
		t.Error("b's change didn't change, its fingerprint did")
	}
}
