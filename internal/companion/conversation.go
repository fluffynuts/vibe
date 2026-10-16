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
	"slices"
	"sort"
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
	Kind  string          `json:"kind"` // user, agent, note, error, done, reset, review
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
	// answered says the agent has given its answer to the running turn's
	// prompt (its result line came), though it may still be at work in the
	// background: it can have commands running that it waits on. The page
	// may then send another prompt, which ends that (superseded) quietly.
	answered   bool
	superseded bool
	// compacting says the agent is summarising its context to make room,
	// which can take minutes with nothing else said.
	compacting bool
	// ended is closed when the running turn has finished.
	ended chan struct{}
	// heard is when the agent last said anything in the running turn (or
	// the turn began): long quiet while it answers may mean it is stuck.
	heard time.Time
	// now is the clock (time.Now when nil), for tests to stand in for.
	now func() time.Time
	// commands are the slash commands the agent said it has, the last time
	// it started, for the page to offer; commandsSeq counts their changes,
	// so a page only fetches them when it hasn't the latest.
	commands    []string
	commandsSeq int
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
	// Commands and CommandsSeq are conversation.commands and commandsSeq.
	Commands    []string `json:"commands,omitempty"`
	CommandsSeq int      `json:"commandsSeq,omitempty"`
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
	c.commands, c.commandsSeq = slashCommands(was.Commands), was.CommandsSeq
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
	data, err := json.Marshal(saved{Seq: c.seq, Session: c.session, Prompt: c.prompt, Running: c.running, Events: c.events,
		Commands: c.commands, CommandsSeq: c.commandsSeq})
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
	c.answered, c.superseded, c.compacting = false, false, false
	c.ended = make(chan struct{})
	c.heard = c.clock()
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

// line takes a line of the agent's output, learning the session from it,
// and says what type of line it was. A result is the agent's answer given;
// anything more it says after that (woken by something it left running)
// is it at work again.
func (c *conversation) line(raw []byte) string {
	raw = []byte(strings.TrimSpace(string(raw)))
	if len(raw) == 0 {
		return ""
	}
	if len(raw) > maxLine || !json.Valid(raw) {
		raw = []byte(`{"type":"truncated"}`)
	}
	var head struct {
		Type          string   `json:"type"`
		Subtype       string   `json:"subtype"`
		SessionID     string   `json:"session_id"`
		SlashCommands []string `json:"slash_commands"`
		Status        *string  `json:"status"`
	}
	json.Unmarshal(raw, &head)
	c.mu.Lock()
	defer c.mu.Unlock()
	if sessionID.MatchString(head.SessionID) {
		c.session = head.SessionID
	}
	if head.Type == "system" && head.Subtype == "init" && head.SlashCommands != nil {
		if commands := slashCommands(head.SlashCommands); !slices.Equal(commands, c.commands) {
			c.commands = commands
			c.commandsSeq++
		}
	}
	if head.Type == "system" && head.Subtype == "status" {
		c.compacting = head.Status != nil && *head.Status == "compacting"
	}
	c.advance(receiptSeen)
	c.heard = c.clock()
	c.add("agent", "", append(json.RawMessage(nil), raw...))
	switch head.Type {
	case "result":
		c.answered = true
		// The process may go on for a long while yet: what it has said
		// so far is kept now, not only when it ends.
		c.save()
	case "assistant":
		c.answered = false
	}
	return head.Type
}

func (c *conversation) finish(err error, stopped bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	switch {
	case c.superseded:
		// Ended for the next prompt, after it had answered: nothing to say.
		c.add("done", "", nil)
	case stopped:
		c.add("done", "stopped", nil)
	case err != nil:
		c.add("error", err.Error(), nil)
	default:
		c.add("done", "", nil)
	}
	c.running = false
	c.answered, c.superseded, c.compacting = false, false, false
	c.cancel = nil
	c.save()
	close(c.ended)
}

// answering says the agent is answering a prompt: running, and not yet
// answered.
func (c *conversation) answering() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.running && !c.answered
}

// supersede makes way for a new prompt: a turn the agent has answered but
// is still at work on in the background is ended (quietly), and waited
// for. It says whether the way is clear — not while the agent is still
// answering.
func (c *conversation) supersede() bool {
	c.mu.Lock()
	if !c.running {
		c.mu.Unlock()
		return true
	}
	if !c.answered || c.cancel == nil {
		c.mu.Unlock()
		return false
	}
	c.superseded = true
	c.cancel()
	ended := c.ended
	c.mu.Unlock()
	select {
	case <-ended:
		return true
	case <-time.After(15 * time.Second):
		return false
	}
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

// quietWarning is how long the agent may answer without a word before the
// page says it may be stuck: one tool call can run quiet for a long while
// (a build, a test suite), so it is generous. The turn isn't stopped: that
// is the user's to do. A variable for tests.
var quietWarning = 60 * time.Minute

func (c *conversation) clock() time.Time {
	if c.now != nil {
		return c.now()
	}
	return time.Now()
}

// quiet is how long the agent has been answering without saying anything
// new; 0 when it isn't answering.
func (c *conversation) quiet() time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.running || c.answered {
		return 0
	}
	return c.clock().Sub(c.heard)
}

// working says a turn is running: the agent answering, or having answered,
// still at work on something it left running.
func (c *conversation) working() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.running
}

// turn is the latest prompt's seq, and whether the agent is still at its
// answer to it.
func (c *conversation) turn() (prompt int, answering bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.prompt, c.running && !c.answered
}

// isCompacting says the running turn's agent is compacting its context.
func (c *conversation) isCompacting() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.running && c.compacting
}

// since is the events after seq, whether the agent is answering, and
// whether, having answered, it is still at work in the background.
func (c *conversation) since(after int) (events []event, last int, running, background bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	events = []event{}
	for _, e := range c.events {
		if e.Seq > after {
			events = append(events, e)
		}
	}
	return events, c.seq, c.running && !c.answered, c.running && c.answered
}

// note says something of vibe's own in the conversation: what it's doing
// before the agent can answer.
func (c *conversation) note(text string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.heard = c.clock()
	c.add("note", text, nil)
}

// run is a turn, in the background: prepare first, when there's one to do.
func (c *conversation) run(ctx context.Context, prepare func(context.Context, func(string)) error, converse Converser, prompt, session string) {
	if prepare != nil {
		if err := prepare(ctx, c.note); err != nil {
			c.finish(err, ctx.Err() != nil)
			return
		}
	}
	onLine := func(raw []byte) { c.line(raw) }
	err := converse(ctx, prompt, session, func() { c.receipt(receiptDelivered) }, onLine)
	c.finish(err, ctx.Err() != nil)
}

// maxCommands bounds how many slash commands are kept for the page.
const maxCommands = 500

// commandName is what a slash command's name looks like: plugin commands
// have a colon (plugin:skill). Anything else the agent sent is dropped.
var commandName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9:._-]{0,99}$`)

// slashCommands is names as the page may offer them: well-formed, without
// their leading slash, once each, sorted.
func slashCommands(names []string) []string {
	seen := map[string]bool{}
	commands := []string{}
	for _, name := range names {
		name = strings.TrimPrefix(strings.TrimSpace(name), "/")
		if !commandName.MatchString(name) || seen[name] || len(commands) >= maxCommands {
			continue
		}
		seen[name] = true
		commands = append(commands, name)
	}
	sort.Strings(commands)
	return commands
}

// commandsFor is the slash commands for a page that has those of seq: nil
// when it has the latest.
func (c *conversation) commandsFor(seq int) ([]string, int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if seq == c.commandsSeq {
		return nil, c.commandsSeq
	}
	return append([]string{}, c.commands...), c.commandsSeq
}

func (s *Server) conversationEvents(w http.ResponseWriter, r *http.Request) {
	after, _ := strconv.Atoi(r.URL.Query().Get("after"))
	events, last, running, background := s.convo.since(after)
	receipt := s.convo.latestReceipt()
	// A page that has seen more than there is (the conversation was begun
	// anew) starts again from the beginning.
	reset := after > last
	if reset {
		events, last, running, background = s.convo.since(0)
		receipt = s.convo.latestReceipt()
	}
	body := map[string]interface{}{
		"events": events, "last": last, "running": running, "background": background, "reset": reset, "receipt": receipt,
		// Minutes the agent has been answering with nothing new, once that
		// is long enough to say so.
		"quietMinutes": quietMinutes(s.convo.quiet()),
		"compacting":   s.convo.isCompacting(),
	}
	// The slash commands go only to a page without the latest (it says
	// which it has), not with every poll.
	known, err := strconv.Atoi(r.URL.Query().Get("commands"))
	if err != nil {
		known = -1
	}
	if commands, seq := s.convo.commandsFor(known); commands != nil {
		body["commands"], body["commandsSeq"] = commands, seq
	}
	writeJSON(w, http.StatusOK, body)
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
	if !s.startTurn(req.Prompt) {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "the agent is still answering the last prompt"})
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]bool{"ok": true})
}

// startTurn sends prompt to the agent as a turn, in the background; false
// when it can't, the agent still answering the last one.
func (s *Server) startTurn(prompt string) bool {
	launch, _, ok := s.claimTurn(prompt)
	if ok {
		launch()
	}
	return ok
}

// claimTurn makes prompt the next turn, unless the agent is still at the
// last, without yet giving it to the agent: launch does, or abandon ends
// it, saying why. Something the turn needs (a review to start) is made in
// between, so the agent never hears of one that isn't there, and nothing
// made is undone because the agent was busy.
func (s *Server) claimTurn(prompt string) (launch func(), abandon func(error), ok bool) {
	session, ctx, ok := "", context.Context(nil), s.convo.supersede()
	if ok {
		session, ctx, ok = s.convo.begin(prompt)
	}
	if !ok {
		return nil, nil, false
	}
	launch = func() { go s.convo.run(ctx, s.cfg.Prepare, s.cfg.Converse, prompt, session) }
	abandon = func(why error) { s.convo.finish(why, false) }
	return launch, abandon, true
}

func (s *Server) conversationStop(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]bool{"stopped": s.convo.stop()})
}

func (s *Server) conversationNew(w http.ResponseWriter, r *http.Request) {
	if !s.convo.supersede() || !s.convo.reset() {
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

// administering wraps a call from the Admin tab: the same protections as
// conversing().
func (s *Server) administering(next http.HandlerFunc) http.HandlerFunc {
	return s.authed(func(w http.ResponseWriter, r *http.Request) {
		if s.cfg.Admin == nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "this page has no admin tab"})
			return
		}
		if !sameOriginJSON(w, r) {
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 64*1024)
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
	return sameOrigin(w, r)
}

// quietMinutes is d in whole minutes when it has reached quietWarning, else 0.
func quietMinutes(d time.Duration) int {
	if d < quietWarning {
		return 0
	}
	return int(d / time.Minute)
}
