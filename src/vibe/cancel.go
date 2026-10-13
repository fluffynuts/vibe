package main

import (
	"strings"
	"sync"
	"time"

	"vibe/internal/running"
)

// cancelAnswerWithin is a backstop for --cancel: the vibe asked answers
// within a second; one that hasn't by then is stuck, and is said to be.
const cancelAnswerWithin = 10 * time.Second

// doCancel is --cancel: it asks each vibe running for target to cancel what
// its companion page has under way — the agent's turn, an Admin action —
// and says what each ended. The sandbox keeps running.
func doCancel(vibeHome, target string) error {
	others := running.Others(vibeHome, target)
	if len(others) == 0 {
		note("no vibe is running for %s — nothing to cancel", target)
		return nil
	}
	var failed error
	for _, pid := range others {
		answer, err := running.AskToCancel(vibeHome, pid, cancelAnswerWithin)
		if err != nil {
			note("%s", err)
			failed = err
			continue
		}
		note("%s", answer)
	}
	return failed
}

var (
	pageCancelMu sync.Mutex
	pageCancel   func() []string
)

// setPageCancel says how to cancel what this vibe's companion page has
// under way; nil while there's no page.
func setPageCancel(cancel func() []string) {
	pageCancelMu.Lock()
	pageCancel = cancel
	pageCancelMu.Unlock()
}

// cancelPage cancels what the page has under way, if there's a page, and
// says what it ended.
func cancelPage() []string {
	pageCancelMu.Lock()
	cancel := pageCancel
	pageCancelMu.Unlock()
	if cancel == nil {
		return nil
	}
	return cancel()
}

// answerCancels answers each --cancel this vibe is asked, until done is
// closed (nil: for as long as the process lives), by cancelling what its
// companion page has under way.
func answerCancels(s *running.Session, cancel func() []string, done <-chan struct{}) {
	for {
		select {
		case <-done:
			return
		case <-s.CancelRequested():
			ended := cancel()
			answer := "nothing was running to cancel"
			if len(ended) > 0 {
				answer = "cancelled " + strings.Join(ended, " and ")
			}
			s.AnswerCancel(answer)
		}
	}
}
