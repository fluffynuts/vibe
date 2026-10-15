package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"vibe/internal/review"
)

// vibeReview runs the review feature's command, as the agent does in the
// sandbox, with its review folder dir, and gives what it said and its exit
// code.
func vibeReview(t *testing.T, dir, stdin string, args ...string) (string, int) {
	return vibeReviewIn(t, "", dir, stdin, args...)
}

// vibeReviewIn is vibeReview, run in folder work ("" for this one).
func vibeReviewIn(t *testing.T, work, dir, stdin string, args ...string) (string, int) {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node isn't installed: vibe-review's tests need it")
	}
	script, err := filepath.Abs(filepath.Join("..", "..", "library", "base", "review", "agent-files", ".local", "share", "vibe", "review-cli.js"))
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(node, append([]string{script}, args...)...)
	cmd.Dir = work
	cmd.Env = append(os.Environ(), "VIBE_REVIEW_DIR="+dir)
	cmd.Stdin = strings.NewReader(stdin)
	out, err := cmd.CombinedOutput()
	code := 0
	if exitErr, ok := err.(*exec.ExitError); ok {
		code = exitErr.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	return string(out), code
}

func TestTheAgentsReviewReachesTheTab(t *testing.T) {
	dir := t.TempDir()
	steps := []struct {
		stdin string
		args  []string
	}{
		{"", []string{"start", "--note", "the login change", "--ref", "main"}},
		{"", []string{"comment", "--file", "main.go", "--line", "12", "--end-line", "14", "--body", "P1: this leaks"}},
		{"P3: a `name` with\nlines, and 'quotes'", []string{"comment", "--file", "a.go", "--line", "3", "--side", "old", "--body-file", "-"}},
		{"", []string{"general", "--body", "Mostly fine."}},
		{`[{"file":"main.go","line":12,"label":"the core","body":"start here"}]`, []string{"order"}},
		{"", []string{"done"}},
	}
	for _, s := range steps {
		if out, code := vibeReview(t, dir, s.stdin, s.args...); code != 0 {
			t.Fatalf("vibe-review %v: exit %d\n%s", s.args, code, out)
		}
	}
	r, err := review.NewStore(dir).Take()
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Problems) != 0 {
		t.Errorf("problems: %q", r.Problems)
	}
	if r.Note != "the login change" || r.Ref != "main" || r.State != review.Done {
		t.Errorf("review: %+v", r)
	}
	if len(r.Threads) != 3 {
		t.Fatalf("threads: %+v", r.Threads)
	}
	if th := r.Threads[1]; th.File != "a.go" || th.Side != "old" || th.Body() != "P3: a `name` with\nlines, and 'quotes'" {
		t.Errorf("the comment from stdin: %+v", th)
	}
	if th := r.Threads[0]; th.Line != 12 || th.EndLine != 14 {
		t.Errorf("the ranged comment: %+v", th)
	}
	if len(r.Order) != 1 || r.Order[0].Label != "the core" {
		t.Errorf("order: %+v", r.Order)
	}
}

// The agent reads the user's replies with list.
func TestTheAgentSeesTheUsersReplies(t *testing.T) {
	dir := t.TempDir()
	store := review.NewStore(dir)
	store.Do(review.Request{Op: "comment", File: "main.go", Line: 12, Body: "why?"}, review.Agent)
	store.Do(review.Request{Op: "reply", Thread: 1, Body: "because of the cache"}, review.User)
	out, code := vibeReview(t, dir, "", "list")
	if code != 0 {
		t.Fatalf("exit %d\n%s", code, out)
	}
	for _, want := range []string{"#1", "main.go:12", "agent: why?", "user: because of the cache"} {
		if !strings.Contains(out, want) {
			t.Errorf("list lacks %q:\n%s", want, out)
		}
	}
	if _, code := vibeReview(t, dir, "", "reply", "--thread", "1", "--body", "got it"); code != 0 {
		t.Error("replying failed")
	}
	if r, _ := store.Take(); len(r.Threads[0].Comments) != 3 {
		t.Errorf("the agent's reply didn't arrive: %+v", r.Threads[0])
	}
}

func TestVibeReviewSaysWhatsWrongWithWhatItWasAsked(t *testing.T) {
	dir := t.TempDir()
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"comment", "--file", "a.go", "--body", "x"}, "--line"},
		{[]string{"comment", "--file", "a.go", "--line", "two", "--body", "x"}, "--line"},
		{[]string{"reply", "--body", "x"}, "--thread"},
		{[]string{"bogus"}, "usage"},
		{[]string{"comment", "--file", "a.go", "--line", "1", "--side", "left", "--body", "x"}, "--side"},
	} {
		out, code := vibeReview(t, dir, "", tc.args...)
		if code == 0 || !strings.Contains(out, tc.want) {
			t.Errorf("%v: exit %d, said %q; want it to mention %q", tc.args, code, out, tc.want)
		}
	}
	if left, _ := os.ReadDir(filepath.Join(dir, "inbox")); len(left) != 0 {
		t.Errorf("a refused request was dropped anyway: %d", len(left))
	}
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node isn't installed")
	}
	cmd := exec.Command(node, filepath.Join("..", "..", "library", "base", "review", "agent-files", ".local", "share", "vibe", "review-cli.js"), "done")
	cmd.Env = []string{"PATH=" + os.Getenv("PATH")}
	if out, err := cmd.CombinedOutput(); err == nil || !strings.Contains(string(out), "VIBE_REVIEW_DIR") {
		t.Errorf("with no review folder: %v, %s", err, out)
	}
}

// The agent reviews what the user sees: diff is the tab's diff, untracked
// files and all.
func TestTheAgentsDiffIsTheTabs(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git isn't installed")
	}
	work := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@t", "-c", "commit.gpgsign=false"}, args...)...)
		cmd.Dir = work
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q", "-b", "main")
	os.WriteFile(filepath.Join(work, "a.txt"), []byte("one\n"), 0o644)
	run("add", "-A")
	run("commit", "-q", "-m", "start")
	os.WriteFile(filepath.Join(work, "a.txt"), []byte("one\ntwo\n"), 0o644)
	os.WriteFile(filepath.Join(work, "new.txt"), []byte("fresh\n"), 0o644)

	out, code := vibeReviewIn(t, work, t.TempDir(), "", "diff")
	if code != 0 {
		t.Fatalf("exit %d\n%s", code, out)
	}
	files, err := review.Parse(out)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, f := range files {
		got = append(got, f.Status+" "+f.Path)
	}
	if strings.Join(got, ", ") != "modified a.txt, added new.txt" {
		t.Errorf("files: %v\n%s", got, out)
	}
}

// A comment records the lines it's on as they read, so the tab can tell
// when they've changed since.
func TestACommentRecordsTheLinesItsOn(t *testing.T) {
	work := t.TempDir()
	os.WriteFile(filepath.Join(work, "a.go"), []byte("one\ntwo\nthree\nfour\n"), 0o644)
	dir := t.TempDir()
	if out, code := vibeReviewIn(t, work, dir, "", "comment", "--file", "a.go", "--line", "2", "--end-line", "3", "--body", "x"); code != 0 {
		t.Fatalf("exit %d\n%s", code, out)
	}
	r, err := review.NewStore(dir).Take()
	if err != nil {
		t.Fatal(err)
	}
	if got := r.Threads[0].Code; got != "two\nthree" {
		t.Errorf("code = %q", got)
	}
}
