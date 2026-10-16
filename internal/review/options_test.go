package review

import (
	"context"
	"path/filepath"
	"testing"
)

// cloned is a clone of a repository with one commit on main, as a clone of
// a real remote is: origin/HEAD says which its default branch is.
func cloned(t *testing.T) string {
	t.Helper()
	origin := repo(t, map[string]string{"a.txt": "one\n"})
	dir := filepath.Join(t.TempDir(), "work")
	git(t, filepath.Dir(dir), "clone", "-q", origin, dir)
	return dir
}

func options(t *testing.T, dir string) StartOptions {
	t.Helper()
	o, err := Options(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	return o
}

func TestOnTheDefaultBranchThereIsNothingToReviewAgainstIt(t *testing.T) {
	dir := cloned(t)
	if o := options(t, dir); o != (StartOptions{}) {
		t.Errorf("a clean clone, on main: %+v", o)
	}
	write(t, dir, "a.txt", "two\n")
	if o := options(t, dir); !o.Uncommitted {
		t.Errorf("with a change: %+v", o)
	}
}

func TestCommitsNotYetPushedAreOfferedForReview(t *testing.T) {
	dir := cloned(t)
	write(t, dir, "a.txt", "two\n")
	git(t, dir, "commit", "-q", "-am", "two")
	if o := options(t, dir); o.Upstream != "origin/main" || o.Unpushed != 1 {
		t.Errorf("one commit ahead of origin/main: %+v", o)
	}
}

func TestOnABranchItCanBeReviewedAgainstTheDefaultOne(t *testing.T) {
	dir := cloned(t)
	git(t, dir, "checkout", "-q", "-b", "feat")
	if o := options(t, dir); o.DefaultBranch != "main" || o.Upstream != "" {
		t.Errorf("a branch not pushed yet: %+v", o)
	}
	git(t, dir, "push", "-q", "-u", "origin", "feat")
	write(t, dir, "b.txt", "b\n")
	git(t, dir, "add", "b.txt")
	git(t, dir, "commit", "-q", "-m", "b")
	if o := options(t, dir); o.DefaultBranch != "main" || o.Upstream != "origin/feat" || o.Unpushed != 1 {
		t.Errorf("a pushed branch, a commit ahead: %+v", o)
	}
}

// Without a remote saying, main or master is taken for the default branch.
func TestWithNoRemoteMainOrMasterIsTheDefaultBranch(t *testing.T) {
	for _, name := range []string{"main", "master"} {
		dir := repo(t, map[string]string{"a.txt": "one\n"})
		git(t, dir, "branch", "-q", "-m", name)
		git(t, dir, "checkout", "-q", "-b", "feat")
		if o := options(t, dir); o.DefaultBranch != name {
			t.Errorf("with %s: %+v", name, o)
		}
	}
	dir := repo(t, map[string]string{"a.txt": "one\n"})
	git(t, dir, "branch", "-q", "-m", "trunk")
	git(t, dir, "checkout", "-q", "-b", "feat")
	if o := options(t, dir); o.DefaultBranch != "" {
		t.Errorf("neither there: %+v", o)
	}
}

// Unpushed commits' diff is what they changed: not what's uncommitted.
func TestTheUnpushedDiffIsTheCommitsAlone(t *testing.T) {
	dir := cloned(t)
	write(t, dir, "a.txt", "two\n")
	git(t, dir, "commit", "-q", "-am", "two")
	write(t, dir, "c.txt", "not committed\n")
	files, err := Unpushed(context.Background(), dir, "origin/main")
	if err != nil {
		t.Fatal(err)
	}
	if got := summary(files); got != "modified a.txt" {
		t.Errorf("files: %s", got)
	}
}
