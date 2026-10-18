package main

import (
	"os/exec"
	"testing"
)

// gitRepoWithOrigin makes a repository whose origin is remote.
func gitRepoWithOrigin(t *testing.T, remote string) string {
	t.Helper()
	need(t, "git")
	dir := t.TempDir()
	for _, args := range [][]string{{"init", "-q"}, {"remote", "add", "origin", remote}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	return dir
}

func TestTheRepositoryIsReadFromItsOrigin(t *testing.T) {
	dir := gitRepoWithOrigin(t, "git@github.com:owner/repo.git")
	repo := repoOf(dir)
	if repo == nil || repo.String() != "owner/repo" || !repo.SSH {
		t.Errorf("repoOf = %+v, want owner/repo over ssh", repo)
	}
	if repo := repoOf(t.TempDir()); repo != nil {
		t.Errorf("repoOf a folder that's no repository = %+v, want nil", repo)
	}
	if repo := repoOf(gitRepoWithOrigin(t, "https://example.com/o/r.git")); repo != nil {
		t.Errorf("repoOf a repository not on GitHub = %+v, want nil", repo)
	}
}
