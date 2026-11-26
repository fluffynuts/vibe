package activity

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Log is vibe's own record of the work it saw: a file of marks, one JSON line each, for each sandbox and day
// (<Dir>/<YYYY-MM-DD>/<sandbox>.jsonl). Each companion writes only its own sandbox's file, so two never write to one.
type Log struct {
	Dir string
}

// maxLogLine is the longest line read: a mark is far shorter.
const maxLogLine = 64 << 10

// Append adds m to sandbox's file for m's local day.
func (l Log) Append(sandbox string, m Mark) error {
	dir := filepath.Join(l.Dir, m.At.Local().Format(time.DateOnly))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	line, err := json.Marshal(m)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(dir, sandbox+".jsonl"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, werr := f.Write(append(line, '\n'))
	return errors.Join(werr, f.Close())
}

// Marks are the marks of every sandbox on the local days from from's to to's, and how many lines couldn't be read (a
// line cut short when vibe was stopped writing it).
func (l Log) Marks(from, to time.Time) (marks []Mark, unread int, err error) {
	for d := midnightOf(from); !d.After(to); d = d.AddDate(0, 0, 1) {
		files, err := filepath.Glob(filepath.Join(l.Dir, d.Format(time.DateOnly), "*.jsonl"))
		if err != nil {
			return nil, 0, err
		}
		for _, file := range files {
			got, bad, err := readMarks(file)
			if err != nil {
				return nil, 0, fmt.Errorf("can't read the activity log %s: %w", file, err)
			}
			marks, unread = append(marks, got...), unread+bad
		}
	}
	return marks, unread, nil
}

func readMarks(file string) (marks []Mark, unread int, err error) {
	f, err := os.Open(file)
	if err != nil {
		return nil, 0, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 4096), maxLogLine)
	for sc.Scan() {
		var m Mark
		if json.Unmarshal(sc.Bytes(), &m) != nil || m.At.IsZero() {
			unread++
			continue
		}
		marks = append(marks, m)
	}
	return marks, unread, sc.Err()
}

func midnightOf(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, t.Location())
}

// The kinds of mark.
const (
	KindPrompt = "prompt"
	KindAgent  = "agent"
	KindPage   = "page"
	KindReview = "review"
	KindGit    = "git"
)

// Recorder writes a sandbox's marks to the log, at most one of each kind on each branch a minute: the page reports
// often, and the minute is all the time model counts in.
type Recorder struct {
	Log     Log
	Sandbox string
	Folder  string
	// Origin is the URL of the folder's origin remote, "" for none; it may be slow (it reads the repository).
	Origin func(ctx context.Context, dir string) (string, error)
	// Now is the clock; nil means time.Now.
	Now func() time.Time
	// Problem is told when a mark can't be kept, once for each different problem; nil means nowhere.
	Problem func(string)

	mu       sync.Mutex
	last     map[string]time.Time
	repo     string
	repoAt   time.Time
	reported string
	// writing is the marks being written, for tests to wait on.
	writing sync.WaitGroup
}

// repoEvery is how often the folder's origin is read again for its name.
const repoEvery = 5 * time.Minute

// Mark records kind of work on branch now, unless one was recorded this minute. It doesn't wait for the writing.
func (r *Recorder) Mark(kind, branch string) {
	now := r.now()
	r.mu.Lock()
	if r.last == nil {
		r.last = map[string]time.Time{}
	}
	key := kind + "\x00" + branch
	if last, ok := r.last[key]; ok && now.Sub(last) < time.Minute {
		r.mu.Unlock()
		return
	}
	r.last[key] = now
	r.writing.Add(1)
	r.mu.Unlock()
	go func() {
		defer r.writing.Done()
		m := Mark{At: now, Folder: r.Folder, Repo: r.repoName(), Branch: branch, Kind: kind}
		if err := r.Log.Append(r.Sandbox, m); err != nil {
			r.problem("couldn't keep a mark in the activity log: " + err.Error())
		}
	}()
}

// Wait waits for the marks being written.
func (r *Recorder) Wait() { r.writing.Wait() }

func (r *Recorder) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

func (r *Recorder) repoName() string {
	r.mu.Lock()
	if r.repo != "" && r.now().Sub(r.repoAt) < repoEvery {
		defer r.mu.Unlock()
		return r.repo
	}
	r.mu.Unlock()
	origin := ""
	if r.Origin != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		var err error
		origin, err = r.Origin(ctx, r.Folder)
		cancel()
		if err != nil {
			r.problem("couldn't read the folder's origin for the activity log, so its marks are under the folder's name: " + err.Error())
		}
	}
	name := RepoName(origin, r.Folder)
	r.mu.Lock()
	r.repo, r.repoAt = name, r.now()
	r.mu.Unlock()
	return name
}

func (r *Recorder) problem(why string) {
	r.mu.Lock()
	same := why == r.reported
	r.reported = why
	r.mu.Unlock()
	if !same && r.Problem != nil {
		r.Problem(why)
	}
}
