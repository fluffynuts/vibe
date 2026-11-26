package activity

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"vibe/internal/review"
)

func TestMain(m *testing.M) {
	review.ServeHelperIfAsked()
	os.Exit(m.Run())
}

func TestRepoNameIsOriginsNameElseTheFolders(t *testing.T) {
	for _, c := range []struct{ origin, folder, want string }{
		{"git@github.com:codeo/yumbi-web.git", "/code/yumbi", "yumbi-web"},
		{"https://github.com/codeo/yumbi-web", "/code/yumbi.alt", "yumbi-web"},
		{"https://github.com/codeo/yumbi-web.git/", "/code/x", "yumbi-web"},
		{"ssh://git@host:2222/team/phoenix.git", "/code/x", "phoenix"},
		{`C:\repos\local`, "/code/x", "local"},
		{"", "/code/phoenix", "phoenix"},
	} {
		if got := RepoName(c.origin, c.folder); got != c.want {
			t.Errorf("RepoName(%q, %q): want %q, got %q", c.origin, c.folder, c.want, got)
		}
	}
}

// repo is a git repository in a temporary folder, where each git command runs at a time set for it.
type repo struct {
	t   *testing.T
	dir string
}

func newRepo(t *testing.T) *repo {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Fatalf("these tests make repositories with git, which isn't installed: %v", err)
	}
	r := &repo{t: t, dir: t.TempDir()}
	r.git("08:00", "init", "-q", "-b", "master")
	r.git("08:00", "config", "user.email", "me@example.com")
	r.git("08:00", "config", "user.name", "Me")
	r.git("08:00", "remote", "add", "origin", "git@github.com:codeo/yumbi-web.git")
	return r
}

func (r *repo) git(hhmm string, args ...string) string {
	r.t.Helper()
	when := at(hhmm).Format(time.RFC3339)
	cmd := exec.Command("git", args...)
	cmd.Dir = r.dir
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_DATE="+when, "GIT_COMMITTER_DATE="+when, "GIT_CONFIG_NOSYSTEM=1", "HOME="+r.dir, "GIT_EDITOR=true")
	out, err := cmd.CombinedOutput()
	if err != nil {
		r.t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

func (r *repo) commit(hhmm, subject string, more ...string) {
	r.t.Helper()
	if err := os.WriteFile(filepath.Join(r.dir, strings.ReplaceAll(subject, " ", "-")), []byte(subject), 0o644); err != nil {
		r.t.Fatal(err)
	}
	r.git(hhmm, "add", "-A")
	r.git(hhmm, append([]string{"commit", "-q", "-m", subject}, more...)...)
}

func (r *repo) history() ([]Event, []Bullet) {
	r.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	h, err := review.History(ctx, r.dir, day.AddDate(0, 0, -7))
	if err != nil {
		r.t.Fatal(err)
	}
	return FromHistory(h, r.dir)
}

func eventsOn(events []Event, branch string) []string {
	var out []string
	for _, e := range events {
		if e.Branch == branch {
			out = append(out, e.At.In(zone).Format("15:04"))
		}
	}
	return out
}

func bulletsOn(bullets []Bullet, branch string) []string {
	var out []string
	for _, b := range bullets {
		if b.Branch == branch {
			out = append(out, b.Text)
		}
	}
	return out
}

func TestCommitsAndCheckoutsAreEventsOnTheirBranchInTheRepositoryNamedByOrigin(t *testing.T) {
	r := newRepo(t)
	r.commit("09:00", "first")
	r.git("09:30", "checkout", "-q", "-b", "feat/x")
	r.commit("10:00", "added the button")
	r.git("11:00", "checkout", "-q", "master")
	r.git("12:00", "checkout", "-q", "feat/x")
	events, bullets := r.history()
	if got, want := eventsOn(events, "feat/x"), []string{"09:30", "10:00", "12:00"}; !reflect.DeepEqual(got, want) {
		t.Errorf("feat/x events: want %v, got %v", want, got)
	}
	if got, want := eventsOn(events, "master"), []string{"09:00", "11:00"}; !reflect.DeepEqual(got, want) {
		t.Errorf("master events: want %v, got %v", want, got)
	}
	for _, e := range events {
		if e.Repo != "yumbi-web" || e.Folder != r.dir {
			t.Fatalf("want every event in yumbi-web at %s, got %+v", r.dir, e)
		}
	}
	if got, want := bulletsOn(bullets, "feat/x"), []string{"added the button"}; !reflect.DeepEqual(got, want) {
		t.Errorf("feat/x bullets: want %v, got %v", want, got)
	}
}

func TestARebaseIsOneEventAndOneBulletAndKeepsItsCommitsAuthoredDays(t *testing.T) {
	r := newRepo(t)
	r.commit("08:10", "first")
	r.git("08:20", "checkout", "-q", "-b", "feat/x")
	lastWeek := day.AddDate(0, 0, -6).Add(9 * time.Hour).Format(time.RFC3339)
	r.commit("08:30", "last week's work", "--date", lastWeek)
	r.commit("08:40", "today's work")
	r.git("09:00", "checkout", "-q", "master")
	r.commit("09:10", "on master meanwhile")
	r.git("10:00", "checkout", "-q", "feat/x")
	r.git("10:30", "rebase", "-q", "master")
	events, bullets := r.history()
	if got, want := eventsOn(events, "feat/x"), []string{"08:20", "08:30", "08:40", "10:00", "10:30"}; !reflect.DeepEqual(got, want) {
		t.Errorf("feat/x events: want %v, got %v", want, got)
	}
	var texts []string
	for _, b := range bullets {
		if b.Branch == "feat/x" {
			texts = append(texts, b.Text+" @ "+b.At.In(zone).Format("01-02 15:04"))
		}
	}
	want := []string{"last week's work @ 11-20 09:00", "today's work @ 11-26 08:40", "rebased onto master @ 11-26 10:30"}
	if !reflect.DeepEqual(texts, want) {
		t.Errorf("feat/x bullets: want %v, got %v", want, texts)
	}
	d := Summarise(day, nil, events, bullets, 30*time.Minute)
	for _, b := range d.Branches {
		if b.Branch == "feat/x" && !reflect.DeepEqual(b.Bullets, []string{"today's work", "rebased onto master"}) {
			t.Errorf("the day's bullets leave out last week's commit: got %v", b.Bullets)
		}
	}
}

func TestMergesReadAsMergedTheBranch(t *testing.T) {
	r := newRepo(t)
	r.commit("08:10", "first")
	r.git("08:20", "checkout", "-q", "-b", "feat/x")
	r.commit("08:30", "work")
	r.git("09:00", "checkout", "-q", "master")
	r.commit("09:10", "on master meanwhile")
	r.git("10:00", "checkout", "-q", "feat/x")
	r.git("10:30", "merge", "-q", "--no-edit", "master")
	r.git("11:00", "checkout", "-q", "-b", "feat/y", "master")
	r.git("11:10", "merge", "-q", "--ff-only", "feat/x")
	_, bullets := r.history()
	if got, want := bulletsOn(bullets, "feat/x"), []string{"work", "merged master"}; !reflect.DeepEqual(got, want) {
		t.Errorf("feat/x bullets: want %v, got %v", want, got)
	}
	if got, want := bulletsOn(bullets, "feat/y"), []string{"merged feat/x"}; !reflect.DeepEqual(got, want) {
		t.Errorf("feat/y bullets: want %v, got %v", want, got)
	}
}

func TestAnAmendedCommitIsOneBulletAndOthersCommitsAreNone(t *testing.T) {
	r := newRepo(t)
	r.commit("08:10", "first")
	r.commit("08:20", "fix the thing")
	r.git("08:30", "commit", "-q", "--amend", "-m", "fix the thing properly")
	r.commit("08:40", "someone else's", "--author", "Other <other@example.com>")
	_, bullets := r.history()
	if got, want := bulletsOn(bullets, "master"), []string{"first", "fix the thing properly"}; !reflect.DeepEqual(got, want) {
		t.Errorf("want %v, got %v", want, got)
	}
}

func TestHistoryLeavesOutWhatIsOlderThanSince(t *testing.T) {
	r := newRepo(t)
	r.commit("08:10", "first")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	h, err := review.History(ctx, r.dir, at("09:00"))
	if err != nil {
		t.Fatal(err)
	}
	if len(h.Entries) != 0 {
		t.Fatalf("want no entries after 09:00, got %+v", h.Entries)
	}
	if h.UserEmail != "me@example.com" || h.Origin != "git@github.com:codeo/yumbi-web.git" {
		t.Fatalf("want the user and origin read, got %q and %q", h.UserEmail, h.Origin)
	}
}

func TestAheadOfCountsTheCommitsHeadHasThatACommitHasnt(t *testing.T) {
	r := newRepo(t)
	r.commit("08:10", "first")
	base := strings.TrimSpace(r.git("08:10", "rev-parse", "HEAD"))
	r.commit("08:20", "second")
	r.commit("08:30", "third")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	got, err := review.AheadOf(ctx, r.dir, base)
	if err != nil || got != (review.Ahead{Count: 2, Known: true}) {
		t.Fatalf("want 2 ahead, got %+v, %v", got, err)
	}
	got, err = review.AheadOf(ctx, r.dir, strings.Repeat("ab", 20))
	if err != nil || got.Known {
		t.Fatalf("want a commit the repository hasn't to be unknown, got %+v, %v", got, err)
	}
}
