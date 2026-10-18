package review

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"vibe/internal/fscopy"
)

// A review's State.
const (
	InProgress = "in progress"
	Done       = "done"
)

// A thread's Status.
const (
	Open      = "open"
	Resolved  = "resolved"
	Dismissed = "dismissed"
)

// Who said something.
const (
	Agent = "agent"
	User  = "user"
)

// maxProblems is how many problems with requests a review keeps to show.
const maxProblems = 20

// maxRequest is the most of a request in the inbox that's read: the
// agent's command writes them small; anything larger isn't one.
const maxRequest = 1 << 20

// maxReview bounds reading review.json: far more than any review.
var maxReview int64 = 256 << 20

// maxTakeAtOnce is how many of the agent's requests are taken in at once:
// the rest wait for the next time, so a flood of them can't take the host's
// memory.
const maxTakeAtOnce = 100

// maxBody is the most one comment can say. How large the review grows is
// held to maxReview by Take, which refuses a request that would take it
// past.
const maxBody = 256 << 10

// Review is a review of a diff: the agent's comments on it, the user's
// replies, and the order to read it in.
type Review struct {
	// Ref is what the diff is of: "" for the working tree, else a branch
	// or commit the agent named when it started.
	Ref string `json:"ref,omitempty"`
	// Commit, when set, is the one commit the review is of.
	Commit string `json:"commit,omitempty"`
	// Unpushed says the review is of the commits Ref (the branch's
	// upstream) hasn't: what they changed, without what's uncommitted.
	Unpushed bool `json:"unpushed,omitempty"`
	// Closed is why the review was closed (by the user, or by itself when
	// there was nothing left to review); "" while it's open.
	Closed string `json:"closed,omitempty"`
	// Reopened says the user reopened the review: it's theirs to read, and
	// doesn't close itself again (see CloseOver).
	Reopened bool `json:"reopened,omitempty"`
	// Note says what's being reviewed, as the agent put it.
	Note string `json:"note,omitempty"`
	// State is "" before a review is started, then InProgress, then Done.
	State   string   `json:"state,omitempty"`
	Threads []Thread `json:"threads"`
	Order   []Stop   `json:"order,omitempty"`
	// Problems are requests that couldn't be carried out, and why: shown,
	// as nobody saw them fail.
	Problems []string  `json:"problems,omitempty"`
	Started  time.Time `json:"started,omitempty"`
	// Viewed is the files the user has ticked as viewed: path to the
	// fingerprint of the change they saw (a File's), so one changed since
	// can be told.
	Viewed map[string]string `json:"viewed,omitempty"`
	// AgentAt is when the agent's requests were last taken in: the page
	// marks the tab when it's later than the user has seen.
	AgentAt time.Time `json:"agentAt,omitempty"`
	// LastID is the last thread's ID: IDs are never reused.
	LastID int `json:"lastId"`
	// Taken are the agent's last requests taken in, by the name of their
	// file in the inbox, and why one couldn't be carried out: so its command
	// can tell what became of its own (see maxTaken).
	Taken []Taken `json:"taken,omitempty"`
}

// Taken is what became of a request: its file's name, and the problem it
// met ("" if none).
type Taken struct {
	Name    string `json:"name"`
	Problem string `json:"problem,omitempty"`
}

// maxTaken is how many requests' outcomes are kept: far more than are
// taken in between two looks of the agent's command.
const maxTaken = 200

// Thread is a comment on lines of the diff (File "" for one on the whole
// review), and the replies to it.
type Thread struct {
	ID      int    `json:"id"`
	File    string `json:"file,omitempty"`
	Side    string `json:"side,omitempty"` // "new" or "old"
	Line    int    `json:"line,omitempty"`
	EndLine int    `json:"endLine,omitempty"`
	// MovedFrom is the line the comment was made on, when it has since
	// followed its code (lines above it came or went); 0 if it hasn't.
	MovedFrom int `json:"movedFrom,omitempty"`
	// Code is the text of the lines when the comment was made, to tell
	// when they've changed since.
	Code     string    `json:"code,omitempty"`
	Status   string    `json:"status"`
	Comments []Comment `json:"comments"`
}

// Body is what the thread was started with.
func (t Thread) Body() string {
	if len(t.Comments) == 0 {
		return ""
	}
	return t.Comments[0].Body
}

// Comment is one thing said in a thread.
type Comment struct {
	Author string    `json:"author"`
	Body   string    `json:"body"`
	At     time.Time `json:"at"`
	// Sent says the agent has been given a comment of the user's (with Ask
	// the agent, or Submit to agent); until then it can be deleted. Edited
	// after, it's unsent again, to be sent saying what changed (see Was).
	// The agent's own are never anything but sent.
	Sent bool `json:"sent,omitempty"`
	// Was is what the agent saw of a comment of the user's edited since,
	// oldest first: an edited comment is sent again, saying what changed.
	Was []string `json:"was,omitempty"`
}

// Stop is a place in the reading order: where to look, and why there.
type Stop struct {
	File    string `json:"file"`
	Line    int    `json:"line,omitempty"`
	EndLine int    `json:"endLine,omitempty"`
	Body    string `json:"body,omitempty"`
	Label   string `json:"label,omitempty"`
}

// Request is one thing to do to the review: dropped in the inbox by the
// agent's command, or made from the page.
type Request struct {
	Op     string `json:"op"`
	Ref    string `json:"ref,omitempty"`
	Commit string `json:"commit,omitempty"`
	Note   string `json:"note,omitempty"`
	// Unpushed is start's: a review of the commits Ref hasn't.
	Unpushed bool `json:"unpushed,omitempty"`
	// Reviewing is the page's start's: the agent has been asked to review
	// it, so it's in progress; else it's just the diff, for the user.
	Reviewing bool   `json:"reviewing,omitempty"`
	File      string `json:"file,omitempty"`
	Side      string `json:"side,omitempty"`
	Line      int    `json:"line,omitempty"`
	EndLine   int    `json:"endLine,omitempty"`
	Code      string `json:"code,omitempty"`
	Thread    int    `json:"thread,omitempty"`
	// At is when the comment edit or delete is of was said: what it's
	// known by, as its place on the thread moves when one before it goes.
	At    time.Time `json:"at,omitempty"`
	Body  string    `json:"body,omitempty"`
	Stops []Stop    `json:"stops,omitempty"`
}

// userOps are what the user can do from the page: the rest is the agent's.
var userOps = map[string]bool{"view": true, "unview": true, "start": true, "close": true, "reopen-review": true, "comment": true, "reply": true, "resolve": true, "dismiss": true, "reopen": true, "edit": true, "delete": true}

// Store keeps a sandbox's review in dir (~/.vibe/reviews/<sandbox>): the
// review in review.json, of which it's the only writer, and a copy of it in
// view/ for the agent's command to read; and an inbox that command drops
// requests into. Only the inbox (read-write) and view/ (read-only) are
// mounted into the sandbox: what's in them is the agent's to make, so
// nothing there is read but plain files, nor written but afresh.
type Store struct {
	dir string
	mu  sync.Mutex
	now func() time.Time
	// readFile reads a request from the inbox (readRequest), which a test
	// stands in for, as no platform can be relied on for a file that can't
	// be read (Windows has no such permission).
	readFile func(string) ([]byte, error)
}

// NewStore is the store in dir.
func NewStore(dir string) *Store {
	return &Store{dir: dir, now: time.Now, readFile: readRequest}
}

// Inbox is the folder the agent's requests are dropped in.
func (s *Store) Inbox() string { return filepath.Join(s.dir, "inbox") }

// View is the folder the review is copied to for the agent to read.
func (s *Store) View() string { return filepath.Join(s.dir, "view") }

func readRequest(path string) ([]byte, error) { return readPlain(path, maxRequest) }

// readPlain reads the plain file at path, up to limit bytes: not a link
// there, nor anything that isn't a file (a FIFO would wait forever).
func readPlain(path string, limit int64) ([]byte, error) {
	f, err := openNoFollow(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("it isn't a plain file")
	}
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, errTooLarge{limit}
	}
	return data, nil
}

// errTooLarge is a file over the limit it's read up to.
type errTooLarge struct{ limit int64 }

func (e errTooLarge) Error() string { return fmt.Sprintf("it's too large (over %d bytes)", e.limit) }

// reviewSize is how large r is, saved: what Take holds under maxReview,
// an eighth of it left for what's noted as it goes (problems, the taken).
func reviewSize(r Review) int64 {
	data, _ := json.MarshalIndent(r, "", "  ")
	return int64(len(data))
}

// Unreadable is a review that can't be read for what's in it (corrupt, or
// too large): one StartAfresh can set aside. A review that can't be read
// for the disk (its permissions, say) isn't one: that's the machine's to
// put right.
type Unreadable struct{ error }

func (u Unreadable) Unwrap() error { return u.error }

// StartAfresh sets aside a review that can't be read (see Unreadable),
// keeping it in history/ as broken-<time>.json, and begins an empty one.
// The inbox is left as it is: what the agent has dropped lands in the new
// review. A review that reads isn't set aside.
func (s *Store) StartAfresh() (Review, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.load(); !errors.As(err, &Unreadable{}) {
		if err == nil {
			return Review{}, Refused{fmt.Errorf("the review reads as it is: there's nothing to start afresh from")}
		}
		return Review{}, err
	}
	history := filepath.Join(s.dir, "history")
	if err := os.MkdirAll(history, 0o700); err != nil {
		return Review{}, fmt.Errorf("the review couldn't be set aside in %s: %w", history, err)
	}
	aside := filepath.Join(history, "broken-"+s.now().UTC().Format("20060102-150405.000000000")+".json")
	if err := os.Rename(s.file(), aside); err != nil {
		return Review{}, fmt.Errorf("the review couldn't be set aside in %s: %w", history, err)
	}
	r := Review{Threads: []Thread{}}
	if err := s.save(r); err != nil {
		return Review{}, fmt.Errorf("a fresh review couldn't be written in %s: %w", s.dir, err)
	}
	return r, nil
}

// failed is a request the store couldn't carry out for its own reasons (a
// disk problem): not one it refused (see Refused).
type failed struct{ error }

func (s *Store) file() string { return filepath.Join(s.dir, "review.json") }

// Load is the review as last written; an empty one when there's none.
func (s *Store) Load() (Review, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.load()
}

func (s *Store) load() (Review, error) {
	r := Review{Threads: []Thread{}}
	data, err := readPlain(s.file(), maxReview)
	if os.IsNotExist(err) {
		return r, nil
	}
	if errors.As(err, &errTooLarge{}) {
		return r, Unreadable{fmt.Errorf("the review in %s is too large to load (over %d MB): move it aside to start afresh (Start afresh on the Review tab does, keeping it in history/)", s.file(), maxReview>>20)}
	}
	if err != nil {
		return r, err
	}
	if err := json.Unmarshal(data, &r); err != nil {
		return r, Unreadable{fmt.Errorf("the review in %s can't be read (%v): Start afresh on the Review tab sets it aside, keeping it in history/", s.file(), err)}
	}
	if r.Threads == nil {
		r.Threads = []Thread{}
	}
	sentIfAnswered(&r)
	return r, nil
}

// sentIfAnswered marks sent the user's comments the agent has answered
// since: a review kept before comments were marked sent has them all
// unsent, and Submit would give the agent them again.
func sentIfAnswered(r *Review) {
	for ti := range r.Threads {
		cs := r.Threads[ti].Comments
		answered := false
		for i := len(cs) - 1; i >= 0; i-- {
			if cs[i].Author == Agent {
				answered = true
			} else if answered {
				cs[i].Sent = true
			}
		}
	}
}

// Take takes in what the agent has dropped in the inbox, oldest first
// (they're named to sort so), and gives the review as it then is. A
// request that can't be carried out is noted in the review's Problems, and
// taken out of the inbox all the same.
func (s *Store) Take() (Review, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, err := s.load()
	if err != nil {
		return r, err
	}
	if err := s.viewed(r); err != nil {
		return r, err
	}
	entries, err := os.ReadDir(s.Inbox())
	if os.IsNotExist(err) {
		return r, nil
	}
	if err != nil {
		return r, err
	}
	names := []string{}
	for _, e := range entries {
		// Directories aside, anything named as a request is taken out,
		// read or not: what isn't a plain file is said to be.
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".json") {
			names = append(names, e.Name())
		}
	}
	if len(names) == 0 {
		return r, nil
	}
	sort.Strings(names)
	if len(names) > maxTakeAtOnce {
		names = names[:maxTakeAtOnce]
	}
	size := reviewSize(r)
	for _, name := range names {
		path := filepath.Join(s.Inbox(), name)
		var req Request
		problem := ""
		if data, err := s.readFile(path); err != nil {
			problem = fmt.Sprintf("%s couldn't be read: %v", name, err)
		} else if err := json.Unmarshal(data, &req); err != nil {
			problem = fmt.Sprintf("%s isn't a request: %v", name, err)
		} else if size+int64(len(data)) > maxReview-maxReview/8 {
			problem = fmt.Sprintf("%s: it would make the review too large (over %d MB): resolve or dismiss what's done with, or start a new review", req.Op, maxReview>>20)
		} else if err := s.apply(&r, req, Agent); err != nil {
			problem = fmt.Sprintf("%s: %v", req.Op, err)
		} else {
			size += int64(len(data))
		}
		if problem != "" {
			s.problem(&r, problem)
		}
		r.Taken = append(r.Taken, Taken{Name: name, Problem: problem})
		if over := len(r.Taken) - maxTaken; over > 0 {
			r.Taken = r.Taken[over:]
		}
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			s.problem(&r, fmt.Sprintf("%s couldn't be taken out of the inbox, so it may be taken in again: %v", name, err))
		}
	}
	r.AgentAt = s.now()
	return r, s.save(r)
}

// Refused is a request the review won't take (a reply to a thread that
// isn't there, say): the asker's to put right, unlike the review failing to
// be read or kept.
type Refused struct{ error }

func (r Refused) Unwrap() error { return r.error }

// Do carries out req, from author, and gives the review as it then is. A
// request that can't be carried out is Refused.
func (s *Store) Do(req Request, author string) (Review, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if author == User && !userOps[req.Op] {
		return Review{}, Refused{fmt.Errorf("%q is the agent's to do, not the page's", req.Op)}
	}
	r, err := s.load()
	if err != nil {
		return r, err
	}
	if err := s.apply(&r, req, author); err != nil {
		var f failed
		if errors.As(err, &f) {
			return r, f.error
		}
		return r, Refused{err}
	}
	return r, s.save(r)
}

func (s *Store) problem(r *Review, text string) {
	r.Problems = append(r.Problems, text)
	if over := len(r.Problems) - maxProblems; over > 0 {
		r.Problems = r.Problems[over:]
	}
}

func (s *Store) apply(r *Review, req Request, author string) error {
	say := func(body string) Comment { return Comment{Author: author, Body: body, At: s.now()} }
	if len(req.Body) > maxBody {
		return fmt.Errorf("what it says is too long (over %d KB)", maxBody>>10)
	}
	switch req.Op {
	case "start":
		// What a review is of reaches git: nothing in its place may be taken
		// for one of git's options.
		for _, ref := range []string{req.Ref, req.Commit} {
			if strings.HasPrefix(ref, "-") || strings.ContainsAny(ref, " \t\n\r\x00") {
				return fmt.Errorf("%q isn't a branch or commit", ref)
			}
		}
		if err := s.keep(*r); err != nil {
			return failed{fmt.Errorf("the last review couldn't be kept in history/: %w", err)}
		}
		state := InProgress
		if author == User && !req.Reviewing {
			state = ""
		}
		*r = Review{Ref: req.Ref, Commit: req.Commit, Unpushed: req.Unpushed, Note: req.Note, State: state, Threads: []Thread{}, Started: s.now(), LastID: r.LastID, Taken: r.Taken}
	case "view", "unview":
		if author != User {
			return fmt.Errorf("ticking files as viewed is the user's to do")
		}
		if req.File == "" {
			return fmt.Errorf("which file?")
		}
		if req.Op == "unview" {
			delete(r.Viewed, req.File)
			break
		}
		if req.Code == "" {
			return fmt.Errorf("a file ticked as viewed needs the fingerprint of the change seen")
		}
		if r.Viewed == nil {
			r.Viewed = map[string]string{}
		}
		r.Viewed[req.File] = req.Code
	case "close", "reopen-review":
		if author != User {
			return fmt.Errorf("closing or reopening the review is the user's to do")
		}
		if req.Op == "reopen-review" {
			r.Closed, r.Reopened = "", true
			break
		}
		r.Closed = req.Body
		if r.Closed == "" {
			r.Closed = "Closed."
		}
	case "comment":
		if req.File == "" || req.Line < 1 || strings.TrimSpace(req.Body) == "" {
			return fmt.Errorf("a comment needs a file, a line and something to say")
		}
		side := req.Side
		if side == "" {
			side = "new"
		}
		if side != "new" && side != "old" {
			return fmt.Errorf("side is new or old, not %q", side)
		}
		end := req.EndLine
		if end < req.Line {
			end = req.Line
		}
		r.LastID++
		r.Threads = append(r.Threads, Thread{ID: r.LastID, File: req.File, Side: side, Line: req.Line, EndLine: end,
			Code: req.Code, Status: Open, Comments: []Comment{say(req.Body)}})
	case "general":
		if strings.TrimSpace(req.Body) == "" {
			return fmt.Errorf("a general comment needs something to say")
		}
		r.LastID++
		r.Threads = append(r.Threads, Thread{ID: r.LastID, Status: Open, Comments: []Comment{say(req.Body)}})
	case "reply", "resolve", "dismiss", "reopen":
		t := r.thread(req.Thread)
		if t == nil {
			return fmt.Errorf("there's no thread %d", req.Thread)
		}
		if req.Op == "reply" && strings.TrimSpace(req.Body) == "" {
			return fmt.Errorf("a reply needs something to say")
		}
		if strings.TrimSpace(req.Body) != "" {
			t.Comments = append(t.Comments, say(req.Body))
		}
		switch req.Op {
		case "resolve":
			t.Status = Resolved
		case "dismiss":
			t.Status = Dismissed
		case "reopen":
			t.Status = Open
		}
	case "edit", "delete":
		t := r.thread(req.Thread)
		i := -1
		if t != nil {
			for k, c := range t.Comments {
				if c.Author == User && c.At.Equal(req.At) {
					i = k
					break
				}
			}
		}
		if i < 0 {
			return fmt.Errorf("there's no such comment of yours on thread %d: deleted already?", req.Thread)
		}
		c := &t.Comments[i]
		if author != User {
			return fmt.Errorf("only the user changes their comments")
		}
		if req.Op == "edit" {
			if strings.TrimSpace(req.Body) == "" {
				return fmt.Errorf("an edited comment needs something to say: delete it instead")
			}
			// One the agent has seen keeps what it saw, and waits to be sent
			// again, to tell it what changed.
			if c.Sent {
				c.Was = append(c.Was, c.Body)
				c.Sent = false
			}
			c.Body = req.Body
			break
		}
		if c.Sent || len(c.Was) > 0 {
			return fmt.Errorf("the agent has seen this comment, so it can't be deleted: edit it instead")
		}
		if i > 0 {
			t.Comments = append(t.Comments[:i], t.Comments[i+1:]...)
			break
		}
		if len(t.Comments) > 1 {
			return fmt.Errorf("thread %d has replies: delete them first", req.Thread)
		}
		for i := range r.Threads {
			if r.Threads[i].ID == req.Thread {
				r.Threads = append(r.Threads[:i], r.Threads[i+1:]...)
				break
			}
		}
	case "order":
		r.Order = req.Stops
	case "done":
		r.State = Done
	default:
		return fmt.Errorf("there's no such request as %q", req.Op)
	}
	return nil
}

// Ref is one comment: the Comment'th of thread Thread's, said at At. At is
// what MarkSent knows it by: the comments before it can be deleted in the
// meantime, moving it up.
type Ref struct {
	Thread  int       `json:"thread"`
	Comment int       `json:"comment"`
	At      time.Time `json:"at"`
}

// Unsent is the user's comments the agent hasn't been given, in the order
// the threads were started, each thread's in the order they were said.
func Unsent(r Review) []Ref {
	var out []Ref
	for _, t := range r.Threads {
		for i, c := range t.Comments {
			if c.Author == User && !c.Sent {
				out = append(out, Ref{Thread: t.ID, Comment: i, At: c.At})
			}
		}
	}
	return out
}

// MarkSent marks refs as given to the agent. One the user deleted in the
// meantime is no longer there to mark, and that's no matter.
func (s *Store) MarkSent(refs []Ref) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, err := s.load()
	if err != nil {
		return err
	}
	for _, ref := range refs {
		t := r.thread(ref.Thread)
		if t == nil {
			continue
		}
		for i := range t.Comments {
			if c := &t.Comments[i]; c.Author == User && c.At.Equal(ref.At) {
				c.Sent = true
			}
		}
	}
	return s.save(r)
}

func (r *Review) thread(id int) *Thread {
	for i := range r.Threads {
		if r.Threads[i].ID == id {
			return &r.Threads[i]
		}
	}
	return nil
}

// keep puts a review that's being replaced in history/, unless there's
// nothing in it.
func (s *Store) keep(r Review) error {
	if r.State == "" && len(r.Threads) == 0 {
		return nil
	}
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Join(s.dir, "history")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(dir, s.now().UTC().Format("20060102-150405.000000000")+".json"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// save writes the review whole, and its copy in the view, each through a
// new temporary file renamed over it, so a reader never sees half of it,
// and nothing left in the store's way is written through.
func (s *Store) save(r Review) error {
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	if err := fscopy.WriteWhole(s.file(), data); err != nil {
		return err
	}
	return fscopy.WriteWhole(filepath.Join(s.View(), "review.json"), data)
}

// viewed copies the review to the view when it isn't there: one kept
// before there was a view.
func (s *Store) viewed(r Review) error {
	if _, err := os.Lstat(filepath.Join(s.View(), "review.json")); !os.IsNotExist(err) {
		return nil
	}
	return s.save(r)
}

// CloseOver closes the review started at started, as over, saying why:
// unless it's no longer the review (another was started while its diff was
// worked out), it's closed already, or the user reopened it. It gives the
// review as it then is, and whether it closed it.
func (s *Store) CloseOver(started time.Time, why string) (Review, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, err := s.load()
	if err != nil {
		return r, false, err
	}
	if !r.Started.Equal(started) || r.Closed != "" || r.Reopened {
		return r, false, nil
	}
	r.Closed = why
	return r, true, s.save(r)
}

// Move is a comment (thread Thread) that has followed its code to Line.
type Move struct {
	Thread int
	Line   int
}

// Move moves comments to where their code is now, keeping the line each
// was first made on. One deleted in the meantime is no longer there to move.
func (s *Store) Move(moves []Move) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, err := s.load()
	if err != nil {
		return err
	}
	for _, m := range moves {
		t := r.thread(m.Thread)
		if t == nil {
			continue
		}
		if t.MovedFrom == 0 {
			t.MovedFrom = t.Line
		}
		t.EndLine += m.Line - t.Line
		t.Line = m.Line
	}
	return s.save(r)
}
