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
	Op      string `json:"op"`
	Ref     string `json:"ref,omitempty"`
	Commit  string `json:"commit,omitempty"`
	Note    string `json:"note,omitempty"`
	File    string `json:"file,omitempty"`
	Side    string `json:"side,omitempty"`
	Line    int    `json:"line,omitempty"`
	EndLine int    `json:"endLine,omitempty"`
	Code    string `json:"code,omitempty"`
	Thread  int    `json:"thread,omitempty"`
	Body    string `json:"body,omitempty"`
	Stops   []Stop `json:"stops,omitempty"`
}

// userOps are what the user can do from the page: the rest is the agent's.
var userOps = map[string]bool{"reply": true, "resolve": true, "dismiss": true, "reopen": true}

// Store keeps a sandbox's review in dir (~/.vibe/reviews/<sandbox>): the
// review in review.json, of which it's the only writer, and an inbox the
// agent's command drops requests into, mounted into the sandbox.
type Store struct {
	dir string
	mu  sync.Mutex
	now func() time.Time
}

// NewStore is the store in dir.
func NewStore(dir string) *Store {
	return &Store{dir: dir, now: time.Now}
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
	return r, nil
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
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var req Request
		if err := json.Unmarshal(data, &req); err != nil {
			s.problem(&r, fmt.Sprintf("%s isn't a request: %v", name, err))
		} else if err := s.apply(&r, req, Agent); err != nil {
			s.problem(&r, fmt.Sprintf("%s: %v", req.Op, err))
		}
		os.Remove(path)
	}
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
		*r = Review{Ref: req.Ref, Commit: req.Commit, Note: req.Note, State: InProgress, Threads: []Thread{}, Started: s.now(), LastID: r.LastID}
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
	case "order":
		r.Order = req.Stops
	case "done":
		r.State = Done
	default:
		return fmt.Errorf("there's no such request as %q", req.Op)
	}
	return nil
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
