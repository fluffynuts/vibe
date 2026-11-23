package main

import (
	"context"
	"sync"
	"time"

	"vibe/internal/sbxrun"
)

// knownSandboxesFresh is how old what sbx last listed may be before the Admin tab's dot asks again, in the background.
const knownSandboxesFresh = 10 * time.Minute

// knownSandboxes is what sbx last listed, by name, kept for what must never wait on sbx (the page's state poll, and so the Admin
// tab's dot): vibe's records can outlive their sandbox (sbx rm, a reset), and one sbx doesn't know is no sandbox to remind of.
// Anything in vibe that lists sbx tells it (noteList); asked for it, it answers at once from what it has, asking sbx again in the
// background when that is missing or old, one ask at a time.
type knownSandboxes struct {
	list func(ctx context.Context) ([]sbxrun.Status, error)
	now  func() time.Time

	mu         sync.Mutex
	names      map[string]bool
	at         time.Time
	refreshing bool
}

// sbxKnows is vibe's own: what every sbx ls it makes tells.
var sbxKnows = &knownSandboxes{list: sbxrun.List, now: time.Now}

// noteList keeps what an sbx ls said.
func (k *knownSandboxes) noteList(statuses []sbxrun.Status) {
	names := make(map[string]bool, len(statuses))
	for _, s := range statuses {
		names[s.Name] = true
	}
	k.mu.Lock()
	k.names, k.at = names, k.now()
	k.mu.Unlock()
}

// known is what sbx last listed (a copy), and whether it has been listed at all; an answer missing or older than fresh is asked
// for again, in the background.
func (k *knownSandboxes) known(fresh time.Duration) (map[string]bool, bool) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if (k.names == nil || k.now().Sub(k.at) > fresh) && !k.refreshing {
		k.refreshing = true
		go k.refresh()
	}
	if k.names == nil {
		return nil, false
	}
	out := make(map[string]bool, len(k.names))
	for name := range k.names {
		out[name] = true
	}
	return out, true
}

func (k *knownSandboxes) refresh() {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	statuses, err := k.list(ctx)
	if err == nil {
		k.noteList(statuses)
	}
	k.mu.Lock()
	k.refreshing = false
	k.mu.Unlock()
}

// expiredForDot is the sandboxes with an expired token for the Admin tab's dot: only those sbx has confirmed it knows, and none
// until it has answered once (the dot then comes on a poll later, rather than for a sandbox that may be gone).
func expiredForDot(vibeHome string, now time.Time, k *knownSandboxes) []string {
	known, listed := k.known(knownSandboxesFresh)
	if !listed {
		return nil
	}
	return expiredGitTokens(vibeHome, now, known)
}
