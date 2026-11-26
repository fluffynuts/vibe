package activity

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"vibe/internal/review"
)

func TestTheLogKeepsEachSandboxsMarksByDayAndReadsThemBack(t *testing.T) {
	l := Log{Dir: t.TempDir()}
	one := Mark{At: at("09:00"), Folder: "/code/a", Repo: "a", Branch: "x", Kind: KindAgent}
	two := Mark{At: at("09:01"), Folder: "/code/b", Repo: "b", Branch: "y", Kind: KindPrompt}
	yesterday := Mark{At: day.Add(-time.Hour), Folder: "/code/a", Repo: "a", Branch: "x", Kind: KindPage}
	for sandbox, m := range map[string]Mark{"one": one, "two": two, "old": yesterday} {
		if err := l.Append(sandbox, m); err != nil {
			t.Fatal(err)
		}
	}
	got, unread, err := l.Marks(day.In(time.Local), day.In(time.Local))
	if err != nil || unread != 0 {
		t.Fatalf("want marks read, got %v, %d unread", err, unread)
	}
	if len(got) != 2 {
		t.Fatalf("want today's two marks, got %+v", got)
	}
	if _, err := os.Stat(filepath.Join(l.Dir, at("09:00").Local().Format(time.DateOnly), "one.jsonl")); err != nil {
		t.Fatalf("want a file for the sandbox and day: %v", err)
	}
}

func TestALineOfTheLogThatCantBeReadIsCountedNotFatal(t *testing.T) {
	l := Log{Dir: t.TempDir()}
	if err := l.Append("one", mark("09:00", "r", "b")); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(l.Dir, at("09:00").Local().Format(time.DateOnly), "one.jsonl")
	f, _ := os.OpenFile(file, os.O_APPEND|os.O_WRONLY, 0)
	f.WriteString(`{"at":"2026-11-26T09:0`)
	f.Close()
	got, unread, err := l.Marks(at("09:00").Local(), at("09:00").Local())
	if err != nil || len(got) != 1 || unread != 1 {
		t.Fatalf("want one mark and one unread line, got %d marks, %d unread, %v", len(got), unread, err)
	}
}

func TestTheRecorderKeepsOneMarkOfAKindOnABranchAMinuteUnderOriginsName(t *testing.T) {
	l := Log{Dir: t.TempDir()}
	now := at("09:00")
	var mu sync.Mutex
	r := &Recorder{Log: l, Sandbox: "s", Folder: "/code/yumbi",
		Origin: func(context.Context, string) (string, error) { return "git@github.com:codeo/yumbi-web.git", nil },
		Now:    func() time.Time { mu.Lock(); defer mu.Unlock(); return now }}
	r.Mark(KindAgent, "feat/x")
	r.Mark(KindAgent, "feat/x")
	r.Mark(KindPage, "feat/x")
	r.Mark(KindAgent, "feat/y")
	mu.Lock()
	now = at("09:01")
	mu.Unlock()
	r.Mark(KindAgent, "feat/x")
	r.Wait()
	got, _, _ := l.Marks(now.Local(), now.Local())
	var seen []string
	for _, m := range got {
		if m.Repo != "yumbi-web" || m.Folder != "/code/yumbi" {
			t.Fatalf("want marks in yumbi-web from /code/yumbi, got %+v", m)
		}
		seen = append(seen, m.At.In(zone).Format("15:04")+" "+m.Kind+" "+m.Branch)
	}
	want := []string{"09:00 agent feat/x", "09:00 agent feat/y", "09:00 page feat/x", "09:01 agent feat/x"}
	if !sameSet(seen, want) {
		t.Fatalf("want %v, got %v", want, seen)
	}
}

func TestTheRecorderSaysOnceWhyItCantKeepAMark(t *testing.T) {
	file := filepath.Join(t.TempDir(), "a-file")
	os.WriteFile(file, nil, 0o644)
	var said []string
	now := at("09:00")
	r := &Recorder{Log: Log{Dir: file}, Sandbox: "s", Folder: "/code/r", Now: func() time.Time { return now }, Problem: func(s string) { said = append(said, s) }}
	r.Mark(KindAgent, "b")
	r.Wait()
	r.Mark(KindPage, "b")
	r.Wait()
	if len(said) != 1 || !strings.Contains(said[0], "activity log") {
		t.Fatalf("want one problem said, got %q", said)
	}
}

func sameSet(a, b []string) bool {
	m := map[string]int{}
	for _, s := range a {
		m[s]++
	}
	for _, s := range b {
		m[s]--
	}
	for _, n := range m {
		if n != 0 {
			return false
		}
	}
	return len(a) == len(b)
}

// fakeGit is a desk's view of git: histories and statuses by folder, and GitHub's branches.
type fakeGit struct {
	mu        sync.Mutex
	histories map[string]review.GitHistory
	statuses  map[string]*review.FolderStatus
	errs      map[string]error
	remote    map[string]string
	ahead     map[string]review.Ahead
	noGitHub  bool
	asked     int
	// notRepos are folders in no repository.
	notRepos map[string]bool
}

func (f *fakeGit) desk(l Log, folders ...string) *Desk {
	return &Desk{
		Log:     l,
		Folders: func() []string { return folders },
		History: func(_ context.Context, dir string, _ time.Time) (review.GitHistory, error) {
			f.mu.Lock()
			defer f.mu.Unlock()
			f.asked++
			h := f.histories[dir]
			h.Repository = !f.notRepos[dir]
			return h, f.errs[dir]
		},
		Status: func(_ context.Context, dir string) (*review.FolderStatus, error) {
			return f.statuses[dir], f.errs[dir]
		},
		RemoteHead: func(_ context.Context, web, branch string) (string, bool, error) {
			if f.noGitHub {
				return "", false, ErrNoGitHub
			}
			sha, ok := f.remote[branch]
			return sha, ok, nil
		},
		AheadOf: func(_ context.Context, dir, sha string) (int, bool, error) {
			a, ok := f.ahead[sha]
			return a.Count, ok && a.Known, nil
		},
		Now: func() time.Time { return at("17:00") },
	}
}

func TestTheDayCombinesTheLogWithGitInEveryFolderSeen(t *testing.T) {
	l := Log{Dir: t.TempDir()}
	logged := t.TempDir()
	other := t.TempDir()
	l.Append("s", Mark{At: at("09:00"), Folder: logged, Repo: "yumbi-web", Branch: "feat/x", Kind: KindAgent})
	l.Append("s", Mark{At: at("09:10"), Folder: logged, Repo: "yumbi-web", Branch: "feat/x", Kind: KindAgent})
	f := &fakeGit{histories: map[string]review.GitHistory{
		other: {Origin: "git@github.com:codeo/phoenix.git", Branches: []string{"fix/y"}, UserEmail: "me@x",
			Entries: []review.ReflogEntry{{Ref: "refs/heads/fix/y", New: "c1", At: at("14:00"), Message: "commit: fix the root cause"}},
			Commits: map[string]review.ReflogCommit{"c1": {AuthorEmail: "me@x", AuthorTime: at("14:00"), Subject: "fix the root cause", Parents: 1}}},
	}}
	r, err := f.desk(l, other).Day(context.Background(), "", 30*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if r.Today != "2026-11-26" || r.Earliest != "2026-11-19" || len(r.Problems) != 0 {
		t.Fatalf("want today, a week back and no problems, got %+v", r)
	}
	var got []string
	for _, b := range r.Day.Branches {
		got = append(got, b.Repo+" "+b.Branch+" "+strings.Join(b.Bullets, ","))
	}
	if want := []string{"phoenix fix/y fix the root cause", "yumbi-web feat/x "}; !reflect.DeepEqual(got, want) {
		t.Fatalf("want %v, got %v", want, got)
	}
	if r.Day.Hours != 1 {
		t.Fatalf("want half an hour each, got %v", r.Day.Hours)
	}
}

func TestTheDayIsOnlyOneOfThoseKept(t *testing.T) {
	d := (&fakeGit{}).desk(Log{Dir: t.TempDir()})
	for _, date := range []string{"2026-11-27", "2026-11-18", "yesterday"} {
		if _, err := d.Day(context.Background(), date, time.Hour); err == nil {
			t.Errorf("want %q refused", date)
		}
	}
	if r, err := d.Day(context.Background(), "2026-11-19", time.Hour); err != nil || r.Day.Date != "2026-11-19" {
		t.Errorf("want a week back allowed, got %v, %+v", err, r)
	}
}

func TestAFolderGoneIsLeftOutAndOneUnreadableIsSaid(t *testing.T) {
	gone := filepath.Join(t.TempDir(), "gone")
	broken := t.TempDir()
	f := &fakeGit{errs: map[string]error{broken: errors.New("it is damaged")}}
	r, err := f.desk(Log{Dir: t.TempDir()}, gone, broken).Day(context.Background(), "", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Problems) != 1 || !strings.Contains(r.Problems[0], "it is damaged") {
		t.Fatalf("want the broken folder said, and the gone one not, got %q", r.Problems)
	}
}

func TestAFoldersHistoryIsReadAtMostOnceAMinute(t *testing.T) {
	dir := t.TempDir()
	f := &fakeGit{}
	d := f.desk(Log{Dir: t.TempDir()}, dir)
	d.Day(context.Background(), "", time.Hour)
	d.Day(context.Background(), "2026-11-25", time.Hour)
	if f.asked != 1 {
		t.Fatalf("want one read, got %d", f.asked)
	}
}

func TestNeedsAttentionSaysUncommittedChangesAndCommitsNotPushed(t *testing.T) {
	dirty, ahead, never, clean := t.TempDir(), t.TempDir(), t.TempDir(), t.TempDir()
	f := &fakeGit{noGitHub: true, statuses: map[string]*review.FolderStatus{
		dirty: {Branch: "feat/x", Modified: 1},
		ahead: {Branch: "fix/y", Ahead: 3},
		never: {Branch: "feat/new", NoUpstream: true},
		clean: {Branch: "master"},
	}}
	got, problems := f.desk(Log{Dir: t.TempDir()}, dirty, ahead, never, clean).Attention(context.Background())
	var said []string
	for _, a := range got {
		said = append(said, a.Branch+": "+a.What)
	}
	want := []string{"feat/x: uncommitted changes: please commit and push", "fix/y: 3 commits not pushed", "feat/new: never pushed"}
	if !sameSet(said, want) || len(problems) != 0 {
		t.Fatalf("want %v, got %v (problems %v)", want, said, problems)
	}
}

func TestNeedsAttentionAsksGitHubBeforeSayingCommitsAreNotPushed(t *testing.T) {
	pushed, behind, never := t.TempDir(), t.TempDir(), t.TempDir()
	f := &fakeGit{
		statuses: map[string]*review.FolderStatus{
			pushed: {Branch: "pushed-by-url", Ahead: 2, Web: "https://github.com/o/r"},
			behind: {Branch: "partly", Ahead: 3, Web: "https://github.com/o/r"},
			never:  {Branch: "new", NoUpstream: true, Web: "https://github.com/o/r"},
		},
		remote: map[string]string{"pushed-by-url": "head", "partly": "older"},
		ahead:  map[string]review.Ahead{"head": {Count: 0, Known: true}, "older": {Count: 1, Known: true}},
	}
	d := f.desk(Log{Dir: t.TempDir()}, pushed, behind, never)
	first, _ := d.Attention(context.Background())
	for _, a := range first {
		if !a.Checking {
			t.Fatalf("want each unpushed branch said to be checked with GitHub at first, got %+v", a)
		}
	}
	d.WaitForGitHub()
	then, _ := d.Attention(context.Background())
	var said []string
	for _, a := range then {
		said = append(said, a.Branch+": "+a.What)
	}
	if want := []string{"partly: 1 commit not pushed", "new: never pushed"}; !sameSet(said, want) {
		t.Fatalf("want %v, got %v", want, said)
	}
}

func TestAFolderInNoRepositoryIsNeitherAProblemNorNeedsAttention(t *testing.T) {
	plain := t.TempDir()
	f := &fakeGit{notRepos: map[string]bool{plain: true}, errs: map[string]error{plain: nil},
		statuses: map[string]*review.FolderStatus{}}
	d := f.desk(Log{Dir: t.TempDir()}, plain)
	r, err := d.Day(context.Background(), "", time.Hour)
	if err != nil || len(r.Problems) != 0 {
		t.Fatalf("want no problems, got %v, %q", err, r.Problems)
	}
	if got, problems := d.Attention(context.Background()); len(got) != 0 || len(problems) != 0 {
		t.Fatalf("want nothing needing attention, got %+v, %q", got, problems)
	}
}
