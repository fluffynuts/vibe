package review

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
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
}

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
// review in review.json, of which it's the only writer, and an inbox the
// agent's command drops requests into, mounted into the sandbox.
type Store struct {
	dir string
	mu  sync.Mutex
	now func() time.Time
	// readFile reads a request from the inbox: os.ReadFile, which a test
	// stands in for, as no platform can be relied on for a file that can't
	// be read (Windows has no such permission).
	readFile func(string) ([]byte, error)
}

// NewStore is the store in dir.
func NewStore(dir string) *Store {
	return &Store{dir: dir, now: time.Now, readFile: os.ReadFile}
}

// Inbox is the folder the agent's requests are dropped in.
func (s *Store) Inbox() string { return filepath.Join(s.dir, "inbox") }

func (s *Store) file() string { return filepath.Join(s.dir, "review.json") }

// Load is the review as last written; an empty one when there's none.
func (s *Store) Load() (Review, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.load()
}

func (s *Store) load() (Review, error) {
	r := Review{Threads: []Thread{}}
	data, err := os.ReadFile(s.file())
	if os.IsNotExist(err) {
		return r, nil
	}
	if err != nil {
		return r, err
	}
	if err := json.Unmarshal(data, &r); err != nil {
		return r, fmt.Errorf("%s: %w", s.file(), err)
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
	entries, err := os.ReadDir(s.Inbox())
	if os.IsNotExist(err) {
		return r, nil
	}
	if err != nil {
		return r, err
	}
	names := []string{}
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".json") {
			names = append(names, e.Name())
		}
	}
	if len(names) == 0 {
		return r, nil
	}
	sort.Strings(names)
	for _, name := range names {
		path := filepath.Join(s.Inbox(), name)
		var req Request
		if data, err := s.readFile(path); err != nil {
			s.problem(&r, fmt.Sprintf("%s couldn't be read: %v", name, err))
		} else if err := json.Unmarshal(data, &req); err != nil {
			s.problem(&r, fmt.Sprintf("%s isn't a request: %v", name, err))
		} else if err := s.apply(&r, req, Agent); err != nil {
			s.problem(&r, fmt.Sprintf("%s: %v", req.Op, err))
		}
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			s.problem(&r, fmt.Sprintf("%s couldn't be taken out of the inbox, so it may be taken in again: %v", name, err))
		}
	}
	r.AgentAt = s.now()
	return r, s.save(r)
}

// Do carries out req, from author, and gives the review as it then is.
func (s *Store) Do(req Request, author string) (Review, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if author == User && !userOps[req.Op] {
		return Review{}, fmt.Errorf("%q is the agent's to do, not the page's", req.Op)
	}
	r, err := s.load()
	if err != nil {
		return r, err
	}
	if err := s.apply(&r, req, author); err != nil {
		return r, err
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
	switch req.Op {
	case "start":
		if err := s.keep(*r); err != nil {
			return err
		}
		state := InProgress
		if author == User && !req.Reviewing {
			state = ""
		}
		*r = Review{Ref: req.Ref, Commit: req.Commit, Unpushed: req.Unpushed, Note: req.Note, State: state, Threads: []Thread{}, Started: s.now(), LastID: r.LastID}
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
			r.Closed = ""
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
	return os.WriteFile(filepath.Join(dir, s.now().UTC().Format("20060102-150405.000000000")+".json"), data, 0o600)
}

// save writes the review whole, through a temporary file, so a reader
// never sees half of it.
func (s *Store) save(r Review) error {
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return err
	}
	tmp := s.file() + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.file())
}

// Put makes r the review, as it was: to undo one started for nothing.
func (s *Store) Put(r Review) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.save(r)
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
