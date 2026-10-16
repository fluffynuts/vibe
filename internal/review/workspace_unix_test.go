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
