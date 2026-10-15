package review

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// repo makes a git repository in a folder of the test's own, with files
// committed, and gives its path.
func repo(t *testing.T, files map[string]string) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git isn't installed")
	}
	dir := t.TempDir()
	git(t, dir, "init", "-q", "-b", "main")
	for name, body := range files {
		write(t, dir, name, body)
	}
	if len(files) > 0 {
		git(t, dir, "add", "-A")
		git(t, dir, "commit", "-q", "-m", "start")
	}
	return dir
}

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@t", "-c", "commit.gpgsign=false"}, args...)...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func write(t *testing.T, dir, name, body string) {
	t.Helper()
	path := filepath.Join(dir, name)
	os.MkdirAll(filepath.Dir(path), 0o755)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func summary(files []File) string {
	var out []string
	for _, f := range files {
		out = append(out, f.Status+" "+f.Path)
	}
	return strings.Join(out, ", ")
}

func TestTheWorkingTreesDiffHasUncommittedAndUntrackedChanges(t *testing.T) {
	dir := repo(t, map[string]string{"a.txt": "one\ntwo\n", "b.txt": "keep\n", "c.txt": "going\n"})
	write(t, dir, "a.txt", "one\nTWO\n")
	write(t, dir, "new/d.txt", "fresh\n")
	write(t, dir, "ignored.log", "noise\n")
	write(t, dir, ".gitignore", "*.log\n")
	os.Remove(filepath.Join(dir, "c.txt"))
	git(t, dir, "add", "a.txt") // staged or not, it's uncommitted

	files, err := WorkingTree(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := summary(files), "added .gitignore, modified a.txt, deleted c.txt, added new/d.txt"; got != want {
		t.Errorf("files: %s\nwant:  %s", got, want)
	}
	for _, f := range files {
		if f.Path == "new/d.txt" && (len(f.Hunks) != 1 || f.Hunks[0].Lines[0] != (Line{Kind: Addition, New: 1, Text: "fresh"})) {
			t.Errorf("the untracked file: %+v", f)
		}
	}
}

func TestAnUntrackedBinaryFileIsShownAsBinary(t *testing.T) {
	dir := repo(t, map[string]string{"a.txt": "a\n"})
	write(t, dir, "logo.png", "\x89PNG\x00\x01\x02")
	files, err := WorkingTree(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || !files[0].Binary || files[0].Status != Added || len(files[0].Hunks) != 0 {
		t.Errorf("files: %+v", files)
	}
}

func TestARepositoryWithNoCommitsYetHasADiff(t *testing.T) {
	dir := repo(t, nil)
	write(t, dir, "first.txt", "hello\n")
	files, err := WorkingTree(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := summary(files); got != "added first.txt" {
		t.Errorf("files: %s", got)
	}
}

func TestADiffAgainstABranchStartsWhereTheyParted(t *testing.T) {
	dir := repo(t, map[string]string{"a.txt": "a\n"})
	git(t, dir, "checkout", "-q", "-b", "feature")
	write(t, dir, "b.txt", "mine\n")
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-q", "-m", "mine")
	git(t, dir, "checkout", "-q", "main")
	write(t, dir, "c.txt", "theirs\n")
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-q", "-m", "theirs")
	git(t, dir, "checkout", "-q", "feature")
	write(t, dir, "a.txt", "a, changed\n")

	files, err := Against(context.Background(), dir, "main")
	if err != nil {
		t.Fatal(err)
	}
	// main's own c.txt isn't this branch's change; the uncommitted one is.
	if got, want := summary(files), "modified a.txt, added b.txt"; got != want {
		t.Errorf("files: %s\nwant:  %s", got, want)
	}
}

func TestACommitsDiffIsWhatItChanged(t *testing.T) {
	dir := repo(t, map[string]string{"a.txt": "a\n"})
	write(t, dir, "b.txt", "b\n")
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-q", "-m", "add b")
	write(t, dir, "a.txt", "uncommitted\n")
	files, err := Commit(context.Background(), dir, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if got := summary(files); got != "added b.txt" {
		t.Errorf("files: %s", got)
	}
}

func TestAFolderThatIsntARepositorySaysSo(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git isn't installed")
	}
	_, err := WorkingTree(context.Background(), t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "isn't a git repository") {
		t.Errorf("err = %v", err)
	}
}

func TestAnUnknownRefSaysSo(t *testing.T) {
	dir := repo(t, map[string]string{"a.txt": "a\n"})
	if _, err := Against(context.Background(), dir, "no-such-branch"); err == nil || !strings.Contains(err.Error(), "no-such-branch") {
		t.Errorf("err = %v", err)
	}
}
