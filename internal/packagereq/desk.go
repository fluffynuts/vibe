package packagereq

import (
	"context"
	"fmt"
	"sync"
)

// Ecosystem is a kind of package the host can fetch (NuGet; npm one day).
type Ecosystem interface {
	// Validate says what's wrong with r, in the agent's terms, or nil.
	// Nothing of r is used before it's valid.
	Validate(r Request) error
	// Fetch fetches rs onto the host, together: what is approved together
	// comes together, and what the ecosystem fails on one request doesn't
	// keep the others from coming. It gives one outcome for each, in order.
	Fetch(ctx context.Context, rs []Request) []Outcome
}

// Outcome is what came of one request in a Fetch: what to tell its asker (what
// was fetched or, on failure, the ecosystem's own words) and, when it failed, why.
type Outcome struct {
	Message string
	Err     error
}

// Policy values for Desk.Policy: "ask" holds each request for the user's
// approval, "allow" fetches at once. Anything else is taken as "ask".
const (
	PolicyAsk   = "ask"
	PolicyAllow = "allow"
)

// Desk takes in a sandbox's requests, holds them for the user's approval or
// fetches them at once, and answers.
type Desk struct {
	Mailbox    Mailbox
	Ecosystems map[string]Ecosystem
	// Policy is asked at each request, so a change of setting applies to
	// the next one.
	Policy func() string
	// Unavailable, when set, is why no request can be carried out: each is
	// answered with it, unfetched.
	Unavailable string
	// Describe says what the user should know of a request in the project's
	// terms (a new package, an upgrade from 1.0.0), worked out by vibe from
	// the project, never from the agent's word; nil says nothing.
	Describe func(Request) string
	// Overview says what is waiting for the user, for an agent that asks (KindStatus); a desk
	// with none says it can't.
	Overview func() string
	// Parallel is how many fetches run at once; DefaultParallel when 0.
	Parallel int
	// Log says what's asked and what came of it, in vibe's own output.
	Log func(string)

	mu      sync.Mutex
	pending []*pending
	running int
	work    sync.WaitGroup
	// looking is held while the overview is worked out, for one request at a time.
	looking sync.Mutex
}

// DefaultParallel is how many fetches run at once: enough to hide each one's
// start-up and round trips, few enough not to flood the user's feeds.
const DefaultParallel = 3

type pending struct {
	Pending
	nonces []string
}

// Pending is a request held for the user.
type Pending struct {
	// Key stands for the request, for the page to say which it means.
	Key     string
	Request Request
	// Approved is a request the user has allowed (or policy has): it is
	// fetched when its turn comes, and can't be taken back.
	Approved bool
	// Note is Describe's, for the page to show with the request.
	Note string
	// Fetching is a request being fetched now.
	Fetching bool
}

func keyOf(r Request) string {
	return r.Ecosystem + "|" + r.ID + "|" + r.Version + "|" + r.Framework
}

func (d *Desk) log(format string, args ...interface{}) {
	if d.Log != nil {
		d.Log(fmt.Sprintf(format, args...))
	}
}

func describe(r Request) string {
	if r.Framework != "" {
		return fmt.Sprintf("%s %s %s (%s)", r.Ecosystem, r.ID, r.Version, r.Framework)
	}
	return fmt.Sprintf("%s %s %s", r.Ecosystem, r.ID, r.Version)
}

func (d *Desk) reply(nonce string, status Status, message string) {
	if err := d.Mailbox.Reply(nonce, Answer{Status: status, Message: message}); err != nil {
		d.log("package request %s couldn't be answered: %v", nonce, err)
	}
}

const waitingMessage = "waiting for the user to approve it on the companion page's Admin tab (vibe has to be running for this sandbox)"

// Poll takes in what's in the inbox, and starts fetching what's approved.
// It doesn't wait for the fetches: requests keep being taken in while they
// run, and each one finished starts the next.
func (d *Desk) Poll(ctx context.Context) {
	for _, taken := range d.Mailbox.Take() {
		d.take(taken)
	}
	d.dispatch(ctx)
}

// Wait waits for the fetches under way, and those that follow them.
func (d *Desk) Wait() { d.work.Wait() }

func (d *Desk) refuse(t Taken, why string) {
	d.log("package request refused: %s", why)
	if t.Nonce != "" {
		d.reply(t.Nonce, Failed, why)
	}
}

func (d *Desk) take(t Taken) {
	if t.Err != nil {
		d.refuse(t, t.Err.Error())
		return
	}
	if d.Unavailable != "" {
		d.refuse(t, d.Unavailable)
		return
	}
	switch t.Request.Kind {
	case "":
	case KindStatus:
		d.status(t)
		return
	default:
		d.refuse(t, fmt.Sprintf("there is no kind of request %q", t.Request.Kind))
		return
	}
	eco, ok := d.Ecosystems[t.Request.Ecosystem]
	if !ok {
		d.refuse(t, fmt.Sprintf("there's no way here to fetch %q packages", t.Request.Ecosystem))
		return
	}
	if err := eco.Validate(t.Request); err != nil {
		d.refuse(t, err.Error())
		return
	}

	key := keyOf(t.Request)
	// Describing can be slow (it reads the project), so it's done without
	// holding what the page reads. Only Poll adds requests, on one
	// goroutine, so nothing else can hold this key meanwhile.
	note := ""
	if d.Describe != nil && !d.holds(key) {
		note = d.Describe(t.Request)
	}

	d.mu.Lock()
	defer d.mu.Unlock()
	var p *pending
	for _, existing := range d.pending {
		if existing.Key == key {
			p = existing
		}
	}
	if p == nil {
		p = &pending{Pending: Pending{Key: key, Request: t.Request, Note: note}}
		d.pending = append(d.pending, p)
		d.log("package request: %s", describe(t.Request))
		if d.Policy != nil && d.Policy() == PolicyAllow {
			p.Approved = true
		}
	}
	p.nonces = append(p.nonces, t.Nonce)
	if !p.Approved {
		d.reply(t.Nonce, Waiting, waitingMessage)
	}
}

// status answers a request for what is waiting for the user, from what the host knows when it looks.
func (d *Desk) status(t Taken) {
	if d.Overview == nil {
		d.refuse(t, "the host has nothing to say of what is waiting for the user")
		return
	}
	if t.Nonce == "" {
		return
	}
	// Saying it can take a look at the whole workspace: not in the loop that takes requests in, which
	// the ones behind it, and the fetches to start, would wait on; and one look at a time.
	d.work.Add(1)
	go func() {
		defer d.work.Done()
		d.looking.Lock()
		defer d.looking.Unlock()
		d.reply(t.Nonce, Info, d.Overview())
	}()
}

func (d *Desk) holds(key string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, p := range d.pending {
		if p.Key == key {
			return true
		}
	}
	return false
}

func (d *Desk) parallel() int {
	if d.Parallel > 0 {
		return d.Parallel
	}
	return DefaultParallel
}

// dispatch starts fetches for approved requests, while there's room: all that
// are approved and waiting, of one ecosystem, go together.
func (d *Desk) dispatch(ctx context.Context) {
	for {
		d.mu.Lock()
		var batch []*pending
		if d.running < d.parallel() {
			batch = d.nextBatch()
		}
		if batch == nil {
			d.mu.Unlock()
			return
		}
		for _, p := range batch {
			p.Fetching = true
		}
		d.running++
		d.work.Add(1)
		d.mu.Unlock()
		go d.fetch(ctx, batch)
	}
}

// nextBatch is every approved request not yet being fetched that is of the
// first one's ecosystem, or nil; the caller holds d.mu.
func (d *Desk) nextBatch() []*pending {
	var batch []*pending
	for _, candidate := range d.pending {
		if !candidate.Approved || candidate.Fetching {
			continue
		}
		if len(batch) == 0 || candidate.Request.Ecosystem == batch[0].Request.Ecosystem {
			batch = append(batch, candidate)
		}
	}
	return batch
}

func (d *Desk) fetch(ctx context.Context, batch []*pending) {
	defer d.work.Done()
	requests := make([]Request, len(batch))
	for i, p := range batch {
		requests[i] = p.Request
	}
	outcomes := d.Ecosystems[requests[0].Ecosystem].Fetch(ctx, requests)

	d.mu.Lock()
	for _, p := range batch {
		d.remove(p)
	}
	d.running--
	d.mu.Unlock()
	for i, p := range batch {
		outcome := Outcome{Err: fmt.Errorf("the %s fetcher gave no outcome for this request", p.Request.Ecosystem)}
		if i < len(outcomes) {
			outcome = outcomes[i]
		}
		status := Fetched
		message := outcome.Message
		if outcome.Err != nil {
			status = Failed
			if message == "" {
				message = outcome.Err.Error()
			}
			d.log("package request failed: %s: %v", describe(p.Request), outcome.Err)
		} else {
			d.log("package request fetched: %s", describe(p.Request))
		}
		for _, nonce := range p.nonces {
			d.reply(nonce, status, message)
		}
	}
	d.dispatch(ctx)
}

// remove forgets p; the caller holds d.mu.
func (d *Desk) remove(p *pending) {
	for i, existing := range d.pending {
		if existing == p {
			d.pending = append(d.pending[:i], d.pending[i+1:]...)
			return
		}
	}
}

// Pending is what's held for the user, oldest first.
func (d *Desk) Pending() []Pending {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make([]Pending, 0, len(d.pending))
	for _, p := range d.pending {
		out = append(out, p.Pending)
	}
	return out
}

// Decide is the user's word on the held requests: those in allow are approved, and fetched as room
// comes, from the next Poll; those in notSelected are answered at once, unfetched, as not selected
// (which may be a slip, and the agent is to ask). A request in both is allowed; one in neither is
// still held (it came after the user's list was drawn, so they haven't seen it); a key that isn't
// held is passed over. One already approved is let be: it is queued or being fetched.
func (d *Desk) Decide(allow, notSelected []string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, key := range allow {
		for _, p := range d.pending {
			if p.Key == key {
				p.Approved = true
			}
		}
	}
	// What was just approved is let be by answerNo, so a key in both lists is allowed.
	d.answerNo(notSelected, "not selected", notSelectedMessage)
}

// DeclineAll answers every request not yet approved as declined, unfetched: the user's own word.
func (d *Desk) DeclineAll() {
	d.mu.Lock()
	defer d.mu.Unlock()
	var keys []string
	for _, p := range d.pending {
		keys = append(keys, p.Key)
	}
	d.answerNo(keys, "declined", declinedMessage)
}

// answerNo answers the held, unapproved requests with these keys Denied, in words of say's making;
// the caller holds d.mu.
func (d *Desk) answerNo(keys []string, how string, say func(Request) string) {
	for _, key := range keys {
		for _, p := range append([]*pending(nil), d.pending...) {
			if p.Key == key && !p.Approved {
				d.remove(p)
				d.log("package request %s: %s", how, describe(p.Request))
				for _, nonce := range p.nonces {
					d.reply(nonce, Denied, say(p.Request))
				}
			}
		}
	}
}

// The messages the agent is told a request was not fetched in are short, and say what to do before anything
// else: an agent may show only the first part of what it's told. They ask for a friendly, open tone by showing it
// (an example question), and say that nothing is broken, instead of forbidding the stiffness or the guessing.
func notSelectedMessage(r Request) string {
	return fmt.Sprintf("Not fetched: the user left out %[1]s %[2]s, so it's off their list. Nothing is broken: it's their choice, and it may be a slip. "+
		"Don't look for another way to get it. Say what you're blocked on, and ask kindly and openly, for example: "+
		"\"Did you mean to leave out %[1]s? If not, I'll ask again and you can approve it; if you did, what shall we do instead?\" "+
		"To ask again, run vibe-nuget fetch for it: that raises a fresh request (don't send them to approve the old one).", r.ID, r.Version)
}

func declinedMessage(r Request) string {
	return fmt.Sprintf("Not fetched: the user declined %s %s. Nothing is broken: it's their choice, and they may already have the package or want something else. "+
		"Don't look for another way to get it. Say what you're blocked on, and ask kindly what they'd like instead.", r.ID, r.Version)
}
