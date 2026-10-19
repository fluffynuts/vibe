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
	// Fetch fetches r onto the host, giving what to tell the asker: what
	// was fetched, or on failure the ecosystem's own words.
	Fetch(ctx context.Context, r Request) (string, error)
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
	// Parallel is how many fetches run at once; DefaultParallel when 0.
	Parallel int
	// Log says what's asked and what came of it, in vibe's own output.
	Log func(string)

	mu      sync.Mutex
	pending []*pending
	running int
	work    sync.WaitGroup
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

// dispatch starts fetches for approved requests, while there's room.
func (d *Desk) dispatch(ctx context.Context) {
	for {
		d.mu.Lock()
		var p *pending
		if d.running < d.parallel() {
			for _, candidate := range d.pending {
				if candidate.Approved && !candidate.Fetching {
					p = candidate
					break
				}
			}
		}
		if p == nil {
			d.mu.Unlock()
			return
		}
		p.Fetching = true
		d.running++
		d.work.Add(1)
		d.mu.Unlock()
		go d.fetch(ctx, p)
	}
}

func (d *Desk) fetch(ctx context.Context, p *pending) {
	defer d.work.Done()
	message, err := d.Ecosystems[p.Request.Ecosystem].Fetch(ctx, p.Request)

	d.mu.Lock()
	d.remove(p)
	d.running--
	nonces := append([]string(nil), p.nonces...)
	d.mu.Unlock()
	status := Fetched
	if err != nil {
		status = Failed
		if message == "" {
			message = err.Error()
		}
		d.log("package request failed: %s: %v", describe(p.Request), err)
	} else {
		d.log("package request fetched: %s", describe(p.Request))
	}
	for _, nonce := range nonces {
		d.reply(nonce, status, message)
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

// Allow approves the held requests with these keys: they're fetched as room
// comes, from the next Poll. A key that isn't held (it was answered meanwhile) is passed over.
func (d *Desk) Allow(keys []string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, key := range keys {
		for _, p := range d.pending {
			if p.Key == key {
				p.Approved = true
			}
		}
	}
}

// Deny answers the held requests with these keys that way, unfetched. One
// already approved is let be: it is queued or being fetched.
func (d *Desk) Deny(keys []string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, key := range keys {
		for _, p := range append([]*pending(nil), d.pending...) {
			if p.Key == key && !p.Approved {
				d.remove(p)
				d.log("package request denied: %s", describe(p.Request))
				for _, nonce := range p.nonces {
					d.reply(nonce, Denied, "the user denied this request: don't look for another way to this package; say so, and ask them")
				}
			}
		}
	}
}

// DenyAll denies every request held.
func (d *Desk) DenyAll() {
	var keys []string
	for _, p := range d.Pending() {
		keys = append(keys, p.Key)
	}
	d.Deny(keys)
}
