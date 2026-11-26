package companion

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"vibe/internal/activity"
	"vibe/internal/fscopy"
)

// Agent is the agent's process for the conversation. It lives while it has
// work: a turn under way, or something it left running in the background,
// which calls it back when it ends (an unprompted turn). Once it has been idle
// for idleFor it is asked to end, so that the sandbox can sleep; the next
// prompt starts another, resuming the conversation.
type Agent interface {
	// Prompt gives the agent a user message. id names it in the agent's
	// command_lifecycle lines. A prompt sent while it is answering is taken
	// into the turn under way, as the console does.
	Prompt(text, id string) error
	// Interrupt stops the turn under way, leaving what runs in the
	// background alone.
	Interrupt() error
	// StopTask stops one thing the agent has running in the background.
	StopTask(id string) error
	// End closes the agent's input: it ends once it has nothing left to do.
	End()
	// Kill ends the agent, and all it started, at once. An error says it
	// could not be confirmed that it has ended.
	Kill() error
	// Done is closed once the agent has ended; Err is how.
	Done() <-chan struct{}
	Err() error
}

// StartAgent starts the agent for the conversation, resuming session when
// there is one, calling onLine with each line of its stream-json output. ctx
// bounds the start, not the agent's life: Kill and End end that.
type StartAgent func(ctx context.Context, session string, onLine func(line []byte)) (Agent, error)

// ErrStopUnconfirmed is what Kill returns when it couldn't confirm that the
// agent has really ended: it may still be running in the sandbox.
var ErrStopUnconfirmed = errors.New("the agent may not have stopped")

// What the user is told about their latest prompt, like the ticks of a
// chat message: it was kept (sent), it reached the sandbox (delivered), the
// agent has started answering (seen). They only ever advance.
const (
	receiptSent      = "sent"
	receiptDelivered = "delivered"
	receiptSeen      = "seen"
	// receiptUndelivered is a prompt never given to the agent.
	receiptUndelivered = "undelivered"
	// receiptQueued is a prompt sent while the agent was still being stopped:
	// it goes to the agent when that has ended.
	receiptQueued = "queued"
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
	// maxKept bounds the bytes of the events kept, as maxEvents bounds how
	// many: each can be up to maxLine, and the whole is written out at
	// every answer.
	maxKept = 32 << 20
)

// SessionID is what an agent's session id looks like. One goes onto the
// agent's command line, so it is only ever taken when it looks like that.
var SessionID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,100}$`)

// event is one thing the page shows: what the user said, a line of the
// agent's output, how a turn ended, or that the conversation began anew.
type event struct {
	Seq   int             `json:"seq"`
	Kind  string          `json:"kind"` // user, agent, note, error, done, reset, review, retract, denied
	Text  string          `json:"text,omitempty"`
	Agent json.RawMessage `json:"agent,omitempty"`
	// Receipt is how far a user event got: see receiptSent.
	Receipt string `json:"receipt,omitempty"`
	// At is when it happened, in milliseconds since the epoch, for the page
	// to show; 0 in conversations kept from before it was recorded.
	At int64 `json:"at,omitempty"`
	// Of is, for a retract, the Seq of the event it takes back: a page that
	// has that event shows it no longer.
	Of int `json:"of,omitempty"`
	// Suggest is, for a denied, the permission mode that would have let it
	// through, for the page to offer ("" for none).
	Suggest string `json:"suggest,omitempty"`
}

// conversation is the one conversation the page has with the agent. It
// lives here rather than in the page so a reload, or a second tab, picks it
// up where it was.
type conversation struct {
	mu      sync.Mutex
	events  []event
	seq     int
	session string
	// agent is the agent's process, while it lives; starting says one is
	// being started (the sandbox made ready first), cancelStart stops that.
	agent    Agent
	starting bool
	// modeInForce is the permission mode the agent last said it is in (as --permission-mode names it), and modeRefused Claude
	// Code's words when it refused vibe's last change of it ("" when it took it). See permissionmode.go.
	modeInForce, modeRefused string
	// modeAsked is the mode that agent had been asked for when it said it was in modeInForce.
	modeAsked   string
	cancelStart context.CancelFunc
	// waiting are prompts given while the agent was being started: they go
	// to it, in order, once it has. handingOver says they are being given to
	// it now: a prompt given meanwhile joins them rather than overtake them.
	waiting     []waitingPrompt
	handingOver bool
	// pending are the prompts given to the agent and not yet done with, by
	// the id the agent's command_lifecycle lines name them by.
	pending map[string]int
	// prompt is the seq of the latest user event.
	prompt int
	// answering says a turn is under way: from a prompt (or the agent
	// starting one by itself, called back by its background work) to its
	// result.
	answering bool
	// stopping says an interrupt was sent and the turn has not yet ended.
	stopping bool
	// tasks is what the agent has running in the background, as it last
	// said (background_tasks_changed).
	tasks []BackgroundTask
	// idle ends an agent that has had nothing to do for idleFor (or
	// idleAfter, when tests set it).
	idle      *time.Timer
	idleAfter time.Duration
	// endAfter stands for endWithin, when tests set it.
	endAfter time.Duration
	// started counts the agents started: an agent being let go knows from it
	// whether another has started since.
	started int
	// compacting says the agent is summarising its context to make room,
	// which can take minutes with nothing else said.
	compacting bool
	// heard is when the agent last said anything in the turn under way (or
	// the turn began): long quiet while it answers may mean it is stuck.
	heard time.Time
	// now is the clock (time.Now when nil), for tests to stand in for.
	now func() time.Time
	// commands are the slash commands the agent said it has, the last time
	// it started, for the page to offer; commandsSeq counts their changes,
	// so a page only fetches them when it hasn't the latest.
	commands    []string
	commandsSeq int
	// model is the model the agent said it was running, the last time it
	// started a turn: what ran, as against what was asked for.
	model string
	// file is where the conversation is kept between runs of vibe, so a
	// page that outlives one carries on where it was; "" keeps it only in
	// memory.
	file string
	// closed says vibe has closed the conversation: nothing more is
	// recorded in it (the next vibe takes it from there).
	closed bool
}

// BackgroundTask is one thing the agent has running in the background, as
// it says in its background_tasks_changed lines.
type BackgroundTask struct {
	ID          string `json:"task_id"`
	Type        string `json:"task_type,omitempty"`
	Description string `json:"description,omitempty"`
}

// taskNames names tasks for a sentence: by what the agent said each is, quoted, or by its id.
func taskNames(tasks []BackgroundTask) string {
	names := make([]string, 0, len(tasks))
	for _, t := range tasks {
		name := t.Description
		if name == "" {
			name = t.ID
		}
		names = append(names, strconv.Quote(name))
	}
	return strings.Join(names, ", ")
}

// waitingPrompt is a prompt in the conversation (its user event is seq), not
// yet given to the agent, which is being started.
type waitingPrompt struct {
	seq    int
	prompt string
	id     string
}

// idleFor is how long the agent is kept with nothing to do (no turn, nothing
// in the background) before it is asked to end: an open agent keeps the
// sandbox awake. endWithin is how long it then has to end before it is
// killed.
const (
	idleFor   = 30 * time.Second
	endWithin = 30 * time.Second
)

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
	// Model is conversation.model.
	Model string `json:"model,omitempty"`
	// Tasks is what the agent had running in the background: kept by a vibe
	// that died, the next says what it left.
	Tasks []BackgroundTask `json:"tasks,omitempty"`
}

// load picks up the conversation file holds, if any. A turn that was
// running when it was saved went with the vibe that ran it, and is said to
// have. Seq carries on from where it was, so a page that was showing the
// conversation sees only what is new.
func (c *conversation) load(file string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.file = file
	data, err := fscopy.ReadFile(file)
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
	c.model = was.Model
	if SessionID.MatchString(was.Session) {
		c.session = was.Session
	}
	for i := range c.events {
		if c.events[i].Receipt == receiptQueued {
			c.events[i].Receipt = receiptUndelivered
		}
	}
	if len(was.Tasks) > 0 {
		c.add("note", "vibe stopped while the agent had work running in the background: "+taskNames(was.Tasks)+". It is ended when the agent next starts, without reporting back.", nil)
	}
	if was.Running {
		c.add("error", "vibe stopped before the agent had finished answering", nil)
	}
	if was.Running || len(was.Tasks) > 0 {
		c.save()
	}
	return nil
}

// save writes the conversation to file, whole, if it has one. It is called
// with mu held, when a turn begins or ends rather than at every line: a
// vibe that stops mid-turn loses what the agent said of it, not what came
// before.
func (c *conversation) save() {
	if c.file == "" || c.closed {
		return
	}
	data, err := json.Marshal(saved{Seq: c.seq, Session: c.session, Prompt: c.prompt, Running: c.busy(), Events: c.events,
		Commands: c.commands, CommandsSeq: c.commandsSeq, Model: c.model, Tasks: c.tasks})
	if err != nil {
		return
	}
	fscopy.WriteWhole(c.file, data)
}

// ranModel is the model the agent said it was running, "" before it has.
func (c *conversation) ranModel() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.model
}

// hasSession says the conversation is one the agent would resume.
func (c *conversation) hasSession() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.session != ""
}

// close saves the conversation as it is, for the next vibe to pick up, and
// ends the agent.
func (c *conversation) close() {
	c.mu.Lock()
	if c.busy() {
		c.add("error", "vibe stopped before the agent had finished answering", nil)
		c.answering, c.starting, c.stopping = false, false, false
	}
	if c.agent != nil && len(c.tasks) > 0 {
		c.add("note", "vibe stopped, and with it what the agent had running in the background: "+taskNames(c.tasks)+".", nil)
	}
	c.tasks = nil
	c.save()
	c.closed = true
	agent := c.agent
	c.agent = nil
	c.disarmIdle()
	if c.cancelStart != nil {
		c.cancelStart()
	}
	c.mu.Unlock()
	// The agent goes with the vibe that ran it: left, it would carry on
	// unseen in the sandbox, its output going nowhere.
	if agent != nil {
		agent.Kill()
	}
}

func (c *conversation) add(kind, text string, agent json.RawMessage) {
	c.seq++
	c.events = append(c.events, event{Seq: c.seq, Kind: kind, Text: text, Agent: agent, At: time.Now().UnixMilli()})
	over := len(c.events) - maxEvents
	kept := 0
	for i := len(c.events) - 1; i >= 0; i-- {
		if kept += len(c.events[i].Text) + len(c.events[i].Agent); kept > maxKept {
			over = max(over, i+1)
			break
		}
	}
	if over > 0 {
		c.events = append([]event(nil), c.events[over:]...)
	}
}

// busy says a turn is under way or the agent is being started for one.
// mu is held.
func (c *conversation) busy() bool {
	return c.answering || c.starting
}

// claim makes prompt the next turn, unless one is under way (when force,
// it is taken into the one under way, as the console does): its user event
// is added, and the turn is held for it until give or abandon. It gives
// the event's seq, or false.
func (c *conversation) claim(prompt string, force bool) (int, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.busy() && !force {
		return 0, false
	}
	c.add("user", prompt, nil)
	c.events[len(c.events)-1].Receipt = receiptSent
	c.prompt = c.seq
	if !c.answering {
		c.answering, c.stopping, c.compacting = true, false, false
		c.heard = c.clock()
	}
	c.disarmIdle()
	c.save()
	return c.seq, true
}

// give gives the claimed prompt (its user event is seq) to the agent,
// starting one with start when there is none, the sandbox made ready first
// with prepare (it may be stopped).
func (c *conversation) give(seq int, prompt string, start StartAgent, prepare func(context.Context, func(string)) error) {
	id := newPromptID()
	c.mu.Lock()
	if c.pending == nil {
		c.pending = map[string]int{}
	}
	c.pending[id] = seq
	agent := c.agent
	switch {
	case c.starting || c.handingOver:
		c.waiting = append(c.waiting, waitingPrompt{seq, prompt, id})
		c.mu.Unlock()
		return
	case agent != nil:
	default:
		c.waiting = append(c.waiting, waitingPrompt{seq, prompt, id})
		c.beginStart(start, prepare)
		c.mu.Unlock()
		return
	}
	c.mu.Unlock()
	c.deliver(agent, waitingPrompt{seq, prompt, id})
}

// abandon gives up the claimed prompt seq, saying why: nothing was given to
// the agent.
func (c *conversation) abandon(seq int, why error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.setReceipt(seq, receiptUndelivered)
	c.answering = false
	if why != nil {
		c.add("error", why.Error(), nil)
	} else {
		c.add("done", "", nil)
	}
	c.armIdle()
	c.save()
}

// deliver writes prompt to agent.
func (c *conversation) deliver(agent Agent, w waitingPrompt) {
	err := agent.Prompt(w.prompt, w.id)
	c.mu.Lock()
	defer c.mu.Unlock()
	if err != nil {
		delete(c.pending, w.id)
		c.setReceipt(w.seq, receiptUndelivered)
		c.add("error", "the prompt couldn't be given to the agent: "+err.Error(), nil)
		c.save()
		return
	}
	c.advanceReceipt(w.seq, receiptDelivered)
}

// beginStart starts an agent for the prompts waiting, c.mu held.
func (c *conversation) beginStart(start StartAgent, prepare func(context.Context, func(string)) error) {
	c.starting = true
	c.started++
	ctx, cancel := context.WithCancel(context.Background())
	c.cancelStart = cancel
	go c.startAgent(ctx, c.session, start, prepare)
}

// startAgent makes the sandbox ready and starts the agent, then gives it
// the prompts that waited.
func (c *conversation) startAgent(ctx context.Context, session string, start StartAgent, prepare func(context.Context, func(string)) error) {
	if prepare != nil {
		// What is said while the sandbox is made ready is true only until it is:
		// once it is, it's taken back, so it doesn't sit in the conversation
		// saying "starting the sandbox" over what then runs. Left, when it can't be
		// made ready: it says what the error that follows is about.
		var said []int
		var mu sync.Mutex
		err := prepare(ctx, func(text string) {
			seq := c.noteSaid(text)
			mu.Lock()
			said = append(said, seq)
			mu.Unlock()
		})
		if err != nil {
			c.startFailed(err, ctx.Err() != nil)
			return
		}
		mu.Lock()
		c.retract(said)
		mu.Unlock()
	}
	agent, err := start(ctx, session, func(raw []byte) { c.line(raw) })
	if err != nil {
		c.startFailed(err, ctx.Err() != nil)
		return
	}
	c.mu.Lock()
	if ctx.Err() != nil || c.closed {
		// Stopped while it started (or vibe closed): what it was started for is
		// not given to it.
		c.mu.Unlock()
		agent.Kill()
		c.startFailed(ctx.Err(), true)
		return
	}
	c.agent, c.starting, c.cancelStart, c.handingOver = agent, false, nil, true
	// A refusal was the last agent's; the mode it was in is still shown until this one says its own.
	c.modeRefused = ""
	c.mu.Unlock()
	go func() {
		<-agent.Done()
		c.agentEnded(agent)
	}()
	for {
		c.mu.Lock()
		waiting := c.waiting
		c.waiting = nil
		if len(waiting) == 0 || c.agent != agent {
			c.handingOver = false
			// The agent ended while the prompts were handed to it. Those it was
			// given were marked undelivered as it ended; any given meanwhile have
			// not been tried, and start a new agent, as with no agent there. Not
			// once vibe is stopping: it was killed for that, and nothing would end
			// the start.
			if len(waiting) > 0 && !c.closed {
				c.waiting = waiting
				c.beginStart(start, prepare)
			}
			c.mu.Unlock()
			return
		}
		c.mu.Unlock()
		for _, w := range waiting {
			c.deliver(agent, w)
		}
	}
}

// startFailed ends the turn the agent was being started for: the prompts
// that waited were never given to it.
func (c *conversation) startFailed(err error, stopped bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return
	}
	for _, w := range c.waiting {
		delete(c.pending, w.id)
		c.setReceipt(w.seq, receiptUndelivered)
	}
	c.waiting, c.starting, c.cancelStart = nil, false, nil
	c.answering, c.stopping = false, false
	if stopped {
		c.add("done", "stopped", nil)
	} else {
		// An error ends the turn on the page, as a done would.
		c.add("error", err.Error(), nil)
	}
	c.save()
}

// agentEnded is the agent having ended: a turn it was at is over, and so is
// what it had running in the background.
func (c *conversation) agentEnded(agent Agent) {
	err := agent.Err()
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.agent != agent {
		return
	}
	if len(c.tasks) > 0 {
		c.add("note", "The agent ended, and with it what it had running in the background: "+taskNames(c.tasks)+".", nil)
	}
	c.agent, c.tasks = nil, nil
	c.disarmIdle()
	for id, seq := range c.pending {
		c.setReceipt(seq, receiptUndelivered)
		delete(c.pending, id)
	}
	if c.answering {
		switch {
		case c.stopping:
			c.add("done", "stopped", nil)
		case err != nil:
			c.add("error", "the agent ended before it had answered: "+err.Error(), nil)
		default:
			c.add("error", "the agent ended before it had answered", nil)
		}
	}
	c.answering, c.stopping, c.compacting = false, false, false
	c.save()
}

// armIdle sets the agent to be ended once it has had nothing to do for
// idleFor. mu is held.
func (c *conversation) armIdle() {
	c.disarmIdle()
	agent := c.agent
	if agent == nil || c.busy() || len(c.tasks) > 0 {
		return
	}
	wait := idleFor
	if c.idleAfter > 0 {
		wait = c.idleAfter
	}
	c.idle = time.AfterFunc(wait, func() {
		c.mu.Lock()
		still := c.agent == agent && !c.busy() && len(c.tasks) == 0
		if still {
			// Let go of it now: its input is closing, so the next prompt
			// starts another (which ends this one, if it is still ending).
			c.agent = nil
		}
		generation := c.started
		c.mu.Unlock()
		if !still {
			return
		}
		agent.End()
		select {
		case <-agent.Done():
		case <-time.After(c.endWait()):
			c.mu.Lock()
			// The kill goes by the conversation's pid file, which names the
			// agent started last: once another has started (and its start
			// ended this one, as a leftover), it would end that one instead.
			newer := c.started != generation
			c.mu.Unlock()
			if !newer {
				agent.Kill()
			}
		}
	})
}

// endWait is how long an agent asked to end has before it is killed.
func (c *conversation) endWait() time.Duration {
	if c.endAfter > 0 {
		return c.endAfter
	}
	return endWithin
}

// disarmIdle keeps the agent: it has something to do. mu is held.
func (c *conversation) disarmIdle() {
	if c.idle != nil {
		c.idle.Stop()
		c.idle = nil
	}
}

// setReceipt sets the receipt of the user event seq. mu is held.
func (c *conversation) setReceipt(seq int, state string) {
	for i := len(c.events) - 1; i >= 0; i-- {
		if c.events[i].Seq == seq && c.events[i].Kind == "user" {
			c.events[i].Receipt = state
			return
		}
	}
}

// receiptOf is the receipt of the user event seq. mu is held.
func (c *conversation) receiptOf(seq int) string {
	for i := len(c.events) - 1; i >= 0; i-- {
		if c.events[i].Seq == seq && c.events[i].Kind == "user" {
			return c.events[i].Receipt
		}
	}
	return ""
}

// advanceReceipt moves the receipt of the user event seq forward to state:
// receipts only ever advance. mu is held.
func (c *conversation) advanceReceipt(seq int, state string) {
	for i := len(c.events) - 1; i >= 0; i-- {
		if e := &c.events[i]; e.Seq == seq && e.Kind == "user" {
			if receiptRank[state] > receiptRank[e.Receipt] {
				e.Receipt = state
			}
			return
		}
	}
}

// line takes a line of the agent's output: what it says of a prompt
// (command_lifecycle) moves that prompt's receipt; a turn begins (init: one
// it was given, or one it started by itself) and ends (result); what it has
// running in the background is kept; the rest is shown. It says what type of
// line it was.
func (c *conversation) line(raw []byte) string {
	raw = []byte(strings.TrimSpace(string(raw)))
	if len(raw) == 0 {
		return ""
	}
	if len(raw) > maxLine || !json.Valid(raw) {
		raw = []byte(`{"type":"truncated"}`)
	}
	var head struct {
		Type          string           `json:"type"`
		Subtype       string           `json:"subtype"`
		SessionID     string           `json:"session_id"`
		SlashCommands []string         `json:"slash_commands"`
		Model         string           `json:"model"`
		Status        *string          `json:"status"`
		CommandUUID   string           `json:"command_uuid"`
		State         string           `json:"state"`
		Tasks         []BackgroundTask `json:"tasks"`
		IsReplay      bool             `json:"isReplay"`
		NumTurns      *int             `json:"num_turns"`
		Result        string           `json:"result"`
		IsError       bool             `json:"is_error"`
		Mode          string           `json:"permissionMode"`
		ToolName      string           `json:"tool_name"`
		Message       string           `json:"message"`
	}
	json.Unmarshal(raw, &head)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.noteMode(raw, head.Type, head.Subtype, head.Mode)
	if SessionID.MatchString(head.SessionID) {
		c.session = head.SessionID
	}
	switch {
	case head.Type == "command_lifecycle":
		c.lifecycle(head.CommandUUID, head.State)
		return head.Type
	case head.Type == "control_response", head.Type == "rate_limit_event", head.IsReplay:
		// Answers to vibe's own requests, and what the agent says back of a
		// setting vibe changed: nothing for the page.
		return head.Type
	case head.Type == "system" && head.Subtype == "background_tasks_changed":
		c.tasks = head.Tasks
		c.armIdle()
		// Kept, so that a vibe that dies with work running has it said by the next.
		c.save()
		return head.Type
	case head.Type == "result" && head.NumTurns != nil && *head.NumTurns == 0 && head.Result == "":
		// A resumed agent's first word, before any turn: nothing answered.
		return head.Type
	case head.Type == "system" && head.Subtype == "permission_denied":
		// The agent may not say it was refused: the page does, with why, and the mode it was in.
		c.add("denied", refusal(head.ToolName, head.Message, c.modeInForce), nil)
		c.events[len(c.events)-1].Suggest = c.modeToSuggest()
		c.heard = c.clock()
		return head.Type
	}
	if head.Type == "system" && head.Subtype == "init" {
		if head.Model != "" {
			c.model = head.Model
		}
		if head.SlashCommands != nil {
			if commands := slashCommands(head.SlashCommands); !slices.Equal(commands, c.commands) {
				c.commands = commands
				c.commandsSeq++
			}
		}
		if !c.answering && c.agent != nil {
			// A turn the agent began by itself: something it left running
			// has called it back.
			c.answering, c.stopping, c.compacting = true, false, false
		}
		c.disarmIdle()
	}
	if head.Type == "system" && head.Subtype == "status" {
		c.compacting = head.Status != nil && *head.Status == "compacting"
	}
	c.heard = c.clock()
	// The agent ends a turn it was interrupted in with a result it marks as an error: it is the user's stop, not a failure, and
	// is marked so for the page.
	mark := ""
	if head.Type == "result" && c.stopping && head.IsError {
		mark = "stopped"
	}
	c.add("agent", mark, append(json.RawMessage(nil), raw...))
	if head.Type == "result" {
		if c.stopping {
			c.add("done", "stopped", nil)
		} else {
			c.add("done", "", nil)
		}
		c.answering, c.stopping, c.compacting = false, false, false
		c.armIdle()
		// The agent goes on: what it has said so far is kept now.
		c.save()
	}
	return head.Type
}

// lifecycle is what the agent says of the prompt it names id: queued (it has
// it), started (it is answering it), completed, or cancelled (dropped
// unanswered, by a stop). mu is held.
func (c *conversation) lifecycle(id, state string) {
	seq, ok := c.pending[id]
	if !ok {
		return
	}
	switch state {
	case "queued":
		c.advanceReceipt(seq, receiptDelivered)
	case "started":
		c.advanceReceipt(seq, receiptSeen)
	case "completed":
		c.advanceReceipt(seq, receiptSeen)
		delete(c.pending, id)
	case "cancelled":
		// A stop cancels the prompt the agent was answering as well as those it had not taken in: only those were never answered.
		if c.receiptOf(seq) != receiptSeen {
			c.setReceipt(seq, receiptUndelivered)
		}
		delete(c.pending, id)
	}
}

// stop stops the turn under way: the agent is interrupted, and what it runs
// in the background is left alone (stopTasks ends that). A turn whose
// agent is still being started is not started. It says whether there was a
// turn to stop.
func (c *conversation) stop() bool {
	c.mu.Lock()
	if c.starting && c.cancelStart != nil {
		c.stopping = true
		c.cancelStart()
		c.mu.Unlock()
		return true
	}
	if !c.answering || c.agent == nil {
		c.mu.Unlock()
		return false
	}
	c.stopping = true
	agent := c.agent
	c.mu.Unlock()
	if err := agent.Interrupt(); err != nil {
		c.mu.Lock()
		c.add("error", "the agent couldn't be asked to stop: "+err.Error(), nil)
		c.mu.Unlock()
	}
	return true
}

// stopTasks stops what the agent has running in the background, leaving
// the agent; it says how many it asked it to stop.
func (c *conversation) stopTasks() int {
	c.mu.Lock()
	agent, tasks := c.agent, append([]BackgroundTask(nil), c.tasks...)
	c.mu.Unlock()
	if agent == nil {
		return 0
	}
	n := 0
	for _, t := range tasks {
		if agent.StopTask(t.ID) == nil {
			n++
		}
	}
	return n
}

// forceStop ends the agent, and all it started, at once: for one that does
// not stop when asked. It says whether there was one.
func (c *conversation) forceStop() bool {
	c.mu.Lock()
	agent := c.agent
	if agent == nil {
		c.mu.Unlock()
		return c.stop()
	}
	c.stopping = c.answering
	c.mu.Unlock()
	if err := agent.Kill(); err != nil {
		c.mu.Lock()
		c.add("error", err.Error(), nil)
		c.mu.Unlock()
	}
	return true
}

// supersede makes way for something that takes the sandbox away (an Admin
// action, a new conversation): an agent with only background work, or none,
// is ended, and waited for. It says whether the way is clear (not while it is
// answering), and what background work was ended, which the conversation says.
func (c *conversation) supersede() (ended []BackgroundTask, ok bool) {
	c.mu.Lock()
	if c.busy() {
		c.mu.Unlock()
		return nil, false
	}
	agent := c.agent
	if agent != nil && len(c.tasks) > 0 {
		// The user chose the action, but may not know the agent had work
		// going, now that it can be left running through any number of prompts.
		ended = c.tasks
		c.add("note", endedWords(ended), nil)
		c.tasks = nil
		c.save()
	}
	c.mu.Unlock()
	if agent == nil {
		return nil, true
	}
	agent.Kill()
	select {
	case <-agent.Done():
		return ended, true
	case <-time.After(15 * time.Second):
		return ended, false
	}
}

// endedWords says that tasks, which the agent had running in the background, were ended to make way.
func endedWords(tasks []BackgroundTask) string {
	return "Ended what the agent had running in the background, to make way: " + taskNames(tasks) + "."
}

// afresh forgets the conversation, unless a turn is under way, and begins
// the new one saying what was done for it, and what failed, if anything. An
// agent still alive (with background work) belongs to the old one, and is
// ended.
func (c *conversation) afresh(done string, failed []string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.busy() {
		return false
	}
	if agent := c.agent; agent != nil {
		c.agent, c.tasks = nil, nil
		c.disarmIdle()
		go agent.Kill()
	}
	c.events, c.session, c.prompt, c.model, c.pending = nil, "", 0, "", nil
	// Pages showing the old events are told to clear them.
	c.add("reset", "", nil)
	if done != "" {
		c.add("note", done, nil)
	}
	for _, why := range failed {
		c.add("error", why, nil)
	}
	c.save()
	return true
}

// newPromptID is the id a prompt is given, for the agent's lifecycle lines to
// name it by: a random UUID.
func newPromptID() string {
	b := make([]byte, 16)
	rand.Read(b)
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
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
	if !c.answering || c.starting {
		return 0
	}
	return c.clock().Sub(c.heard)
}

// working says the agent has work: a turn under way (or being started), or
// something running in the background, which calls it back.
func (c *conversation) working() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.busy() || (c.agent != nil && len(c.tasks) > 0)
}

// answeringNow says a turn is under way, or being started.
func (c *conversation) answeringNow() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.busy()
}

// turn is the latest prompt's seq, and whether a turn is under way.
func (c *conversation) turn() (prompt int, answering bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.prompt, c.busy()
}

// isCompacting says the agent is compacting its context.
func (c *conversation) isCompacting() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.answering && c.compacting
}

// backgroundTasks is what the agent has running in the background.
func (c *conversation) backgroundTasks() []BackgroundTask {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.agent == nil {
		return []BackgroundTask{}
	}
	return append([]BackgroundTask{}, c.tasks...)
}

// stopState is "stopping" while a stop is under way.
func (c *conversation) stopState() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.busy() && c.stopping {
		return "stopping"
	}
	return ""
}

// since is the events after seq, whether a turn is under way, and whether,
// with none, the agent has work running in the background.
func (c *conversation) since(after int) (events []event, last int, running, background bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	events = []event{}
	for _, e := range c.events {
		// Taking back what a page never had is no news to it.
		if e.Seq > after && (e.Kind != "retract" || e.Of <= after) {
			events = append(events, e)
		}
	}
	return events, c.seq, c.busy(), !c.busy() && c.agent != nil && len(c.tasks) > 0
}

// receipts are how far the prompts still in flight have got (and the
// latest's), for a page that already shows them to catch up with.
func (c *conversation) receipts() []map[string]interface{} {
	c.mu.Lock()
	defer c.mu.Unlock()
	open := map[int]bool{c.prompt: true}
	for _, seq := range c.pending {
		open[seq] = true
	}
	for _, w := range c.waiting {
		open[w.seq] = true
	}
	out := []map[string]interface{}{}
	for _, e := range c.events {
		if e.Kind == "user" && open[e.Seq] {
			out = append(out, map[string]interface{}{"seq": e.Seq, "state": e.Receipt})
		}
	}
	return out
}

// noteSaid says something of vibe's own in the conversation: what it's doing
// before the agent can answer. It gives the note's Seq.
func (c *conversation) noteSaid(text string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.heard = c.clock()
	c.add("note", text, nil)
	return c.seq
}

// retract takes back the events with the Seqs given: they are no longer in the
// conversation, as kept or as a new page gets it, and a page that has them is
// told to drop them (a retract event for each).
func (c *conversation) retract(seqs []int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, seq := range seqs {
		c.events = slices.DeleteFunc(c.events, func(e event) bool { return e.Seq == seq })
		c.add("retract", "", nil)
		c.events[len(c.events)-1].Of = seq
	}
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
		"receipts": s.convo.receipts(),
		// What the agent has running in the background, for the page to name
		// (and, after a stop, to offer to stop too).
		"tasks": s.convo.backgroundTasks(),
		// Minutes the agent has been answering with nothing new, once that
		// is long enough to say so.
		"quietMinutes": quietMinutes(s.convo.quiet()),
		"compacting":   s.convo.isCompacting(),
		"stopState":    s.convo.stopState(),
		// The agent's permission mode, when the one it says it is in isn't the one it was asked for (else null).
		"permissionMode": s.convo.permission(),
		// The agent's permission mode, as asked for and as it says it is in, whenever that is known (else null).
		"agentMode": s.convo.modeNow(),
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

// conversationSend gives the agent a prompt. One sent while the agent is
// answering is taken into the turn under way, as the console does.
func (s *Server) conversationSend(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Prompt string `json:"prompt"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			writeError(w, http.StatusRequestEntityTooLarge, "that prompt is too long")
			return
		}
		writeError(w, http.StatusBadRequest, "expected {\"prompt\": ...}")
		return
	}
	if strings.TrimSpace(req.Prompt) == "" {
		writeError(w, http.StatusBadRequest, "there is nothing to send")
		return
	}
	seq, _ := s.convo.claim(req.Prompt, true)
	s.convo.give(seq, req.Prompt, s.cfg.StartAgent, s.cfg.Prepare)
	s.mark(activity.KindPrompt)
	writeJSON(w, http.StatusAccepted, map[string]bool{"ok": true})
}

// claimTurn makes prompt the next turn, unless the agent is answering,
// without yet giving it to the agent: launch does, or abandon ends it,
// saying why. Something the turn needs (a review to start) is made in
// between, so the agent never hears of one that isn't there, and nothing
// made is undone because the agent was busy. A review's prompts are never
// taken into a turn under way: they are turns of their own.
func (s *Server) claimTurn(prompt string) (launch func(), abandon func(error), ok bool) {
	seq, ok := s.convo.claim(prompt, false)
	if !ok {
		return nil, nil, false
	}
	launch = func() {
		s.convo.give(seq, prompt, s.cfg.StartAgent, s.cfg.Prepare)
		s.mark(activity.KindPrompt)
	}
	abandon = func(why error) { s.convo.abandon(seq, why) }
	return launch, abandon, true
}

// conversationStop stops the agent's turn: it is interrupted, and what it
// runs in the background is left alone; the page then offers to stop that
// too ({"tasks": true}). {"force": true} ends the agent and all it started, for
// one that does not stop when asked. While the agent is writing a review, a
// stop ends the review (it would be left saying the agent is reviewing, with
// nothing at work on it): so it asks first, with a 409 and the code
// "review-in-progress", unless the request says confirmed; confirmed, the turn
// is stopped and the review dropped, as closing the review does.
func (s *Server) conversationStop(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Confirmed bool `json:"confirmed"`
		Tasks     bool `json:"tasks"`
		Force     bool `json:"force"`
	}
	json.NewDecoder(r.Body).Decode(&req) // the body is optional: a stop from before it had one
	if req.Tasks {
		writeJSON(w, http.StatusOK, map[string]int{"stopped": s.convo.stopTasks()})
		return
	}
	if req.Force {
		writeJSON(w, http.StatusOK, map[string]bool{"stopped": s.convo.forceStop()})
		return
	}
	if tab := s.cfg.ReviewTab; tab != nil {
		if rv, err := tab.Store.Load(); err == nil && s.reviewWriting(rv) {
			if !req.Confirmed {
				writeJSON(w, http.StatusConflict, map[string]interface{}{
					"error": "the agent is writing a review: stopping it drops the review, which can be started again at any time",
					"code":  "review-in-progress"})
				return
			}
			rv, stopped, _, err := s.stopAndDropReview("You stopped the agent while it was still writing this review, so it was dropped: start another whenever you like.")
			if err != nil {
				writeStoreError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, map[string]interface{}{"stopped": stopped, "review": rv})
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]bool{"stopped": s.convo.stop()})
}

// sameOriginJSON refuses, and says so, anything that is not a same-origin
// POST of JSON: what a page on another site could not make.
func sameOriginJSON(w http.ResponseWriter, r *http.Request) bool {
	if ct, _, _ := strings.Cut(r.Header.Get("Content-Type"), ";"); strings.TrimSpace(ct) != "application/json" {
		writeError(w, http.StatusUnsupportedMediaType, "expected application/json")
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
