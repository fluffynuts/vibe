package main

import (
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

	mu       sync.Mutex
	upgrades []string
	checked  bool
}

// adminArgs are the arguments the tab's actions run with: nothing asked.
var adminArgs = cliargs.Args{Force: true}

func (a *companionAdmin) Status() companion.AdminStatus {
	st := companion.AdminStatus{Commands: adminCommands(a.target), Features: []companion.AdminFeature{}, Backups: []companion.AdminBackup{}}
	info, profileKnown, err := gatherInfo(a.lay, cliargs.Args{}, a.name, a.target)
	var text strings.Builder
	printInfo(&text, info, profileKnown)
	st.Info = text.String()
	st.Exists, st.Running, st.Profile, st.Guided = info.exists, info.running, info.profile, info.guided
	if err != nil {
		st.Error = err.Error()
	}
	if info.guided {
		features, labels, checked := featureChecklist(a.lay, a.lay.Features(), info.features)
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

func (a *companionAdmin) Do(action companion.AdminAction, log func(string)) error {
	teeNotes(log)
	defer teeNotes(nil)
	switch action.Kind {
	case companion.AdminStart:
		return a.start()
	case companion.AdminStop:
		hold.begin()
		note("stopping sandbox '%s'", a.name)
		if err := sbxrun.StopQuiet(a.name); err != nil {
			a.settle()
			return err
		}
		note("stopped '%s' — start it again from this page, or with 'vibe' in its folder", a.name)
		return nil
	case companion.AdminReInit:
		return a.rebuild(func() error { return nil })
	case companion.AdminReCompose:
		features := action.Features
		if features == nil {
			features = []string{}
		}
		return a.rebuild(func() error {
			return recomposeProfile(a.lay, adminArgs, a.name, a.target, false, features)
		})
	case companion.AdminRestore:
		return a.rebuild(func() error { return a.restore(action.Backup) })
	case companion.AdminMemories:
		if !sbxrun.Reachable(a.name) {
			return errors.New("the sandbox isn't running: its memories can only be read while it is")
		}
		saveMemoriesOnExit(a.lay.Home, a.name)
		note("done")
		return nil
	}
	return fmt.Errorf("unknown action %q", action.Kind)
}

// start starts the sandbox, and lets the terminal attach to it again.
func (a *companionAdmin) start() error {
	note("starting sandbox '%s'", a.name)
	if !sbxrun.Start(a.name) {
		return fmt.Errorf("sandbox '%s' didn't start — 'sbx ls' says what state it is in", a.name)
	}
	note("'%s' is running", a.name)
	hold.release()
	return nil
}

// rebuild removes the sandbox and creates it again, after prepare (which
// changes the profile it is built from, or doesn't), then starts it.
func (a *companionAdmin) rebuild(prepare func() error) error {
	hold.begin()
	if err := prepare(); err != nil {
		a.settle()
		return err
	}
	if err := rebuildSandbox(a.lay, adminArgs, a.name, a.target, true, false); err != nil {
		a.settle()
		return err
	}
	return a.start()
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
