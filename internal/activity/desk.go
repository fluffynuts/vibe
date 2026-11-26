package activity

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"vibe/internal/pathresolve"
	"vibe/internal/review"
)

// HistoryDays is how far back the page can step: about a week, as the user looks once a day.
const HistoryDays = 7

// Roundings are the steps a branch's day can be rounded up to, as the setting names them; DefaultRounding is the one
// used when none is set.
var Roundings = map[string]time.Duration{"15m": 15 * time.Minute, "30m": 30 * time.Minute, "1h": time.Hour}

const DefaultRounding = "30m"

// historyEvery is how long a folder's git history is used before it is read again: reading it is a helper process for
// each folder. statusEvery, the same for its status, which Needs attention shows as it is now.
const (
	historyEvery = time.Minute
	statusEvery  = 15 * time.Second
	remoteEvery  = time.Minute
)

// ErrNoGitHub is Desk.RemoteHead's answer when it can't ask GitHub (no gh): the local copy of the remote is all there is.
var ErrNoGitHub = errors.New("can't ask GitHub")

// Desk reads the work of a day, for the Activity tab, from the log and the git history of every folder vibe has seen.
type Desk struct {
	Log Log
	// Folders are the folders of the sandboxes vibe has now: those seen in the log are added.
	Folders func() []string
	History func(ctx context.Context, dir string, since time.Time) (review.GitHistory, error)
	Status  func(ctx context.Context, dir string) (*review.FolderStatus, error)
	// RemoteHead is the commit branch is at on GitHub (web is the repository's address there), found false when GitHub
	// has no such branch; ErrNoGitHub when it can't be asked. Nil means it isn't.
	RemoteHead func(ctx context.Context, web, branch string) (sha string, found bool, err error)
	// AheadOf is how many commits the folder's HEAD has that sha hasn't, known false when the folder hasn't sha.
	AheadOf func(ctx context.Context, dir, sha string) (ahead int, known bool, err error)
	Now     func() time.Time

	mu      sync.Mutex
	history map[string]cachedHistory
	status  map[string]cachedStatus
	remote  map[string]*remoteLook
	// looking is the remote looks under way, for tests to wait on.
	looking sync.WaitGroup
}

type cachedHistory struct {
	at    time.Time
	since time.Time
	h     review.GitHistory
	err   error
}

type cachedStatus struct {
	at  time.Time
	st  *review.FolderStatus
	err error
}

type remoteLook struct {
	at, sha string
	asked   time.Time
	ahead   int
	known   bool
	done    bool
	found   bool
	err     error
}

// Report is a day's work for the page.
type Report struct {
	Day Day `json:"day"`
	// Today and Earliest bound the days the page steps through.
	Today    string `json:"today"`
	Earliest string `json:"earliest"`
	// Problems are what couldn't be read, in words.
	Problems []string `json:"problems"`
}

func (d *Desk) now() time.Time {
	if d.Now != nil {
		return d.Now()
	}
	return time.Now()
}

// Day is the work of date (YYYY-MM-DD, local; "" for today), each branch rounded up to step.
func (d *Desk) Day(ctx context.Context, date string, step time.Duration) (Report, error) {
	now := d.now()
	today := midnightOf(now)
	earliest := today.AddDate(0, 0, -HistoryDays)
	midnight := today
	if date != "" {
		t, err := time.ParseInLocation(time.DateOnly, date, now.Location())
		if err != nil {
			return Report{}, fmt.Errorf("%q isn't a day (YYYY-MM-DD)", date)
		}
		midnight = t
	}
	if midnight.After(today) || midnight.Before(earliest) {
		return Report{}, fmt.Errorf("%s is outside the days kept (%s to %s)", midnight.Format(time.DateOnly), earliest.Format(time.DateOnly), today.Format(time.DateOnly))
	}
	r := Report{Today: today.Format(time.DateOnly), Earliest: earliest.Format(time.DateOnly), Problems: []string{}}
	// The day before is read too: a stretch begun then can run past midnight.
	marks, unread, err := d.Log.Marks(midnight.AddDate(0, 0, -1), midnight)
	if err != nil {
		return Report{}, err
	}
	if unread > 0 {
		r.Problems = append(r.Problems, fmt.Sprintf("%d lines of the activity log couldn't be read (one cut short when vibe was stopped writing it?), and are left out", unread))
	}
	var events []Event
	var bullets []Bullet
	for _, f := range d.knownFolders(earliest) {
		h, err := d.historyOf(ctx, f, earliest.AddDate(0, 0, -1))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			r.Problems = append(r.Problems, fmt.Sprintf("couldn't read git in %s: %v", pathresolve.Tilde(f), err))
			continue
		}
		e, b := FromHistory(h, f)
		events, bullets = append(events, e...), append(bullets, b...)
	}
	r.Day = Summarise(midnight, marks, events, bullets, step)
	for i := range r.Day.Branches {
		for j, f := range r.Day.Branches[i].Folders {
			r.Day.Branches[i].Folders[j] = pathresolve.Tilde(f)
		}
	}
	return r, nil
}

// knownFolders are the folders vibe has now, and those in the log since since.
func (d *Desk) knownFolders(since time.Time) []string {
	seen := map[string]bool{}
	if d.Folders != nil {
		for _, f := range d.Folders() {
			seen[f] = true
		}
	}
	if marks, _, err := d.Log.Marks(since, d.now()); err == nil {
		for _, m := range marks {
			seen[m.Folder] = true
		}
	}
	return sortedKeys(seen)
}

func (d *Desk) historyOf(ctx context.Context, dir string, since time.Time) (review.GitHistory, error) {
	d.mu.Lock()
	if d.history == nil {
		d.history = map[string]cachedHistory{}
	}
	c, ok := d.history[dir]
	d.mu.Unlock()
	if ok && c.since.Equal(since) && d.now().Sub(c.at) < historyEvery {
		return c.h, c.err
	}
	if _, err := os.Stat(dir); err != nil {
		return review.GitHistory{}, err
	}
	h, err := d.History(ctx, dir, since)
	d.mu.Lock()
	d.history[dir] = cachedHistory{at: d.now(), since: since, h: h, err: err}
	d.mu.Unlock()
	return h, err
}

// Attention is something about a folder that wants doing now: changes to commit, or commits to push.
type Attention struct {
	Folder string `json:"folder"`
	Branch string `json:"branch"`
	What   string `json:"what"`
	// Checking says GitHub is being asked whether the commits are there already: the local copy of it can be out of date.
	Checking bool `json:"checking,omitempty"`
}

// Attention is what wants doing now in each folder vibe has seen this week: it is git's state now, whichever day the page
// shows, as git keeps no record of what was uncommitted before.
func (d *Desk) Attention(ctx context.Context) ([]Attention, []string) {
	out := []Attention{}
	problems := []string{}
	since := midnightOf(d.now()).AddDate(0, 0, -HistoryDays)
	for _, f := range d.knownFolders(since) {
		if h, err := d.historyOf(ctx, f, since.AddDate(0, 0, -1)); err == nil && !h.Repository {
			continue
		}
		st, err := d.statusOf(ctx, f)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			problems = append(problems, fmt.Sprintf("couldn't read git's status in %s: %v", pathresolve.Tilde(f), err))
			continue
		}
		folder := pathresolve.Tilde(f)
		if st.Staged+st.Modified+st.Untracked+st.Conflicted > 0 {
			out = append(out, Attention{Folder: folder, Branch: st.Branch, What: "uncommitted changes: please commit and push"})
		}
		if st.Detached || (st.Ahead == 0 && !st.NoUpstream) {
			continue
		}
		a := Attention{Folder: folder, Branch: st.Branch, What: unpushed(st.Ahead, st.NoUpstream)}
		if look := d.remoteLookFor(f, st); look != nil {
			switch {
			case !look.done:
				a.Checking = true
			case look.err != nil:
				problems = append(problems, fmt.Sprintf("couldn't ask GitHub whether %s's commits in %s are there already: %v", st.Branch, folder, look.err))
			case look.found && look.known && look.ahead == 0:
				continue
			case look.found && look.known:
				a.What = unpushed(look.ahead, false)
			}
		}
		out = append(out, a)
	}
	return out, problems
}

func unpushed(ahead int, noUpstream bool) string {
	switch {
	case noUpstream:
		return "never pushed"
	case ahead == 1:
		return "1 commit not pushed"
	default:
		return fmt.Sprintf("%d commits not pushed", ahead)
	}
}

func (d *Desk) statusOf(ctx context.Context, dir string) (*review.FolderStatus, error) {
	d.mu.Lock()
	if d.status == nil {
		d.status = map[string]cachedStatus{}
	}
	c, ok := d.status[dir]
	d.mu.Unlock()
	if ok && d.now().Sub(c.at) < statusEvery {
		return c.st, c.err
	}
	if _, err := os.Stat(dir); err != nil {
		return nil, err
	}
	st, err := d.Status(ctx, dir)
	d.mu.Lock()
	d.status[dir] = cachedStatus{at: d.now(), st: st, err: err}
	d.mu.Unlock()
	return st, err
}

// remoteLookFor is what GitHub said, or is being asked, of the folder's branch: nil when it can't be asked. A look is
// started in the background when there's none for the branch as it is now, or it is old.
func (d *Desk) remoteLookFor(dir string, st *review.FolderStatus) *remoteLook {
	if d.RemoteHead == nil || d.AheadOf == nil || st.Web == "" {
		return nil
	}
	key := dir + "\x00" + st.Branch
	at := fmt.Sprintf("%d/%t", st.Ahead, st.NoUpstream)
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.remote == nil {
		d.remote = map[string]*remoteLook{}
	}
	look := d.remote[key]
	if look != nil && look.done && errors.Is(look.err, ErrNoGitHub) {
		return nil
	}
	if look == nil || (look.done && (look.at != at || d.now().Sub(look.asked) >= remoteEvery)) {
		fresh := &remoteLook{at: at, asked: d.now()}
		if look != nil && look.done && look.at == at {
			// Asked again only to be sure: what it said stands meanwhile.
			fresh.done, fresh.found, fresh.known, fresh.ahead, fresh.err = true, look.found, look.known, look.ahead, look.err
		}
		look = fresh
		d.remote[key] = look
		d.looking.Add(1)
		go d.askRemote(look, dir, st.Web, st.Branch)
	}
	cp := *look
	return &cp
}

func (d *Desk) askRemote(look *remoteLook, dir, web, branch string) {
	defer d.looking.Done()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	sha, found, err := d.RemoteHead(ctx, web, branch)
	ahead, known := 0, false
	if err == nil && found {
		ahead, known, err = d.AheadOf(ctx, dir, sha)
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	look.done, look.sha, look.found, look.ahead, look.known, look.err = true, sha, found, ahead, known, err
}

// WaitForGitHub waits for the looks at GitHub under way.
func (d *Desk) WaitForGitHub() { d.looking.Wait() }
