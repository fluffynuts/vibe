package main

import (
	"bytes"
	"context"
	"io"
	"regexp"
	"time"

	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"

	"vibe/internal/backup"
	"vibe/internal/cliargs"
	"vibe/internal/companion"
	"vibe/internal/gittoken"
	"vibe/internal/layout"
	"vibe/internal/sbxrun"
	"vibe/internal/state"
)

// --- the companion page's Admin tab -----------------------------------------

// companionAdmin is what the companion page's Admin tab does to the sandbox
// for target: what the CLI does, without a terminal. Whatever would ask a
// question is answered as -f would answer it, the tab having asked first.
type companionAdmin struct {
	lay          layout.Layout
	name, target string
	// listSandboxes is sbx ls (sbxrun.ListContext when nil), for tests to
	// stand in for.
	listSandboxes func(ctx context.Context) ([]sbxrun.Status, error)

	mu       sync.Mutex
	upgrades []string
	checked  bool
}

// statusWithin is how long the tab's status may take: it is normally a
// second or two of sbx calls.
const statusWithin = 20 * time.Second

// adminArgs are the arguments the tab's actions run with: nothing asked.
var adminArgs = cliargs.Args{Force: true}

func (a *companionAdmin) Status() companion.AdminStatus {
	st := companion.AdminStatus{Commands: adminCommands(a.target), Features: []companion.AdminFeature{}, Backups: []companion.AdminBackup{}}
	// A backstop: the tab polls this, and a stuck sbx mustn't leave it
	// waiting for good — it says the state is unknown, and why.
	ctx, cancel := context.WithTimeout(context.Background(), statusWithin)
	defer cancel()
	info, profileKnown, err := gatherInfo(ctx, a.lay, cliargs.Args{}, a.name, a.target)
	if ctx.Err() != nil {
		err = fmt.Errorf("sbx didn't answer within %s, so the sandbox's state is unknown — 'sbx ls' in a terminal shows whether sbx itself is stuck", statusWithin)
	}
	var text strings.Builder
	printInfo(&text, info, profileKnown)
	st.Info = text.String()
	st.Exists, st.Running, st.Profile, st.Guided = info.exists, info.running, info.profile, info.guided
	if err != nil {
		st.Error = err.Error()
	}
	if info.guided {
		offered := offeredFor(a.lay, a.lay.Features(), info.agent, info.features)
		features, labels, checked := featureChecklist(a.lay, offered, info.features)
		for i, f := range features {
			st.Features = append(st.Features, companion.AdminFeature{Name: f.Name, Label: labels[i], On: checked[i]})
		}
	}
	if backups, err := backup.List(a.lay.HomePath("backup"), a.name); err == nil {
		for _, b := range backups {
			st.Backups = append(st.Backups, companion.AdminBackup{ID: filepath.Base(b.Path), Label: b.Label()})
		}
	}
	st.GitToken = adminGitToken(a.name, a.target, info)
	return st
}

// adminGitToken is what the tab shows of the sandbox's GitHub token: only
// ever shown, with where to make or renew one. Giving vibe the token stays
// in the terminal ('vibe --git-token').
func adminGitToken(name, target string, info sandboxInfo) companion.AdminGitToken {
	now := timeNow()
	t := companion.AdminGitToken{
		Has:     info.gitToken != "",
		Source:  info.gitToken,
		Rules:   info.gitRules,
		Command: "vibe --git-token " + shellWord(target),
	}
	if !info.gitTokenExpires.IsZero() {
		t.Expiry = gittoken.DescribeExpiry(info.gitTokenExpires, now)
		t.Expired = gittoken.Expired(info.gitTokenExpires, now)
		t.Expiring = gittoken.Expiring(info.gitTokenExpires, now)
	}
	repo := repoOf(target)
	if repo != nil {
		t.Repo = repo.String()
	}
	switch {
	case !t.Has:
		// There's no asking how long here: the link makes one for as
		// long as the terminal first offers.
		t.TokenName, t.Days = "vibe "+name, tokenDays
		t.NewTokenURL = gittoken.NewTokenURL(t.TokenName, tokenDescription(name, repo), repoOwner(repo), t.Days)
	case t.Expired || t.Expiring:
		// As renewGitAccess names it: the default token is shared.
		t.TokenName = "vibe " + name
		if info.gitToken == "default" {
			t.TokenName = "vibe default"
		}
		t.RenewURL = gittoken.TokensPageURL
	}
	return t
}

// adminCommands are the CLI's ways to do what the tab does, for target.
func adminCommands(target string) map[string]string {
	path := shellWord(target)
	return map[string]string{
		companion.AdminStart:     "vibe " + path,
		companion.AdminStop:      "vibe --stop " + path,
		companion.AdminReInit:    "vibe -r -f " + path,
		companion.AdminReCompose: "vibe -C " + path,
		companion.AdminRestore:   "vibe --restore " + path,
		"upgrade":                "vibe --upgrade",
		"upgrade-all":            "vibe --upgrade -f",
	}
}

// shellWord is s as a shell word: as it is when that's safe, else in
// single quotes.
func shellWord(s string) string {
	if s != "" && strings.IndexFunc(s, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("/._-~+:", r))
	}) < 0 {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func (a *companionAdmin) Upgrades(fresh bool) []string {
	a.mu.Lock()
	if a.checked && !fresh {
		defer a.mu.Unlock()
		return a.upgrades
	}
	a.mu.Unlock()
	found := availableUpgrades()
	a.mu.Lock()
	defer a.mu.Unlock()
	a.upgrades, a.checked = found, true
	return found
}

func (a *companionAdmin) Do(ctx context.Context, action companion.AdminAction, log func(string)) error {
	teeNotes(log)
	defer teeNotes(nil)
	err := a.do(ctx, action)
	if ctx.Err() != nil {
		// Cancelled: say where that left the sandbox, which sbx can tell.
		err = fmt.Errorf("cancelled — %s", a.sandboxState())
	}
	return err
}

// sandboxState says, in words, what sbx says the sandbox is now.
func (a *companionAdmin) sandboxState() string {
	ctx, cancel := context.WithTimeout(context.Background(), statusWithin)
	defer cancel()
	statuses, err := sbxrun.ListContext(ctx)
	if err != nil {
		return "and sbx can't say what state the sandbox is in (" + err.Error() + ")"
	}
	for _, st := range statuses {
		if st.Name == a.name {
			if st.Running {
				return fmt.Sprintf("sandbox '%s' is running", a.name)
			}
			return fmt.Sprintf("sandbox '%s' is stopped: Start it, or Rebuild if it was part-way through being made", a.name)
		}
	}
	return fmt.Sprintf("sandbox '%s' is gone: Rebuild makes it again from its profile", a.name)
}

func (a *companionAdmin) do(ctx context.Context, action companion.AdminAction) error {
	if pageActionStartsAVM(action.Kind) {
		if err := vmCheck(); err != nil {
			return err
		}
	}
	switch action.Kind {
	case companion.AdminStart:
		return a.start(ctx)
	case companion.AdminStop:
		hold.begin()
		note("stopping sandbox '%s'", a.name)
		if err := sbxrun.StopQuietContext(ctx, a.name); err != nil {
			a.settle()
			return err
		}
		stoppedOnPurpose.Store(true)
		note("stopped '%s' — start it again from this page, or with 'vibe' in its folder", a.name)
		return nil
	case companion.AdminReInit:
		return a.rebuild(ctx, func() error { return nil })
	case companion.AdminReCompose:
		features := action.Features
		if features == nil {
			features = []string{}
		}
		return a.rebuild(ctx, func() error {
			return recomposeProfile(a.lay, adminArgs, a.name, a.target, false, features)
		})
	case companion.AdminRestore:
		return a.rebuild(ctx, func() error { return a.restore(action.Backup) })
	case companion.AdminMemories:
		if !sbxrun.ReachableContext(ctx, a.name) {
			return errors.New("the sandbox isn't running: its memories can only be read while it is")
		}
		saveMemories(ctx, a.lay.Home, a.name)
		note("done")
		return nil
	}
	return fmt.Errorf("unknown action %q", action.Kind)
}

// start starts the sandbox, and lets the terminal attach to it again.
func (a *companionAdmin) start(ctx context.Context) error {
	note("starting sandbox '%s'", a.name)
	if !sbxrun.StartContext(ctx, a.name) {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("sandbox '%s' didn't start — 'sbx ls' says what state it is in", a.name)
	}
	note("'%s' is running", a.name)
	stoppedOnPurpose.Store(false)
	hold.release()
	return nil
}

// rebuild removes the sandbox and creates it again, after prepare (which
// changes the profile it is built from, or doesn't), then starts it.
func (a *companionAdmin) rebuild(ctx context.Context, prepare func() error) error {
	hold.begin()
	if err := prepare(); err != nil {
		a.settle()
		return err
	}
	if err := rebuildSandbox(ctx, a.lay, adminArgs, a.name, a.target, true, false); err != nil {
		a.settle()
		return err
	}
	return a.start(ctx)
}

// settle is for an action that failed part-way: a sandbox still running
// keeps its terminal; one gone or stopped leaves the terminal waiting for
// the page (or Ctrl-C).
func (a *companionAdmin) settle() {
	if sbxrun.Reachable(a.name) {
		hold.release()
	}
}

// restore puts back the backup of the profile with id (its file name).
func (a *companionAdmin) restore(id string) error {
	profile, err := resolveReInitProfile(a.lay.Home, adminArgs, a.name, a.target)
	if err != nil {
		return err
	}
	backups, err := backup.List(a.lay.HomePath("backup"), a.name)
	if err != nil {
		return err
	}
	for _, b := range backups {
		if filepath.Base(b.Path) == id && id != "" {
			if err := backup.Restore(b.Path, a.lay.NewProfileDir(profile)); err != nil {
				return err
			}
			note("restored profile '%s' from the backup of %s", profile, b.Label())
			// As --restore does: compose it afresh from what it now records.
			return recomposeProfile(a.lay, adminArgs, a.name, a.target, false, nil)
		}
	}
	return fmt.Errorf("there is no backup %q of profile '%s'", id, profile)
}

// adminFor is the companion page's Admin tab for the sandbox, or nil when
// there's no record of it to act on.
func adminFor(lay layout.Layout, name, target string) companion.Admin {
	if _, found, err := state.Load(lay.Home, name); err != nil || !found {
		return nil
	}
	return &companionAdmin{lay: lay, name: name, target: target}
}

// --- notes, for the page too --------------------------------------------------

var (
	teeMu sync.Mutex
	tee   func(string)
)

// teeNotes sends every note to to as well as the terminal, until called
// with nil: how an action taken from the page says what it is doing there.
func teeNotes(to func(string)) {
	teeMu.Lock()
	tee = to
	teeMu.Unlock()
}

func teeNote(line string) {
	teeMu.Lock()
	to := tee
	teeMu.Unlock()
	if to != nil {
		to(line)
	}
}

// --- keeping out of the agent's screen ---------------------------------------

var (
	attachedMu sync.Mutex
	attached   bool
	heldNotes  []string
)

// setAttached says whether the terminal is the agent's now: a session is
// attached, and its full-screen interface would be written over by anything
// vibe printed. Detaching prints what was held back meanwhile.
func setAttached(now bool) {
	attachedMu.Lock()
	attached = now
	held := heldNotes
	if !now {
		heldNotes = nil
	}
	attachedMu.Unlock()
	if !now && len(held) > 0 {
		fmt.Fprintln(os.Stderr, "vibe: while the session was attached:")
		for _, line := range held {
			fmt.Fprintf(os.Stderr, "vibe:   %s\n", line)
		}
	}
}

// holdNote keeps line back from the terminal while a session is attached,
// to print once it isn't; it says whether it did.
func holdNote(line string) bool {
	attachedMu.Lock()
	defer attachedMu.Unlock()
	if !attached {
		return false
	}
	heldNotes = append(heldNotes, line)
	return true
}

// noteIfFree is note for what is only worth saying now: dropped, rather
// than held, while a session is attached (the page says it anyway).
func noteIfFree(format string, a ...interface{}) {
	attachedMu.Lock()
	busy := attached
	attachedMu.Unlock()
	if busy {
		teeNote(fmt.Sprintf(format, a...))
		return
	}
	note(format, a...)
}

// --- holding the terminal while the page acts -------------------------------

// sessionHold is how the terminal's session survives the page stopping or
// rebuilding the sandbox under it: while held, a session that ends waits
// for the sandbox to come back and attaches again, rather than ending vibe.
type sessionHold struct {
	mu     sync.Mutex
	held   bool
	resume chan struct{}
}

var hold = &sessionHold{}

// begin holds the session, before the sandbox is taken from under it.
func (h *sessionHold) begin() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.held {
		h.held, h.resume = true, make(chan struct{})
	}
}

// release lets a waiting session attach again, the sandbox being back.
func (h *sessionHold) release() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.held {
		h.held = false
		close(h.resume)
	}
}

// waiting is the channel a session that has ended waits on to attach again,
// or nil when it isn't held and should end.
func (h *sessionHold) waiting() <-chan struct{} {
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.held {
		return nil
	}
	return h.resume
}

// awaitResume waits, after the session has ended under a hold, for the
// sandbox to be back: true then, false when vibe is to end instead (Ctrl-C,
// or another vibe asking it to stop).
func awaitResume(resume <-chan struct{}, stop <-chan struct{}) bool {
	interrupts := make(chan os.Signal, 1)
	signal.Notify(interrupts, os.Interrupt)
	defer signal.Stop(interrupts)
	select {
	case <-resume:
		return true
	case <-stop:
		return false
	case <-interrupts:
		return false
	}
}

// actionOutput is where what sbx prints should go: while a companion page's
// action runs (its notes are teed to the page), to that action's log, as
// plain lines, since the terminal is then the session's agent's screen; and
// nil, the terminal, otherwise.
func actionOutput() io.Writer {
	teeMu.Lock()
	to := tee
	teeMu.Unlock()
	if to == nil {
		return nil
	}
	return &lineWriter{line: to}
}

// lineWriter passes what's written to it on, a line at a time, without the
// terminal's colours and cursor moves; blank lines are dropped.
type lineWriter struct {
	line    func(string)
	partial []byte
}

var terminalEscapes = regexp.MustCompile(`\x1b\[[0-9;?]*[A-Za-z]`)

func (w *lineWriter) Write(p []byte) (int, error) {
	w.partial = append(w.partial, p...)
	for {
		i := bytes.IndexAny(w.partial, "\r\n")
		if i < 0 {
			return len(p), nil
		}
		line := strings.TrimRight(terminalEscapes.ReplaceAllString(string(w.partial[:i]), ""), " \t")
		w.partial = w.partial[i+1:]
		if line != "" {
			w.line(line)
		}
	}
}
