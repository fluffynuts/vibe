package companion

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestParseGitStatus(t *testing.T) {
	out := "# branch.oid 1234567890abcdef\n" +
		"# branch.head master\n" +
		"# branch.upstream origin/master\n" +
		"# branch.ab +2 -1\n" +
		"# stash 3\n" +
		"1 .M N... 100644 100644 100644 a b readme.md\n" +
		"1 M. N... 100644 100644 100644 a b go.mod\n" +
		"1 MM N... 100644 100644 100644 a b go.sum\n" +
		"2 R. N... 100644 100644 100644 a b R100 new\told\n" +
		"u UU N... 100644 100644 100644 100644 a b c conflict.go\n" +
		"? scratch.txt\n" +
		"? other.txt\n"
	got := parseGitStatus(out)
	want := GitInfo{Branch: "master", Staged: 3, Modified: 2, Untracked: 2, Conflicted: 1, Ahead: 2, Behind: 1, Stashed: 3}
	if got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestParseGitStatusDetached(t *testing.T) {
	if got := parseGitStatus("# branch.oid 1234567890abcdef\n# branch.head (detached)\n").Branch; got != "@1234567" {
		t.Errorf("detached head = %q", got)
	}
	if got := parseGitStatus("# branch.oid (initial)\n# branch.head (detached)\n").Branch; got != "detached" {
		t.Errorf("detached initial = %q", got)
	}
}

func TestShortenPath(t *testing.T) {
	home := filepath.Join(string(filepath.Separator), "home", "me")
	cases := map[string]string{
		home:                                "~",
		filepath.Join(home, "code", "vibe"): filepath.Join("~", "code", "vibe"),
		filepath.Join(home+"x", "code"):     filepath.Join(home+"x", "code"),
		filepath.Join(string(filepath.Separator), "srv"): filepath.Join(string(filepath.Separator), "srv"),
	}
	for in, want := range cases {
		if got := shortenPath(in, home); got != want {
			t.Errorf("shortenPath(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestReadGit(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	dir := t.TempDir()
	if readGit(dir) != nil {
		t.Errorf("a folder outside any repository has git status")
	}
	if out, err := exec.Command("git", "-C", dir, "init", "-q", "-b", "trunk").CombinedOutput(); err != nil {
		t.Skipf("git init: %v %s", err, out)
	}
	os.WriteFile(filepath.Join(dir, "new.txt"), []byte("x"), 0o644)
	got := readGit(dir)
	if got == nil || got.Branch != "trunk" || got.Untracked != 1 {
		t.Errorf("readGit = %+v", got)
	}
}

// The repository's config is the sandbox's to write: reading the header's
// status mustn't run what it says.
func TestReadingGitStatusDoesntRunTheRepositorysCommands(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	dir := t.TempDir()
	if out, err := exec.Command("git", "-C", dir, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	marker := filepath.Join(t.TempDir(), "ran")
	if out, err := exec.Command("git", "-C", dir, "config", "core.fsmonitor", "touch '"+filepath.ToSlash(marker)+"'").CombinedOutput(); err != nil {
		t.Fatalf("git config: %v %s", err, out)
	}
	os.WriteFile(filepath.Join(dir, "new.txt"), []byte("x"), 0o644)
	readGit(dir)
	if _, err := os.Stat(marker); err == nil {
		t.Error("reading git status ran the repository's fsmonitor")
	}
}

func TestStateSaysWhereTheWorkspaceIs(t *testing.T) {
	home := homeDir()
	s := New(Config{Sandbox: "vibe", Token: token, Port: testPort,
		Workspace: filepath.Join(home, "code", "vibe"),
		Git:       func(string) *GitInfo { return &GitInfo{Branch: "master", Modified: 2} },
	})
	var got struct {
		Folder string   `json:"folder"`
		Git    *GitInfo `json:"git"`
	}
	json.Unmarshal(do(t, s, req{path: "/api/state", headers: authed(nil)}).Body.Bytes(), &got)
	if home != "" && got.Folder != filepath.Join("~", "code", "vibe") {
		t.Errorf("folder = %q", got.Folder)
	}
	if got.Git == nil || got.Git.Branch != "master" || got.Git.Modified != 2 {
		t.Errorf("git = %+v", got.Git)
	}

	none := New(Config{Sandbox: "vibe", Token: token, Port: testPort})
	got.Folder, got.Git = "stale", &GitInfo{}
	json.Unmarshal(do(t, none, req{path: "/api/state", headers: authed(nil)}).Body.Bytes(), &got)
	if got.Folder != "" || got.Git != nil {
		t.Errorf("a page with no workspace showed %q %+v", got.Folder, got.Git)
	}
}
