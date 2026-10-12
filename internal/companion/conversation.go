package companion

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Converser runs one turn of the conversation: it sends prompt to the agent
// in the sandbox (resuming session when there is one) and calls onLine with
// each line of the agent's stream-json output, until the agent is done or
// ctx is cancelled. delivered is called once the whole prompt has been
// handed to the sandbox.
type Converser func(ctx context.Context, prompt, session string, delivered func(), onLine func(line []byte)) error

// What the user is told about their latest prompt, like the ticks of a
// chat message: it was kept (sent), it reached the sandbox (delivered), the
// agent has started answering (seen). They only ever advance.
const (
	receiptSent      = "sent"
	receiptDelivered = "delivered"
	receiptSeen      = "seen"
)

var receiptRank = map[string]int{receiptSent: 1, receiptDelivered: 2, receiptSeen: 3}

const (
	// maxPrompt is how much of a prompt the page may send; the 4096 bytes
	// changing() allows for a rule is far too little for one.
	maxPrompt = 64 * 1024
	// maxLine is the longest line of agent output kept; a tool result can
	// be huge and the page only ever shows a summary of one.
	maxLine = 256 * 1024
	// maxEvents bounds what a long conversation keeps in memory. Sequence
	// numbers keep counting past it, so a page that is behind just misses
	// what was dropped.
	maxEvents = 2000
)

// A session id goes onto the agent's command line, so it is only ever what
// the agent's own ids look like.
var sessionID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,100}$`)

// event is one thing the page shows: what the user said, a line of the
// agent's output, how a turn ended, or that the conversation began anew.
type event struct {
	Seq   int             `json:"seq"`
	Kind  string          `json:"kind"` // user, agent, error, done, reset
	Text  string          `json:"text,omitempty"`
	Agent json.RawMessage `json:"agent,omitempty"`
	// Receipt is how far a user event got: see receiptSent.
	Receipt string `json:"receipt,omitempty"`
	// At is when it happened, in milliseconds since the epoch, for the page
	// to show; 0 in conversations kept from before it was recorded.
	At int64 `json:"at,omitempty"`
}

// conversation is the one conversation the page has with the agent. It
// lives here rather than in the page so a reload, or a second tab, picks it
// up where it was.
type conversation struct {
	mu      sync.Mutex
	events  []event
	seq     int
	session string
	running bool
	cancel  context.CancelFunc
	// prompt is the seq of the user event of the turn running or last run.
	prompt int
	// file is where the conversation is kept between runs of vibe, so a
	// page that outlives one carries on where it was; "" keeps it only in
	// memory.
	file string
}

// saved is what file holds.
type saved struct {
	Seq     int     `json:"seq"`
	Session string  `json:"session,omitempty"`
	Prompt  int     `json:"prompt,omitempty"`
	Running bool    `json:"running,omitempty"`
	Events  []event `json:"events"`
}

// load picks up the conversation file holds, if any. A turn that was
// running when it was saved went with the vibe that ran it, and is said to
// have. Seq carries on from where it was, so a page that was showing the
// conversation sees only what is new.
func (c *conversation) load(file string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.file = file
	data, err := os.ReadFile(file)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var was saved
	if err := json.Unmarshal(data, &was); err != nil {
		return fmt.Errorf("%s: %w", file, err)
	}
	c.events, c.seq, c.prompt = was.Events, was.Seq, was.Prompt
	if sessionID.MatchString(was.Session) {
		c.session = was.Session
	}
	if was.Running {
		c.add("error", "vibe stopped before the agent had finished answering", nil)
		c.save()
	}
	return nil
}

// save writes the conversation to file, whole, if it has one. It is called
// with mu held, when a turn begins or ends rather than at every line: a
// vibe that stops mid-turn loses what the agent said of it, not what came
// before.
func (c *conversation) save() {
	if c.file == "" {
		return
	}
	data, err := json.Marshal(saved{Seq: c.seq, Session: c.session, Prompt: c.prompt, Running: c.running, Events: c.events})
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(c.file), 0o700); err != nil {
		return
	}
	tmp := c.file + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return
	}
	os.Rename(tmp, c.file)
}

// close saves the conversation as it is, for the next vibe to pick up.
func (c *conversation) close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.save()
}

func (c *conversation) add(kind, text string, agent json.RawMessage) {
	c.seq++
	c.events = append(c.events, event{Seq: c.seq, Kind: kind, Text: text, Agent: agent, At: time.Now().UnixMilli()})
	if over := len(c.events) - maxEvents; over > 0 {
		c.events = append([]event(nil), c.events[over:]...)
	}
}

// begin starts a turn, or says one is already running.
func (c *conversation) begin(prompt string) (session string, ctx context.Context, ok bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.running {
		return "", nil, false
	}
	ctx, c.cancel = context.WithCancel(context.Background())
	c.running = true
	c.add("user", prompt, nil)
	c.prompt = c.seq
	c.events[len(c.events)-1].Receipt = receiptSent
	c.save()
	return c.session, ctx, true
}

// receipt moves the latest prompt's receipt forward to state.
func (c *conversation) receipt(state string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.advance(state)
}

func (c *conversation) advance(state string) {
	for i := len(c.events) - 1; i >= 0 && c.events[i].Seq >= c.prompt; i-- {
		if e := &c.events[i]; e.Seq == c.prompt && receiptRank[state] > receiptRank[e.Receipt] {
			e.Receipt = state
		}
	}
}

// line takes a line of the agent's output, learning the session from it.
func (c *conversation) line(raw []byte) {
	raw = []byte(strings.TrimSpace(string(raw)))
	if len(raw) == 0 {
		return
	}
	if len(raw) > maxLine || !json.Valid(raw) {
		raw = []byte(`{"type":"truncated"}`)
	}
	var head struct {
		SessionID string `json:"session_id"`
	}
	json.Unmarshal(raw, &head)
	c.mu.Lock()
	defer c.mu.Unlock()
	if sessionID.MatchString(head.SessionID) {
		c.session = head.SessionID
	}
	c.advance(receiptSeen)
	c.add("agent", "", append(json.RawMessage(nil), raw...))
}

func (c *conversation) finish(err error, stopped bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	switch {
	case stopped:
		c.add("done", "stopped", nil)
	case err != nil:
		c.add("error", err.Error(), nil)
	default:
		c.add("done", "", nil)
	}
	c.running = false
	c.cancel = nil
	c.save()
}

func (c *conversation) stop() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.running || c.cancel == nil {
		return false
	}
	c.cancel()
	return true
}

// reset forgets the conversation, unless a turn is running.
func (c *conversation) reset() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.running {
		return false
	}
	c.events, c.session, c.prompt = nil, "", 0
	// Pages showing the old events are told to clear them.
	c.add("reset", "", nil)
	c.save()
	return true
}

// latestReceipt is how far the latest prompt got, for a page that already
// has it to catch up with: its seq and receipt, or nil before there is one.
func (c *conversation) latestReceipt() map[string]interface{} {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, e := range c.events {
		if e.Seq == c.prompt && e.Kind == "user" {
			return map[string]interface{}{"seq": e.Seq, "state": e.Receipt}
		}
	}
	return nil
}

// since is the events after seq, and whether a turn is running.
func (c *conversation) since(after int) (events []event, last int, running bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	events = []event{}
	for _, e := range c.events {
		if e.Seq > after {
			events = append(events, e)
		}
	}
	return events, c.seq, c.running
}

// run is a turn, in the background.
func (c *conversation) run(ctx context.Context, converse Converser, prompt, session string) {
	err := converse(ctx, prompt, session, func() { c.receipt(receiptDelivered) }, c.line)
	stopped := ctx.Err() != nil
	c.finish(err, stopped)
}

func (s *Server) conversationEvents(w http.ResponseWriter, r *http.Request) {
	after, _ := strconv.Atoi(r.URL.Query().Get("after"))
	events, last, running := s.convo.since(after)
	receipt := s.convo.latestReceipt()
	// A page that has seen more than there is (the conversation was begun
	// anew) starts again from the beginning.
	reset := after > last
	if reset {
		events, last, running = s.convo.since(0)
		receipt = s.convo.latestReceipt()
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"events": events, "last": last, "running": running, "reset": reset, "receipt": receipt,
	})
}

func (s *Server) conversationSend(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Prompt string `json:"prompt"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "that prompt is too long"})
			return
		}
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "expected {\"prompt\": ...}"})
		return
	}
	if strings.TrimSpace(req.Prompt) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "there is nothing to send"})
		return
	}
	session, ctx, ok := s.convo.begin(req.Prompt)
	if !ok {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "the agent is still answering the last prompt"})
		return
	}
	go s.convo.run(ctx, s.cfg.Converse, req.Prompt, session)
	writeJSON(w, http.StatusAccepted, map[string]bool{"ok": true})
}

func (s *Server) conversationStop(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]bool{"stopped": s.convo.stop()})
}

func (s *Server) conversationNew(w http.ResponseWriter, r *http.Request) {
	if !s.convo.reset() {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "stop the agent first"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// conversing wraps a call that changes the conversation: the same
// protections as changing(), but with room for a prompt, and nothing of
// sbx's rules to forget.
func (s *Server) conversing(next http.HandlerFunc) http.HandlerFunc {
	return s.authed(func(w http.ResponseWriter, r *http.Request) {
		if s.cfg.Converse == nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "this sandbox has no conversation to have"})
			return
		}
		if !sameOriginJSON(w, r) {
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxPrompt)
		next(w, r)
	})
}

// sameOriginJSON refuses, and says so, anything that is not a same-origin
// POST of JSON: what a page on another site could not make.
func sameOriginJSON(w http.ResponseWriter, r *http.Request) bool {
	if ct, _, _ := strings.Cut(r.Header.Get("Content-Type"), ";"); strings.TrimSpace(ct) != "application/json" {
		writeJSON(w, http.StatusUnsupportedMediaType, map[string]string{"error": "expected application/json"})
		return false
	}
	if origin := r.Header.Get("Origin"); origin != "" && origin != "http://"+r.Host {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "cross-origin request refused"})
		return false
	}
	if site := r.Header.Get("Sec-Fetch-Site"); site != "" && site != "same-origin" {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "cross-site request refused"})
		return false
	}
	return true
}
