package review

import (
	"context"
	"os"
	"path/filepath"
	"strings"
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

// Without origin saying which is the default branch, none is guessed (not
// even main or master): why is said instead, with the fix when there is one.
func TestWithoutOriginsHEADNoDefaultBranchIsGuessed(t *testing.T) {
	for _, name := range []string{"main", "master"} {
		dir := repo(t, map[string]string{"a.txt": "one\n"})
		git(t, dir, "branch", "-q", "-m", name)
		git(t, dir, "checkout", "-q", "-b", "feat")
		if o := options(t, dir); o.DefaultBranch != "" || !strings.Contains(o.NoDefaultBranch, "no origin remote") {
			t.Errorf("with %s and no remote: %+v", name, o)
		}
	}
	dir := cloned(t)
	git(t, dir, "remote", "set-head", "origin", "-d")
	git(t, dir, "checkout", "-q", "-b", "feat")
	if o := options(t, dir); o.DefaultBranch != "" || !strings.Contains(o.NoDefaultBranch, "git remote set-head origin --auto") {
		t.Errorf("origin without a HEAD: %+v", o)
	}
}

func mainline(t *testing.T, dir string) Mainline {
	t.Helper()
	m, err := DefaultBranch(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestTheDefaultBranchIsNamedAsOriginsHEADSays(t *testing.T) {
	dir := cloned(t)
	if m := mainline(t, dir); m.Name != "main" || m.Set || m.Why != "" {
		t.Errorf("default branch = %+v", m)
	}
	git(t, dir, "remote", "set-head", "origin", "-d")
	if m := mainline(t, dir); m.Name != "" || !strings.Contains(m.Why, "set-head") {
		t.Errorf("without origin/HEAD: %+v", m)
	}
}

// A repository origin can't speak for (none yet, or nothing pushed) can
// have its default branch set: it's used only then.
func TestASetDefaultBranchIsUsedWhenOriginCantSay(t *testing.T) {
	dir := repo(t, map[string]string{"a.txt": "one\n"})
	git(t, dir, "config", DefaultBranchKey, "main")
	git(t, dir, "checkout", "-q", "-b", "feat")
	if m := mainline(t, dir); m.Name != "main" || !m.Set || m.Why != "" {
		t.Errorf("default branch = %+v", m)
	}
	if o := options(t, dir); o.DefaultBranch != "main" || o.NoDefaultBranch != "" {
		t.Errorf("options = %+v", o)
	}
}

func TestOriginsHEADWinsOverASetDefaultBranch(t *testing.T) {
	dir := cloned(t)
	git(t, dir, "config", DefaultBranchKey, "trunk")
	if m := mainline(t, dir); m.Name != "main" || m.Set {
		t.Errorf("default branch = %+v", m)
	}
}

func TestASetDefaultBranchThatIsntABranchNameIsntUsed(t *testing.T) {
	dir := repo(t, map[string]string{"a.txt": "one\n"})
	git(t, dir, "config", DefaultBranchKey, "a..b")
	if m := mainline(t, dir); m.Name != "" || !strings.Contains(m.Why, "isn't a branch name") {
		t.Errorf("default branch = %+v", m)
	}
}

// In a new repository, the default branch can be named before it has a
// commit: there's nothing to review against it yet, which is said.
func TestADefaultBranchWithNoCommitsYetIsSaid(t *testing.T) {
	dir := repo(t, nil)
	git(t, dir, "config", DefaultBranchKey, "master")
	write(t, dir, "a.txt", "one\n")
	if o := options(t, dir); o.DefaultBranch != "" || o.EmptyDefaultBranch != "master" || o.NoDefaultBranch != "" {
		t.Errorf("an empty repository: %+v", o)
	}
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-q", "-m", "start")
	if o := options(t, dir); o.DefaultBranch != "" || o.EmptyDefaultBranch != "master" {
		t.Errorf("with commits on main alone: %+v", o)
	}
}

func TestWhenNoDefaultBranchCanBeToldTheLocalBranchesAreSuggested(t *testing.T) {
	dir := repo(t, map[string]string{"a.txt": "one\n"})
	git(t, dir, "branch", "-q", "feat")
	if m := mainline(t, dir); m.Name != "" || strings.Join(m.Branches, ",") != "feat,main" {
		t.Errorf("default branch = %+v", m)
	}
	if m := mainline(t, repo(t, nil)); m.Name != "" || len(m.Branches) != 0 || m.Why == "" {
		t.Errorf("a new repository: %+v", m)
	}
}

// With no local branch of that name, the review is against origin's.
func TestWithNoLocalDefaultBranchTheReviewIsAgainstOrigins(t *testing.T) {
	dir := cloned(t)
	git(t, dir, "checkout", "-q", "-b", "feat")
	git(t, dir, "branch", "-q", "-D", "main")
	if o := options(t, dir); o.DefaultBranch != "origin/main" {
		t.Errorf("options = %+v", o)
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

// As on a Windows runner: git, by the system's core.autocrlf, checks the
// clone out with CRLFs. Clean, it has nothing uncommitted.
func TestACleanCRLFCheckoutHasNothingUncommitted(t *testing.T) {
	system := filepath.Join(t.TempDir(), "gitconfig")
	os.WriteFile(system, []byte("[core]\n\tautocrlf = true\n"), 0o644)
	t.Setenv("GIT_CONFIG_SYSTEM", system)
	dir := cloned(t)
	if data, _ := os.ReadFile(filepath.Join(dir, "a.txt")); string(data) != "one\r\n" {
		t.Fatalf("git didn't check out CRLFs: %q", data)
	}
	if o := options(t, dir); o != (StartOptions{}) {
		t.Errorf("a clean clone, on main: %+v", o)
	}
}
