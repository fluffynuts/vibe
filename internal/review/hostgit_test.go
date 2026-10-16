package review

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// ranMarker is a file a command the repository asks git to run would make:
// the test fails if it's there, as nothing the repository says may run.
func ranMarker(t *testing.T) (path, command string) {
	t.Helper()
	path = filepath.Join(t.TempDir(), "ran")
	return path, "touch '" + filepath.ToSlash(path) + "'"
}

func notRun(t *testing.T, marker, what string) {
	t.Helper()
	if _, err := os.Stat(marker); err == nil {
		t.Errorf("%s ran a command the repository set", what)
	}
}

// The repository's own config is the agent's to write: what it says to run
// must never run on this machine.
func TestTheRepositorysFsmonitorIsntRun(t *testing.T) {
	dir := repo(t, map[string]string{"a.txt": "a\n"})
	marker, command := ranMarker(t)
	git(t, dir, "config", "core.fsmonitor", command)
	write(t, dir, "a.txt", "b\n")
	if _, err := WorkingTree(context.Background(), dir); err != nil {
		t.Fatal(err)
	}
	if _, err := Options(context.Background(), dir); err != nil {
		t.Fatal(err)
	}
	notRun(t, marker, "the diff, or the start options,")
}

func TestTheRepositorysTextconvIsntRun(t *testing.T) {
	dir := repo(t, map[string]string{"a.txt": "a\n"})
	marker, command := ranMarker(t)
	write(t, dir, ".gitattributes", "*.txt diff=conv\n")
	git(t, dir, "config", "diff.conv.textconv", command+"; cat")
	write(t, dir, "a.txt", "b\n")
	if _, err := WorkingTree(context.Background(), dir); err != nil {
		t.Fatal(err)
	}
	notRun(t, marker, "the diff")
}

func TestTheRepositorysHooksArentRun(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("a hook is a shell script")
	}
	dir := repo(t, map[string]string{"a.txt": "a\n"})
	marker, command := ranMarker(t)
	hook := filepath.Join(dir, ".git", "hooks", "post-index-change")
	if err := os.WriteFile(hook, []byte("#!/bin/sh\n"+command+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, dir, "a.txt", "b\n")
	if _, err := Options(context.Background(), dir); err != nil {
		t.Fatal(err)
	}
	notRun(t, marker, "the start options")
}

// A filter can't be turned off by name in advance: a repository that sets
// one of its own isn't diffed, saying why.
func TestARepositoryWithAFilterOfItsOwnIsntDiffed(t *testing.T) {
	dir := repo(t, map[string]string{"a.txt": "a\n"})
	marker, command := ranMarker(t)
	write(t, dir, ".gitattributes", "*.txt filter=pwn\n")
	git(t, dir, "config", "filter.pwn.clean", command+"; cat")
	write(t, dir, "a.txt", "b\n")
	_, err := WorkingTree(context.Background(), dir)
	if err == nil || !strings.Contains(err.Error(), "filter.pwn.clean") {
		t.Errorf("err = %v, want it to name the filter", err)
	}
	notRun(t, marker, "the diff")
}

func TestOneOfTheUsersOwnFiltersIsTrusted(t *testing.T) {
	dir := repo(t, map[string]string{"a.txt": "a\n"})
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, ".gitconfig"), []byte("[filter \"lfs\"]\n\tclean = cat\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	write(t, dir, "a.txt", "b\n")
	if _, err := WorkingTree(context.Background(), dir); err != nil {
		t.Errorf("a filter in the user's own config refused: %v", err)
	}
}

// What the agent asks a review to be of reaches git: an option in its
// place mustn't.
func TestTheOldSideRefusesAnOptionForACommit(t *testing.T) {
	dir := repo(t, map[string]string{"a.txt": "a\n"})
	out := filepath.Join(t.TempDir(), "written")
	for _, rv := range []Review{{Commit: "--output=" + out}, {Ref: "--output=" + out}} {
		if _, err := OldTextOf(context.Background(), dir, rv, "a.txt"); err == nil {
			t.Errorf("%+v: no error", rv)
		}
	}
	if matches, _ := filepath.Glob(out + "*"); len(matches) > 0 {
		t.Errorf("git wrote %v", matches)
	}
}

func TestAStartForAnOptionIsntTakenIn(t *testing.T) {
	s := NewStore(t.TempDir())
	drop(t, s, "1.json", Request{Op: "start", Commit: "--output=/tmp/x"})
	drop(t, s, "2.json", Request{Op: "start", Ref: "-x"})
	r, err := s.Take()
	if err != nil {
		t.Fatal(err)
	}
	if r.Commit != "" || r.Ref != "" || r.State != "" {
		t.Errorf("review = %+v", r)
	}
	if len(r.Problems) != 2 {
		t.Errorf("problems = %q", r.Problems)
	}
}

func TestAPathWithASpaceIsReadWithoutGitsTab(t *testing.T) {
	dir := repo(t, map[string]string{"foo bar.txt": "one\n", "pl ain.txt": "same\nsame\nsame\nsame\n"})
	write(t, dir, "foo bar.txt", "one\ntwo\n")
	git(t, dir, "mv", "pl ain.txt", "new name.txt")
	write(t, dir, "new name.txt", "same\nsame\nsame\nsame\nmore\n")
	files, err := WorkingTree(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := summary(files); got != "modified foo bar.txt, renamed new name.txt" {
		t.Errorf("files = %q", got)
	}
	if files[1].OldPath != "pl ain.txt" {
		t.Errorf("old path = %q", files[1].OldPath)
	}
	if files[0].Lines != 2 {
		t.Errorf("foo bar.txt has %d lines", files[0].Lines)
	}
}

func TestAMergeCommitsDiffIsWhatItBroughtIn(t *testing.T) {
	dir := repo(t, map[string]string{"a.txt": "a\n"})
	git(t, dir, "checkout", "-q", "-b", "side")
	write(t, dir, "b.txt", "from the side\n")
	git(t, dir, "add", "b.txt")
	git(t, dir, "commit", "-q", "-m", "side")
	git(t, dir, "checkout", "-q", "main")
	write(t, dir, "a.txt", "a\nmain\n")
	git(t, dir, "commit", "-q", "-am", "main")
	git(t, dir, "merge", "-q", "--no-edit", "side")
	files, err := Commit(context.Background(), dir, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if got := summary(files); got != "added b.txt" {
		t.Errorf("files = %q", got)
	}
}

func TestTheRootOfAFoldersRepositoryIsFound(t *testing.T) {
	dir := repo(t, map[string]string{"sub/a.txt": "a\n"})
	root, err := Root(context.Background(), filepath.Join(dir, "sub"))
	if err != nil {
		t.Fatal(err)
	}
	want, _ := filepath.EvalSymlinks(dir)
	if got, _ := filepath.EvalSymlinks(root); got != want {
		t.Errorf("root = %q, want %q", root, dir)
	}
}

func TestABinaryFileChangedAgainHasANewFingerprint(t *testing.T) {
	dir := repo(t, map[string]string{"a.bin": "\x00one"})
	print := func() string {
		files, err := WorkingTree(context.Background(), dir)
		if err != nil {
			t.Fatal(err)
		}
		return files[0].Fingerprint
	}
	write(t, dir, "a.bin", "\x00two")
	before := print()
	write(t, dir, "a.bin", "\x00three")
	if print() == before {
		t.Error("the binary file changed again, its fingerprint didn't")
	}
}

func TestAnUntrackedBinaryFileChangedAgainHasANewFingerprint(t *testing.T) {
	dir := repo(t, map[string]string{"a.txt": "a\n"})
	print := func() string {
		files, err := WorkingTree(context.Background(), dir)
		if err != nil {
			t.Fatal(err)
		}
		return files[0].Fingerprint
	}
	write(t, dir, "b.bin", "\x00two")
	before := print()
	write(t, dir, "b.bin", "\x00three, longer")
	if print() == before {
		t.Error("the untracked binary file changed again, its fingerprint didn't")
	}
}

func TestAModeChangeChangedBackHasANewFingerprint(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no executable bit")
	}
	dir := repo(t, map[string]string{"a.sh": "echo\n"})
	print := func() string {
		files, err := WorkingTree(context.Background(), dir)
		if err != nil {
			t.Fatal(err)
		}
		if len(files) == 0 {
			return "none"
		}
		return files[0].Fingerprint
	}
	os.Chmod(filepath.Join(dir, "a.sh"), 0o755)
	executable := print()
	git(t, dir, "commit", "-q", "-am", "x")
	os.Chmod(filepath.Join(dir, "a.sh"), 0o644)
	if print() == executable {
		t.Error("a different mode change, the same fingerprint")
	}
}

func TestWhyGitCantReadAFolderIsGitsOwnSay(t *testing.T) {
	dir := repo(t, map[string]string{"a.txt": "a\n"})
	// An unknown repository format is one git refuses, and says why.
	git(t, dir, "config", "core.repositoryformatversion", "99")
	_, err := WorkingTree(context.Background(), dir)
	if err == nil || strings.Contains(err.Error(), "isn't a git repository") || !strings.Contains(err.Error(), "found 99") {
		t.Errorf("err = %v, want git's own reason", err)
	}
}

// Carriage returns aren't part of a line: a CRLF file's lines read the same
// from the diff, the file and the comments made on it.
func TestACarriageReturnIsntPartOfALine(t *testing.T) {
	dir := repo(t, map[string]string{"a.txt": "one\r\ntwo\r\n"})
	write(t, dir, "a.txt", "zero\r\none\r\ntwo\r\n")
	files, err := WorkingTree(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range files[0].Hunks {
		for _, l := range h.Lines {
			if strings.HasSuffix(l.Text, "\r") {
				t.Errorf("diff line %q keeps its carriage return", l.Text)
			}
		}
	}
	lines, err := TextOf(context.Background(), dir, Review{}, "a.txt")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(lines, "|") != "zero|one|two" {
		t.Errorf("lines = %q", lines)
	}
	// A comment made on "two" at line 2 follows it to line 3, exactly.
	moves := Moves(context.Background(), dir, Review{Threads: []Thread{{ID: 1, File: "a.txt", Side: "new", Line: 2, Code: "two"}}})
	if len(moves) != 1 || moves[0].Line != 3 {
		t.Errorf("moves = %+v", moves)
	}
}

// Whitespace changes can be left out of the diff; a file's fingerprint (its
// Viewed tick) doesn't change with how the diff is shown.
func TestWhitespaceChangesCanBeLeftOut(t *testing.T) {
	dir := repo(t, map[string]string{"a.txt": "one\ntwo\n", "b.txt": "x\n"})
	write(t, dir, "a.txt", "one\n  two\n")
	write(t, dir, "b.txt", "y\n")
	all, err := WorkingTree(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if summary(all) != "modified a.txt, modified b.txt" {
		t.Fatalf("all: %s", summary(all))
	}
	some, err := WorkingTree(context.Background(), dir, Diffing{IgnoreSpace: true})
	if err != nil {
		t.Fatal(err)
	}
	if summary(some) != "modified b.txt" {
		t.Errorf("ignoring whitespace: %s", summary(some))
	}
	if len(some) == 1 && some[0].Fingerprint != all[1].Fingerprint {
		t.Error("b.txt's fingerprint changed with how the diff is shown")
	}
}

func TestAnUntrackedFileTouchedButUnchangedKeepsItsFingerprint(t *testing.T) {
	dir := repo(t, map[string]string{"a.txt": "a\n"})
	write(t, dir, "new.txt", "fresh\n")
	print := func() string {
		files, err := WorkingTree(context.Background(), dir)
		if err != nil {
			t.Fatal(err)
		}
		return files[0].Fingerprint
	}
	before := print()
	later := time.Now().Add(time.Hour)
	os.Chtimes(filepath.Join(dir, "new.txt"), later, later)
	if print() != before {
		t.Error("touched, unchanged, its fingerprint changed")
	}
	write(t, dir, "new.txt", "fresher\n")
	if print() == before {
		t.Error("changed, its fingerprint didn't")
	}
}
