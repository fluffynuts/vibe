package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"vibe/internal/review"
)

// need gives the path of tool, which the test needs: without it, the test
// is skipped, but never in CI, where its going missing would leave
// vibe-review untested without anyone noticing.
func need(t *testing.T, tool string) string {
	t.Helper()
	path, err := exec.LookPath(tool)
	if err != nil {
		if os.Getenv("CI") != "" {
			t.Fatalf("%s isn't installed, and vibe-review's tests need it", tool)
		}
		t.Skipf("%s isn't installed: vibe-review's tests need it", tool)
	}
	return path
}

// reviewFolder is a review folder as the host makes it for a sandbox: its
// inbox, and the view of the review.
func reviewFolder(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, sub := range []string{"inbox", "view"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// vibeReview runs the review feature's command, as the agent does in the
// sandbox, with its review folder dir, and gives what it said and its exit
// code.
func vibeReview(t *testing.T, dir, stdin string, args ...string) (string, int) {
	return vibeReviewIn(t, "", dir, stdin, args...)
}

// vibeReviewIn is vibeReview, run in folder work ("" for this one), which
// is the sandbox's workspace too.
func vibeReviewIn(t *testing.T, work, dir, stdin string, args ...string) (string, int) {
	t.Helper()
	env := []string{"VIBE_REVIEW_DIR=" + dir}
	if work != "" {
		env = append(env, "VIBE_REVIEW_WORKSPACE="+work)
	}
	return runVibeReview(t, work, env, stdin, args...)
}

// runVibeReview runs the review feature's command in folder cwd, with env
// added to this process's.
func runVibeReview(t *testing.T, cwd string, env []string, stdin string, args ...string) (string, int) {
	t.Helper()
	node := need(t, "node")
	script, err := filepath.Abs(filepath.Join("..", "..", "library", "base", "review", "agent-files", ".local", "share", "vibe", "review-cli.js"))
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(node, append([]string{script}, args...)...)
	cmd.Dir = cwd
	cmd.Env = append(os.Environ(), env...)
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
	dir := reviewFolder(t)
	host(t, dir, 0)
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
	dir := reviewFolder(t)
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
	dir := reviewFolder(t)
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
	node := need(t, "node")
	cmd := exec.Command(node, filepath.Join("..", "..", "library", "base", "review", "agent-files", ".local", "share", "vibe", "review-cli.js"), "done")
	cmd.Env = []string{"PATH=" + os.Getenv("PATH")}
	if out, err := cmd.CombinedOutput(); err == nil || !strings.Contains(string(out), "VIBE_REVIEW_DIR") {
		t.Errorf("with no review folder: %v, %s", err, out)
	}
}

// The agent reviews what the user sees: diff is the tab's diff, untracked
// files and all.
func TestTheAgentsDiffIsTheTabs(t *testing.T) {
	need(t, "git")
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

	out, code := vibeReviewIn(t, work, reviewFolder(t), "", "diff")
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
	dir := reviewFolder(t)
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

// A review of unpushed commits is what they changed, as the tab shows it:
// nothing uncommitted. The agent can start one itself, of the branch's
// upstream.
func TestTheAgentReviewsUnpushedCommitsAsTheTabDoes(t *testing.T) {
	need(t, "git")
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
	run("branch", "pushed")
	run("branch", "-q", "-u", "pushed")
	os.WriteFile(filepath.Join(work, "a.txt"), []byte("one\ntwo\n"), 0o644)
	run("commit", "-q", "-am", "two")
	os.WriteFile(filepath.Join(work, "new.txt"), []byte("not committed\n"), 0o644)

	dir := reviewFolder(t)
	host(t, dir, 0)
	if out, code := vibeReviewIn(t, work, dir, "", "start", "--unpushed"); code != 0 {
		t.Fatalf("start --unpushed: exit %d\n%s", code, out)
	}
	store := review.NewStore(dir)
	r, err := store.Take()
	if err != nil {
		t.Fatal(err)
	}
	if r.Ref != "pushed" || !r.Unpushed {
		t.Fatalf("started: %+v", r)
	}
	out, code := vibeReviewIn(t, work, dir, "", "diff")
	if code != 0 {
		t.Fatalf("diff: exit %d\n%s", code, out)
	}
	files, err := review.Parse(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].Path != "a.txt" {
		t.Errorf("files: %+v\n%s", files, out)
	}
}

// Without the review folder mounted (a sandbox built before, a failed
// mount), a request would go nowhere: vibe-review says so, and makes no
// folder of its own.
func TestWithoutItsInboxVibeReviewSaysTheFolderIsntMounted(t *testing.T) {
	work := t.TempDir()
	dir := filepath.Join(t.TempDir(), "not-mounted")
	out, code := vibeReviewIn(t, work, dir, "", "general", "--body", "x")
	if code == 0 || !strings.Contains(out, "isn't mounted") || !strings.Contains(out, "vibe -r") {
		t.Errorf("exit %d, said %q", code, out)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("a review folder was made: %v", err)
	}
}

// Only no review at all is an empty one: one that can't be read says so.
func TestAReviewThatCantBeReadIsSaidToBe(t *testing.T) {
	dir := reviewFolder(t)
	if err := os.WriteFile(filepath.Join(dir, "view", "review.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, code := vibeReview(t, dir, "", "list")
	if code == 0 || !strings.Contains(out, "review.json") {
		t.Errorf("exit %d, said %q", code, out)
	}
}

func TestListSaysWhatABranchsReviewIsOf(t *testing.T) {
	dir := reviewFolder(t)
	review.NewStore(dir).Do(review.Request{Op: "start", Ref: "main", Reviewing: true}, review.User)
	out, code := vibeReview(t, dir, "", "list")
	if code != 0 || !strings.HasPrefix(out, "review of the changes against main") {
		t.Errorf("exit %d, said %q", code, out)
	}
}

// The agent may have gone into a folder of the workspace: its diff is
// still the whole workspace's, with the paths the tab has.
func TestTheAgentsDiffIsTheWorkspacesFromASubfolder(t *testing.T) {
	need(t, "git")
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
	os.MkdirAll(filepath.Join(work, "sub"), 0o755)
	os.WriteFile(filepath.Join(work, "top.txt"), []byte("top\n"), 0o644)
	os.WriteFile(filepath.Join(work, "sub", "inner.txt"), []byte("inner\n"), 0o644)

	out, code := runVibeReview(t, filepath.Join(work, "sub"), []string{"VIBE_REVIEW_DIR=" + reviewFolder(t), "VIBE_REVIEW_WORKSPACE=" + work}, "", "diff")
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
	if strings.Join(got, ", ") != "added sub/inner.txt, added top.txt" {
		t.Errorf("files: %v\n%s", got, out)
	}
}

// A stop the tab couldn't take in is refused at once, saying why, rather
// than failing later where the agent wouldn't see it.
func TestAnOrderTheTabCouldntTakeIsRefused(t *testing.T) {
	dir := reviewFolder(t)
	for _, stops := range []string{`[{"file":"a.go","line":"12"}]`, `[{"line":3}]`, `["a.go"]`, `[{"file":"a.go","line":0}]`} {
		out, code := vibeReview(t, dir, stops, "order")
		if code == 0 || !strings.Contains(out, "stop 1") {
			t.Errorf("%s: exit %d, said %q", stops, code, out)
		}
	}
	if left, _ := os.ReadDir(filepath.Join(dir, "inbox")); len(left) != 0 {
		t.Errorf("a refused order was dropped anyway: %d", len(left))
	}
}

// host stands in for vibe on the host, taking in the review folder dir's
// inbox every so often (as the companion does), the first time after
// first, until the test ends.
func host(t *testing.T, dir string, first time.Duration) *review.Store {
	t.Helper()
	store := review.NewStore(dir)
	done := make(chan struct{})
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		select {
		case <-done:
			return
		case <-time.After(first):
		}
		for {
			select {
			case <-done:
				return
			case <-time.After(20 * time.Millisecond):
				store.Take()
			}
		}
	}()
	t.Cleanup(func() { close(done); <-stopped })
	return store
}

// start waits until vibe has taken its review in: the diff that follows is
// the new review's, as the tab shows it.
func TestAStartIsTakenInBeforeItReturns(t *testing.T) {
	need(t, "git")
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
	run("checkout", "-q", "-b", "feature")
	os.WriteFile(filepath.Join(work, "b.txt"), []byte("branch work\n"), 0o644)
	run("add", "-A")
	run("commit", "-q", "-m", "branch")

	dir := reviewFolder(t)
	// Slower than the agent's next step, to show start waits for it.
	host(t, dir, 700*time.Millisecond)
	if out, code := vibeReviewIn(t, work, dir, "", "start", "--ref", "main"); code != 0 {
		t.Fatalf("start: exit %d\n%s", code, out)
	}
	out, code := vibeReviewIn(t, work, dir, "", "diff")
	if code != 0 {
		t.Fatalf("diff: exit %d\n%s", code, out)
	}
	if files, _ := review.Parse(out); len(files) != 1 || files[0].Path != "b.txt" {
		t.Errorf("the diff isn't the branch's:\n%s", out)
	}
}

func TestAStartNobodyTakesInSaysSo(t *testing.T) {
	dir := reviewFolder(t)
	out, code := runVibeReview(t, "", []string{"VIBE_REVIEW_DIR=" + dir, "VIBE_REVIEW_START_WAIT_MS=300"}, "", "start")
	if code == 0 || !strings.Contains(out, "is vibe running") {
		t.Errorf("exit %d, said %q", code, out)
	}
}

func TestAStartTheTabRefusesSaysWhy(t *testing.T) {
	dir := reviewFolder(t)
	host(t, dir, 0)
	out, code := vibeReview(t, dir, "", "start", "--ref", "-x")
	if code == 0 || !strings.Contains(out, "isn't a branch or commit") {
		t.Errorf("exit %d, said %q", code, out)
	}
}
