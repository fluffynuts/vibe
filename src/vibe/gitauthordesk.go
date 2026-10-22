package main

import (
	"context"
	"fmt"
	"sync"

	"vibe/internal/companion"
	"vibe/internal/layout"
	"vibe/internal/state"
)

// authorDesk is the headless session's way of settling that the agent can
// commit, where there is no terminal to ask on: it finds out when the sandbox
// starts (Check), and the companion page asks and answers (State, Decide).
//
// The question is "ask" until the user chooses; "later" turns it into a
// "reminder" they can dismiss or reopen; and the next Check, at the next
// start of the sandbox, asks again, since nothing was settled.
type authorDesk struct {
	// assess looks at the sandbox's git author, given the gitAuthor setting.
	assess func(preference string) authorFinding
	// preference is the gitAuthor setting, as the profile and the user's
	// settings have it.
	preference func() (string, error)
	// push says whether the agent can push, for the question.
	push func() string
	// give puts the author in the sandbox; keep records the user's choice (gitAuthorImport
	// or gitAuthorLeave) in the profile.
	give func(name, email string) error
	keep func(choice string) error
	say  func(format string, a ...interface{})

	mu       sync.Mutex
	phase    string
	finding  authorFinding
	pushText string
	// deciding is an answer being carried out, which a second must wait out:
	// two tabs answering together give the author once.
	deciding bool
}

// Check looks at the sandbox, as it starts.
func (d *authorDesk) Check() {
	preference, err := d.preference()
	if err != nil {
		d.say("WARNING: couldn't read the settings, so can't tell whether the agent can commit: %v", err)
		return
	}
	found := d.assess(preference)
	switch found.Kind {
	case authorUnknown:
		d.say("WARNING: %s", found.Why)
	case authorOffer:
		if preference == gitAuthorImport {
			if err := d.give(found.GiveName, found.GiveEmail); err != nil {
				d.say("WARNING: couldn't give the agent a git author: %v", err)
				return
			}
			d.say("the agent commits as %s", found.Shown)
			found.Kind = authorFine
		}
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.phase = ""
	if found.Kind == authorOffer {
		d.phase, d.finding, d.pushText = "ask", found, d.push()
	}
}

// State is what the page shows now.
func (d *authorDesk) State() companion.GitAuthorState {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.phase == "" {
		return companion.GitAuthorState{}
	}
	return companion.GitAuthorState{Phase: d.phase, Shown: d.finding.Shown, Push: d.pushText}
}

// Decide is the user's answer: give (the author), leave (the sandbox
// without), later (ask me again, and remind me meanwhile), fix (from the
// reminder, to be asked now) or dismiss (the reminder, until the sandbox is
// started again). An answer that makes no sense now, or while another is
// being carried out, is refused, and the page asks again of what is so.
func (d *authorDesk) Decide(answer string) error {
	d.mu.Lock()
	if d.deciding {
		d.mu.Unlock()
		return fmt.Errorf("%w: another answer is being carried out: look at the page again", companion.ErrAnswerNotNow)
	}
	phase, finding := d.phase, d.finding
	allowed := map[string]bool{
		"give": phase != "", "leave": phase != "", "later": phase == "ask",
		"fix": phase == "reminder", "dismiss": phase == "reminder",
	}
	if !allowed[answer] {
		d.mu.Unlock()
		return fmt.Errorf("%w: there is nothing to answer %q to now (it may have been answered already): look at the page again", companion.ErrAnswerNotNow, answer)
	}
	switch answer {
	case "later":
		d.phase = "reminder"
	case "fix":
		d.phase = "ask"
	case "dismiss":
		d.phase = ""
	default:
		d.deciding = true
	}
	d.mu.Unlock()
	if answer != "give" && answer != "leave" {
		return nil
	}

	var err error
	if answer == "give" {
		err = d.give(finding.GiveName, finding.GiveEmail)
		if err != nil {
			err = fmt.Errorf("couldn't give the agent a git author: %w", err)
		} else if keepErr := d.keep(gitAuthorImport); keepErr != nil {
			d.say("WARNING: the agent has its git author, but your choice couldn't be kept, so you'll be asked again: %v", keepErr)
		}
	} else if err = d.keep(gitAuthorLeave); err != nil {
		err = fmt.Errorf("couldn't keep your choice: %w", err)
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.deciding = false
	if err == nil {
		d.phase = ""
	}
	return err
}

// gitAuthorDesk is the headless session's desk, which its companion page
// asks through and its start-up checks: nil in a session with a terminal, which
// asks there (see checkSandboxGitAuthor).
var gitAuthorDesk *authorDesk

// newAuthorDesk is the desk for sandbox name, as inst records it.
func newAuthorDesk(lay layout.Layout, name string, inst state.Instance) *authorDesk {
	ctx := context.Background()
	nothingSaid := func(string, ...interface{}) {}
	return &authorDesk{
		assess: func(preference string) authorFinding {
			return assessAuthor(gitAuthorDeps(ctx, name, nothingSaid, func(string) bool { return false }), preference)
		},
		preference: func() (string, error) {
			return gitAuthorPreference(lay, inst.Profile)
		},
		push: func() string {
			if fresh, found, err := state.Load(lay.Home, name); err == nil && found {
				return pushStatement(fresh)
			}
			return pushStatement(inst)
		},
		give: func(n, e string) error { return giveSandboxGitAuthor(ctx, name, n, e) },
		keep: func(choice string) error { return keepGitAuthorChoice(lay, inst.Profile, choice) },
		say:  func(format string, a ...interface{}) { note(format, a...) },
	}
}
