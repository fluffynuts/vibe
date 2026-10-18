//go:build !windows

package review

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// within fails the test if do takes more than a few seconds: a FIFO read
// would wait forever.
func within(t *testing.T, what string, do func()) {
	t.Helper()
	done := make(chan struct{})
	go func() { do(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatalf("%s hung", what)
	}
}

// The workspace is the sandbox's to fill: what's in it is read as git
// would, never through a link to elsewhere, never waiting on a FIFO.
func TestAnUntrackedLinkShowsWhereItPointsNotWhatsThere(t *testing.T) {
	dir := repo(t, map[string]string{"a.txt": "a\n"})
	secret := filepath.Join(t.TempDir(), "secret")
	os.WriteFile(secret, []byte("a secret of the host's\n"), 0o600)
	os.Symlink(secret, filepath.Join(dir, "leak"))
	os.Symlink("/dev/zero", filepath.Join(dir, "zero"))
	var files []File
	within(t, "the diff", func() {
		var err error
		if files, err = WorkingTree(context.Background(), dir); err != nil {
			t.Error(err)
		}
	})
	var texts []string
	for _, f := range files {
		for _, h := range f.Hunks {
			for _, l := range h.Lines {
				texts = append(texts, f.Path+": "+l.Text)
			}
		}
	}
	got := strings.Join(texts, "\n")
	if strings.Contains(got, "secret of the host") {
		t.Errorf("a linked file's content was read:\n%s", got)
	}
	if !strings.Contains(got, "leak: "+secret) || !strings.Contains(got, "zero: /dev/zero") {
		t.Errorf("want each link's target, as git shows it:\n%s", got)
	}
}

// git doesn't list a FIFO as untracked: nor does the diff, without
// waiting on it.
func TestAnUntrackedFIFOIsLeftOut(t *testing.T) {
	dir := repo(t, map[string]string{"a.txt": "a\n"})
	if err := syscall.Mkfifo(filepath.Join(dir, "pipe"), 0o600); err != nil {
		t.Fatal(err)
	}
	within(t, "the diff", func() {
		files, err := WorkingTree(context.Background(), dir)
		if err != nil {
			t.Error(err)
			return
		}
		if len(files) != 0 {
			t.Errorf("files = %+v", files)
		}
	})
}

func TestAFIFOsLinesArentWaitedOn(t *testing.T) {
	dir := repo(t, map[string]string{"a.txt": "a\n"})
	if err := syscall.Mkfifo(filepath.Join(dir, "pipe"), 0o600); err != nil {
		t.Fatal(err)
	}
	within(t, "reading its lines", func() {
		if _, err := FileLines(context.Background(), dir, Review{}, "pipe"); err == nil {
			t.Error("a FIFO's lines were read")
		}
	})
	within(t, "following comments", func() {
		Moves(context.Background(), dir, Review{Threads: []Thread{{ID: 1, File: "pipe", Side: "new", Line: 1, Code: "x"}}})
	})
}

func TestAnUntrackedFileThatCantBeReadSaysSo(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads any file")
	}
	dir := repo(t, map[string]string{"a.txt": "a\n"})
	write(t, dir, "locked.txt", "x\n")
	if err := os.Chmod(filepath.Join(dir, "locked.txt"), 0); err != nil {
		t.Fatal(err)
	}
	files, err := WorkingTree(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].Unreadable == "" {
		t.Errorf("files = %+v, want locked.txt saying it couldn't be read", files)
	}
}

// A link to a folder out of the workspace is shown as a link, and nothing
// in that folder is read, through it or by a path through it.
func TestALinkToAHostFolderIsntReadThrough(t *testing.T) {
	dir := repo(t, map[string]string{"a.txt": "a\n"})
	host := t.TempDir()
	os.WriteFile(filepath.Join(host, "id_rsa"), []byte("HOST SECRET\n"), 0o600)
	os.Symlink(host, filepath.Join(dir, "sub"))
	files, err := WorkingTree(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		for _, h := range f.Hunks {
			for _, l := range h.Lines {
				if strings.Contains(l.Text, "HOST SECRET") {
					t.Errorf("%s showed the host's file", f.Path)
				}
			}
		}
	}
	if _, err := FileLines(context.Background(), dir, Review{}, "sub/id_rsa"); err == nil {
		t.Error("a file was read through the link")
	}
}

// A file too large to show isn't read, however large: a sparse one, here.
func TestAHugeFileIsntRead(t *testing.T) {
	dir := repo(t, map[string]string{"a.txt": "a\n"})
	f, err := os.Create(filepath.Join(dir, "huge"))
	if err != nil {
		t.Fatal(err)
	}
	f.Truncate(20 << 30)
	f.Close()
	within(t, "the diff", func() {
		files, err := WorkingTree(context.Background(), dir)
		if err != nil {
			t.Error(err)
			return
		}
		if len(files) != 1 || !files[0].TooLarge || files[0].Lines != 0 {
			t.Errorf("files = %+v", files)
		}
	})
}

// A FIFO where git would read an ignore or attributes file is said to be
// unreadable, at once: nothing waits on it.
func TestAFIFOForAnIgnoreOrAttributesFileDoesntHang(t *testing.T) {
	for _, name := range []string{"sub/.gitignore", ".gitattributes", ".git/info/exclude", ".gitmodules"} {
		t.Run(name, func(t *testing.T) {
			dir := repo(t, map[string]string{"a.txt": "a\n", "sub/b.txt": "b\n"})
			os.MkdirAll(filepath.Join(dir, ".git", "info"), 0o755)
			os.Remove(filepath.Join(dir, filepath.FromSlash(name)))
			if err := syscall.Mkfifo(filepath.Join(dir, filepath.FromSlash(name)), 0o600); err != nil {
				t.Fatal(err)
			}
			write(t, dir, "a.txt", "changed\n")
			within(t, "the diff and the status", func() {
				WorkingTree(context.Background(), dir)
				StatusOf(context.Background(), dir)
			})
		})
	}
}

// A tracked file grown huge isn't read, by the diff or the status: it
// stands as changed, too large to show.
func TestAHugeTrackedFileIsntRead(t *testing.T) {
	dir := repo(t, map[string]string{"a.txt": "a\n"})
	if err := os.Truncate(filepath.Join(dir, "a.txt"), 4<<30); err != nil {
		t.Fatal(err)
	}
	within(t, "the diff and the status", func() {
		files, err := WorkingTree(context.Background(), dir)
		if err != nil || len(files) != 1 || !files[0].TooLarge {
			t.Errorf("files = %+v, %v", files, err)
		}
		if s, err := StatusOf(context.Background(), dir); err != nil || s.Modified != 1 {
			t.Errorf("status = %+v, %v", s, err)
		}
	})
}

// The .git the sandbox writes, swapped for a link to another repository
// once checked, isn't read: the one checked is.
func TestAGitDirectorySwappedAfterItsCheckedIsntRead(t *testing.T) {
	other := repo(t, map[string]string{"secret.txt": "the host's\n"})
	dir := repo(t, map[string]string{"a.txt": "a\n"})
	mine := strings.TrimSpace(gitOut(t, dir, "rev-parse", "HEAD"))
	r, err := openRepo(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer r.close()
	os.Rename(filepath.Join(dir, ".git"), filepath.Join(dir, ".git-was"))
	if err := os.Symlink(filepath.Join(other, ".git"), filepath.Join(dir, ".git")); err != nil {
		t.Fatal(err)
	}
	head, err := r.head()
	if err != nil || head.Hash.String() != mine {
		t.Errorf("head = %v, %v; want this repository's %s", head, err, mine)
	}
}

func TestADotGitThatsALinkIsRefused(t *testing.T) {
	other := repo(t, map[string]string{"x.txt": "the host's\n"})
	dir := t.TempDir()
	os.Symlink(filepath.Join(other, ".git"), filepath.Join(dir, ".git"))
	if _, err := WorkingTree(context.Background(), dir); err == nil || !strings.Contains(err.Error(), "is a link") {
		t.Errorf("err = %v", err)
	}
}

func TestACommonDirectoryOutsideTheFolderIsRefused(t *testing.T) {
	other := repo(t, map[string]string{"x.txt": "the host's\n"})
	dir := repo(t, map[string]string{"a.txt": "a\n"})
	write(t, dir, filepath.Join(".git", "commondir"), filepath.Join(other, ".git")+"\n")
	if _, err := WorkingTree(context.Background(), dir); err == nil || !strings.Contains(err.Error(), "outside the folder") {
		t.Errorf("err = %v", err)
	}
}

// The system's config is the host's own: one linked (as NixOS links
// /etc/gitconfig) is read.
func TestALinkedSystemConfigIsRead(t *testing.T) {
	dir := repo(t, map[string]string{"a.txt": "one\ntwo\n"})
	real := filepath.Join(t.TempDir(), "gitconfig")
	os.WriteFile(real, []byte("[core]\n\tautocrlf = true\n"), 0o644)
	link := filepath.Join(t.TempDir(), "linked")
	os.Symlink(real, link)
	t.Setenv("GIT_CONFIG_SYSTEM", link)
	write(t, dir, "a.txt", "one\r\ntwo\r\n")
	if files, _ := WorkingTree(context.Background(), dir); len(files) != 0 {
		t.Errorf("diff: %s", summary(files))
	}
}

// A git linked into a prefix (as Homebrew links it from its keg) has its
// system config beside the link, and beside where it really is.
func TestSystemConfigIsLookedForBesideALinkedGit(t *testing.T) {
	prefix := t.TempDir()
	keg := filepath.Join(prefix, "Cellar", "git", "2.50", "bin")
	os.MkdirAll(keg, 0o755)
	os.WriteFile(filepath.Join(keg, "git"), []byte("#!/bin/sh\n"), 0o755)
	os.MkdirAll(filepath.Join(prefix, "bin"), 0o755)
	os.Symlink(filepath.Join(keg, "git"), filepath.Join(prefix, "bin", "git"))
	t.Setenv("PATH", filepath.Join(prefix, "bin"))
	t.Setenv("GIT_CONFIG_SYSTEM", "")
	found := strings.Join(systemConfigs(), "\n")
	for _, want := range []string{filepath.Join(prefix, "bin", "..", "etc", "gitconfig"), filepath.Join(keg, "..", "etc", "gitconfig")} {
		if !strings.Contains(found, want) {
			t.Errorf("%s not looked at in:\n%s", want, found)
		}
	}
}

// The git directory is checked to be in the folder, then opened: a folder
// on its way swapped for a link out of it in between (the sandbox can) must
// not be followed. This is the state that swap leaves.
func TestAGitDirectoryReachedThroughALinkOutOfTheFolderIsntOpened(t *testing.T) {
	ws, outside := t.TempDir(), t.TempDir()
	if err := os.MkdirAll(filepath.Join(outside, "gd"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(ws, "sub")); err != nil {
		t.Fatal(err)
	}
	if root, err := openChecked(filepath.Join(ws, "sub", "gd"), ws); err == nil {
		root.Close()
		t.Error("a git directory out of the folder, through a link, was opened")
	}
	if err := os.MkdirAll(filepath.Join(ws, "real", "gd"), 0o755); err != nil {
		t.Fatal(err)
	}
	root, err := openChecked(filepath.Join(ws, "real", "gd"), ws)
	if err != nil {
		t.Fatalf("one in the folder: %v", err)
	}
	root.Close()
}
