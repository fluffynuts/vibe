package review

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/go-git/go-billy/v5/osfs"
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

// A filter is a command the repository names for git to run on its files:
// none is run here, whoever set it; the file is diffed as it is.
func TestARepositorysFilterIsntRun(t *testing.T) {
	dir := repo(t, map[string]string{"a.txt": "a\n"})
	marker, command := ranMarker(t)
	write(t, dir, ".gitattributes", "*.txt filter=pwn\n")
	git(t, dir, "config", "filter.pwn.clean", command+"; cat")
	write(t, dir, "a.txt", "b\n")
	files, err := WorkingTree(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if summary(files) != "added .gitattributes, modified a.txt" {
		t.Errorf("files = %s", summary(files))
	}
	notRun(t, marker, "the diff")
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

func TestWhyARepositoryCantBeReadIsSaid(t *testing.T) {
	dir := repo(t, map[string]string{"a.txt": "a\n"})
	sub := filepath.Join(dir, "sub")
	os.MkdirAll(sub, 0o755)
	// A .git file that doesn't say where its git directory is.
	write(t, sub, ".git", "nonsense\n")
	_, err := WorkingTree(context.Background(), sub)
	if err == nil || strings.Contains(err.Error(), "isn't a git repository") || !strings.Contains(err.Error(), "doesn't say where") {
		t.Errorf("err = %v, want why", err)
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

// The page's header: what's staged, changed, new; how far the branch is
// from its upstream; and stashes, as git status says them.
func TestTheFoldersStatusIsCounted(t *testing.T) {
	dir := repo(t, map[string]string{"a.txt": "a\n", "b.txt": "b\n", "c.txt": "c\n"})
	git(t, dir, "branch", "up")
	git(t, dir, "branch", "-q", "-u", "up")
	write(t, dir, "a.txt", "a2\n")
	git(t, dir, "commit", "-q", "-am", "ahead")
	write(t, dir, "b.txt", "b2\n")
	git(t, dir, "add", "b.txt")
	write(t, dir, "c.txt", "c2\n")
	write(t, dir, "new.txt", "n\n")
	write(t, dir, "s.txt", "s\n")
	git(t, dir, "add", "s.txt")
	git(t, dir, "stash", "push", "-q", "--", "s.txt")
	s, err := StatusOf(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	want := FolderStatus{Branch: "main", Staged: 1, Modified: 1, Untracked: 1, Ahead: 1, Stashed: 1}
	if *s != want {
		t.Errorf("status = %+v, want %+v", *s, want)
	}
}

func TestADetachedHeadIsNamedByItsCommit(t *testing.T) {
	dir := repo(t, map[string]string{"a.txt": "a\n"})
	git(t, dir, "checkout", "-q", "--detach")
	s, err := StatusOf(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(s.Branch, "@") || len(s.Branch) != 8 {
		t.Errorf("branch = %q", s.Branch)
	}
}

// A file kept in Git LFS is committed as a pointer to its content, which
// git-lfs (a filter: never run here) makes; its content unchanged is no
// change, and changed is the pointer's change, as git shows it.
func TestAFileInLFSIsComparedByItsPointer(t *testing.T) {
	content := "a large binary, say\n"
	sum := sha256.Sum256([]byte(content))
	pointer := "version https://git-lfs.github.com/spec/v1\noid sha256:" + hex.EncodeToString(sum[:]) + "\nsize " + strconv.Itoa(len(content)) + "\n"
	dir := repo(t, map[string]string{"big.bin": pointer})
	write(t, dir, "big.bin", content)
	files, err := WorkingTree(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 0 {
		t.Errorf("unchanged, yet: %s", summary(files))
	}
	write(t, dir, "big.bin", "something else\n")
	if files, _ = WorkingTree(context.Background(), dir); summary(files) != "modified big.bin" {
		t.Errorf("changed: %s", summary(files))
	}
}

// With core.autocrlf, a CRLF file whose LF-ended lines are as committed
// hasn't changed, as git sees it.
func TestCRLFsAreLFsWithAutocrlf(t *testing.T) {
	dir := repo(t, map[string]string{"a.txt": "one\ntwo\n"})
	git(t, dir, "config", "core.autocrlf", "true")
	write(t, dir, "a.txt", "one\r\ntwo\r\n")
	files, err := WorkingTree(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 0 {
		t.Errorf("unchanged, yet: %s", summary(files))
	}
}

// A file's fingerprint rests on the full ids of the blobs compared, not
// git's shortened ones, which another content can share.
func TestAFingerprintRestsOnFullBlobIDs(t *testing.T) {
	dir := repo(t, map[string]string{"a.txt": "a\n"})
	write(t, dir, "a.txt", "b\n")
	// The stamp is the helper's own: read in this process.
	files, err := workingTree(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`\b[0-9a-f]{40}\b.*\b[0-9a-f]{40}\b`).MatchString(files[0].stamp) {
		t.Errorf("stamp = %q", files[0].stamp)
	}
}

// The repository's config can't point the tab elsewhere: core.worktree is
// no matter; the top is the folder or one above it.
func TestCoreWorktreeDoesntMoveTheTop(t *testing.T) {
	dir := repo(t, map[string]string{"a.txt": "a\n"})
	elsewhere := t.TempDir()
	write(t, elsewhere, "secret", "host secret\n")
	git(t, dir, "config", "core.worktree", elsewhere)
	top, err := Root(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	want, _ := filepath.EvalSymlinks(dir)
	if top != want {
		t.Errorf("top = %q, want the folder", top)
	}
	if lines, err := FileLines(context.Background(), dir, Review{}, "secret"); err == nil {
		t.Errorf("read %q from elsewhere", lines)
	}
}

// A .git in the folder is the sandbox's to write: one leading out of it
// (to another repository on the host) is refused, saying why.
func TestAGitDirectoryOutsideTheFolderIsRefused(t *testing.T) {
	other := repo(t, map[string]string{"x.txt": "the host's\n"})
	dir := t.TempDir()
	write(t, dir, ".git", "gitdir: "+filepath.Join(other, ".git")+"\n")
	_, err := WorkingTree(context.Background(), dir)
	if err == nil || !strings.Contains(err.Error(), "outside the folder") {
		t.Errorf("err = %v", err)
	}
}

func TestObjectsBorrowedFromElsewhereAreRefused(t *testing.T) {
	other := repo(t, map[string]string{"x.txt": "the host's\n"})
	dir := repo(t, map[string]string{"a.txt": "a\n"})
	write(t, dir, filepath.Join(".git", "objects", "info", "alternates"), filepath.Join(other, ".git", "objects")+"\n")
	_, err := WorkingTree(context.Background(), dir)
	if err == nil || !strings.Contains(err.Error(), "alternates") {
		t.Errorf("err = %v", err)
	}
}

// Line endings as git sees them: a checkout whose CRLFs git made (by the
// system's core.autocrlf, as Git for Windows sets; or .gitattributes'
// text and eol) is clean, in the diff, the start options and the status.
func TestTheSystemsAutocrlfIsHeeded(t *testing.T) {
	dir := repo(t, map[string]string{"a.txt": "one\ntwo\n"})
	system := filepath.Join(t.TempDir(), "gitconfig")
	os.WriteFile(system, []byte("[core]\n\tautocrlf = true\n"), 0o644)
	t.Setenv("GIT_CONFIG_SYSTEM", system)
	write(t, dir, "a.txt", "one\r\ntwo\r\n")
	cleanAsGitSeesIt(t, dir)
}

func TestTextAttributesMakeCRLFsLFs(t *testing.T) {
	dir := repo(t, map[string]string{".gitattributes": "* text=auto\n", "a.txt": "one\ntwo\n"})
	write(t, dir, "a.txt", "one\r\ntwo\r\n")
	cleanAsGitSeesIt(t, dir)
}

func TestNotTextLeavesCRLFsAsTheyAre(t *testing.T) {
	dir := repo(t, map[string]string{".gitattributes": "*.txt -text\n", "a.txt": "one\ntwo\n"})
	git(t, dir, "config", "core.autocrlf", "true")
	write(t, dir, "a.txt", "one\r\ntwo\r\n")
	if files, _ := WorkingTree(context.Background(), dir); summary(files) != "modified a.txt" {
		t.Errorf("files = %s", summary(files))
	}
}

func cleanAsGitSeesIt(t *testing.T, dir string) {
	t.Helper()
	files, err := WorkingTree(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 0 {
		t.Errorf("diff: %s", summary(files))
	}
	if o, _ := Options(context.Background(), dir); o.Uncommitted {
		t.Error("the start options see uncommitted changes")
	}
	if s, _ := StatusOf(context.Background(), dir); s == nil || s.Modified != 0 {
		t.Errorf("status = %+v", s)
	}
}

// A large committed file isn't read whole, by the diff or the status,
// however small it has become.
func TestALargeCommittedFileIsntReadWhole(t *testing.T) {
	t.Setenv("VIBE_REVIEW_HELPER_MEMORY", "200")
	dir := repo(t, map[string]string{"a.txt": "a\n"})
	big := make([]byte, maxFileBytes+(1<<20))
	for i := range big {
		big[i] = byte('a' + i%26)
	}
	write(t, dir, "big.txt", string(big))
	git(t, dir, "add", "big.txt")
	git(t, dir, "commit", "-q", "-m", "big")
	write(t, dir, "big.txt", "small now\n")
	files, err := WorkingTree(context.Background(), dir)
	if err != nil || len(files) != 1 || !files[0].TooLarge {
		t.Errorf("files = %+v, %v", files, err)
	}
	if _, err := StatusOf(context.Background(), dir); err != nil {
		t.Error(err)
	}
}

// The Review tab only reads: a submodule set up but not fetched (which
// go-git's own status would set up) is left as it is, as is all the rest.
func TestReadingARepositoryChangesNothingInIt(t *testing.T) {
	lib := repo(t, map[string]string{"lib.txt": "lib\n"})
	super := repo(t, map[string]string{"a.txt": "a\n"})
	git(t, super, "-c", "protocol.file.allow=always", "submodule", "add", "-q", lib, "lib")
	git(t, super, "commit", "-q", "-m", "lib")
	dir := filepath.Join(t.TempDir(), "clone")
	git(t, filepath.Dir(dir), "clone", "-q", super, dir)
	git(t, dir, "submodule", "init")
	write(t, dir, "a.txt", "changed\n")
	before := snapshot(t, dir)
	WorkingTree(context.Background(), dir)
	StatusOf(context.Background(), dir)
	Options(context.Background(), dir)
	if after := snapshot(t, dir); after != before {
		t.Errorf("the repository changed:\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

// snapshot is every path under dir, with its size and time.
func snapshot(t *testing.T, dir string) string {
	t.Helper()
	var out []string
	filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err == nil {
			out = append(out, fmt.Sprintf("%s %d %d", p, info.Size(), info.ModTime().UnixNano()))
		}
		return nil
	})
	return strings.Join(out, "\n")
}

func TestASparseCheckoutSaysWhatIsntSupported(t *testing.T) {
	dir := repo(t, map[string]string{"a.txt": "a\n"})
	git(t, dir, "config", "core.repositoryformatversion", "1")
	git(t, dir, "config", "extensions.worktreeConfig", "true")
	if _, err := WorkingTree(context.Background(), dir); err == nil || !strings.Contains(err.Error(), "sparse-checkout") {
		t.Errorf("err = %v", err)
	}
}

// Where git status would say otherwise, the tab says as git does.
func TestAFileTakenOutOfTheIndexIsADeletionAndUntracked(t *testing.T) {
	dir := repo(t, map[string]string{"a.txt": "a\n", "b.txt": "b\n"})
	git(t, dir, "rm", "-q", "--cached", "a.txt")
	s, err := StatusOf(context.Background(), dir)
	if err != nil || s.Staged != 1 || s.Untracked != 1 {
		t.Errorf("status = %+v, %v", s, err)
	}
	files, _ := WorkingTree(context.Background(), dir)
	if summary(files) != "deleted a.txt, added a.txt" {
		t.Errorf("files = %s", summary(files))
	}
}

func TestAModeChangeIsNoChangeWithoutFileMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no executable bit")
	}
	dir := repo(t, map[string]string{"a.sh": "echo\n"})
	git(t, dir, "config", "core.fileMode", "false")
	os.Chmod(filepath.Join(dir, "a.sh"), 0o755)
	if files, _ := WorkingTree(context.Background(), dir); len(files) != 0 {
		t.Errorf("files = %s", summary(files))
	}
	if s, _ := StatusOf(context.Background(), dir); s == nil || s.Modified != 0 {
		t.Errorf("status = %+v", s)
	}
}

func TestTheUsersOwnIgnoreFilesAreHeeded(t *testing.T) {
	dir := repo(t, map[string]string{"a.txt": "a\n"})
	xdg := t.TempDir()
	write(t, xdg, filepath.Join("git", "ignore"), "*.log\n")
	t.Setenv("XDG_CONFIG_HOME", xdg)
	t.Setenv("HOME", t.TempDir())
	write(t, dir, ".git/own-ignore", "*.tmp\n")
	git(t, dir, "config", "core.excludesfile", ".git/own-ignore")
	write(t, dir, "x.log", "log\n")
	write(t, dir, "y.tmp", "tmp\n")
	write(t, dir, "z.txt", "kept\n")
	files, _ := WorkingTree(context.Background(), dir)
	if summary(files) != "added z.txt" {
		t.Errorf("files = %s", summary(files))
	}
}

func TestBinaryContentIsntMadeLFsByTextAuto(t *testing.T) {
	binary := "PNG\x00\r\nbody\r\n"
	dir := repo(t, map[string]string{".gitattributes": "* text=auto eol=crlf\n", "a.png": binary})
	r, err := openRepo(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer r.close()
	if lf, err := r.lfInRepository("a.png", []byte(binary)); err != nil || lf {
		t.Errorf("binary made LFs: %v, %v", lf, err)
	}
}

// The host's own repository may borrow objects (a clone --shared): read
// where they are.
func TestTheHostsRepositoryReadsBorrowedObjects(t *testing.T) {
	origin := repo(t, map[string]string{"sub/a.txt": "a\n"})
	top := filepath.Join(t.TempDir(), "shared")
	git(t, filepath.Dir(top), "clone", "-q", "--shared", origin, top)
	write(t, top, "sub/a.txt", "changed\n")
	files, err := WorkingTree(context.Background(), filepath.Join(top, "sub"))
	if err != nil || summary(files) != "modified sub/a.txt" {
		t.Errorf("files = %s, %v", summary(files), err)
	}
}

// The walk counting how far a branch is from its upstream is bounded.
func TestTheWalkAheadAndBehindIsBounded(t *testing.T) {
	dir := repo(t, map[string]string{"a.txt": "0\n"})
	git(t, dir, "branch", "up")
	for i := 1; i <= 30; i++ {
		write(t, dir, "a.txt", fmt.Sprintf("%d\n", i))
		git(t, dir, "commit", "-q", "-am", fmt.Sprint(i))
	}
	r, err := openRepo(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer r.close()
	head, _ := r.head()
	up, _ := r.commit("up")
	if ahead, _ := aheadBehind(head, up); ahead != 30 {
		t.Fatalf("ahead %d, want 30", ahead)
	}
	was := maxWalk
	maxWalk = 10
	defer func() { maxWalk = was }()
	if ahead, _ := aheadBehind(head, up); ahead > 12 {
		t.Errorf("walked on past the bound: ahead %d", ahead)
	}
}

// A borrowed object folder named by its absolute path is read there,
// whatever the platform's paths look like.
func TestTheHostsFileSystemTakesAbsolutePathsAsTheyAre(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "objects"), 0o755)
	h := hostFS{osfs.New("/")}
	if info, err := h.Stat(filepath.Join(dir, "objects")); err != nil || !info.IsDir() {
		t.Fatalf("stat: %v, %v", info, err)
	}
	sub, err := h.Chroot(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sub.Stat("objects"); err != nil {
		t.Errorf("in the chroot: %v", err)
	}
}
