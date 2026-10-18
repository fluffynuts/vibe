package main

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"vibe/internal/browser"
	"vibe/internal/claudetheme"
	"vibe/internal/cliargs"
	"vibe/internal/companion"
	"vibe/internal/layout"
	"vibe/internal/policy"
	"vibe/internal/portalloc"
	"vibe/internal/review"
	"vibe/internal/sbxrun"
	"vibe/internal/state"
)

// What vibe does about the companion page: "open" serves it and opens it in
// the browser, "serve" serves it and prints the URL, "off" doesn't serve it.
const (
	companionOpen  = "open"
	companionServe = "serve"
	companionOff   = "off"
)

// companionBasePort is where the search for a companion port starts: well
// clear of the service ports (5390, 5391 and the ones a profile counts up
// through from them). Once a sandbox has one, it keeps it, so the page's
// URL (and a bookmark of it) is the same at every start.
const (
	companionBasePort = 10000
	companionPortSpan = 60
)

// companionMode settles what to do from -N and the companion setting. An
// unset setting means "open"; one that isn't a mode is an error, since
// guessing which way a typo meant would either open pages the user turned
// off or hide ones they asked for.
func companionMode(noCompanion bool, setting string) (string, error) {
	if noCompanion {
		return companionOff, nil
	}
	switch strings.ToLower(strings.TrimSpace(setting)) {
	case "", companionOpen:
		return companionOpen, nil
	case companionServe:
		return companionServe, nil
	case companionOff:
		return companionOff, nil
	}
	return "", fmt.Errorf("companion: %q is not one of open, serve, off", setting)
}

// chooseCompanionPort picks the host port for a sandbox's companion page.
// The one the sandbox had last time is kept, if it's free — or if what
// holds it is this sandbox's own page, served by another vibe attached to
// it, in which case served is true and there is nothing to start. Otherwise
// it is the first port from companionBasePort that no other sandbox
// publishes or serves a page on, and nothing is listening on.
func chooseCompanionPort(instances []state.Instance, name string, inUse func(int) bool, isOurs func(int) bool) (port int, served bool, err error) {
	taken := map[int]bool{}
	remembered := 0
	for _, inst := range instances {
		if inst.Name == name {
			remembered = inst.CompanionPort
		} else if inst.CompanionPort != 0 {
			taken[inst.CompanionPort] = true
		}
		for _, rec := range inst.Publish {
			taken[rec.HostPort] = true
		}
	}
	return choosePagePort(taken, remembered, inUse, isOurs)
}

// chooseManagePort is chooseCompanionPort for the Profiles page served on
// its own, which was last on remembered: clear of every sandbox's page and
// published ports.
func chooseManagePort(instances []state.Instance, remembered int, inUse func(int) bool, isOurs func(int) bool) (port int, served bool, err error) {
	taken := map[int]bool{}
	for _, inst := range instances {
		if inst.CompanionPort != 0 {
			taken[inst.CompanionPort] = true
		}
		for _, rec := range inst.Publish {
			taken[rec.HostPort] = true
		}
	}
	return choosePagePort(taken, remembered, inUse, isOurs)
}

// choosePagePort keeps the port a page had last time, remembered, if it's
// free (or what holds it is this same page, served by another vibe: served
// is then true); else it's the first port from companionBasePort that isn't
// taken and nothing is listening on.
func choosePagePort(taken map[int]bool, remembered int, inUse func(int) bool, isOurs func(int) bool) (port int, served bool, err error) {
	if remembered != 0 && !taken[remembered] {
		if !inUse(remembered) {
			return remembered, false, nil
		}
		if isOurs(remembered) {
			return remembered, true, nil
		}
	}
	for p := companionBasePort; p < companionBasePort+companionPortSpan; p++ {
		if !taken[p] && !inUse(p) {
			return p, false, nil
		}
	}
	return 0, false, fmt.Errorf("no free port found in %d-%d", companionBasePort, companionBasePort+companionPortSpan-1)
}

// clipboardPageURL is where the sandbox's clipboard page can be reached on
// this machine, or "" when its profile doesn't have one. It is built the
// way the URL written into the sandbox is: from the host port sbx says the
// mapping has right now, the recorded one standing in while sbx can't say.
func clipboardPageURL(vibeHome, name string) string {
	inst, found, err := state.Load(vibeHome, name)
	if err != nil || !found {
		return ""
	}
	live, _ := sbxrun.PublishedPorts(name)
	for _, u := range publishedURLs(inst.Publish, live) {
		if u.name == "clipboard" {
			return u.url()
		}
	}
	return ""
}

// runningNow says whether sandbox name is running, without starting it if
// it isn't: what the companion page asks of its own accord mustn't wake a
// sandbox sbx has stopped while nobody was using it, nor keep it awake. Not
// knowing counts as not running. A variable for tests.
var runningNow = func(ctx context.Context, name string) bool {
	ctx, cancel := context.WithTimeout(ctx, statusWithin)
	defer cancel()
	running, _ := sbxrun.Running(ctx, name)
	return running
}

// clipboardTrouble is the last error start-clipboard logged in the sandbox
// (it starts each run with a "===" line, so only this run's count), or "".
func clipboardTrouble(name string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	// Asked while the clipboard page isn't up, which a stopped sandbox's
	// isn't: running something in it to ask would start it again.
	if !runningNow(ctx, name) {
		return ""
	}
	out, err := sbxrun.ExecStdout(ctx, name, "sh", "-c",
		`[ -f /tmp/clipboard.log ] && sed -n '/^=== start-clipboard/h; /^=== start-clipboard/!H; $!d; x; p' /tmp/clipboard.log | grep '^ERROR: ' | tail -n 1`)
	if err != nil {
		return ""
	}
	return strings.TrimPrefix(strings.TrimSpace(out), "ERROR: ")
}

// startClipboardScript starts the clipboard page's server in the sandbox,
// and does nothing when it is already running (see the clipboard-bridge kit).
const startClipboardScript = "/home/agent/.local/bin/start-clipboard"

// startClipboard is the companion page's "start it" for a clipboard page
// that hasn't come up: it runs what on-start would have, and waits for it
// (start-clipboard itself gives the server ten seconds to listen).
func startClipboard(name string) ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	args := []string{"exec", name, "--", startClipboardScript}
	if out, err := sbxrun.ExecCaptureContext(ctx, name, startClipboardScript); err != nil {
		if out = strings.TrimSpace(out); out != "" {
			return args, fmt.Errorf("start-clipboard failed: %s", out)
		}
		return args, fmt.Errorf("start-clipboard failed: %v — see /tmp/clipboard.log in the sandbox", err)
	}
	return args, nil
}

// themeReadTimeout bounds reading the agent's theme from its sandbox: what
// it reads is the agent's to make, and could be made never to end.
const themeReadTimeout = 15 * time.Second

// themeColorFor reads the colour of the theme the sandbox's agent uses, for
// the companion page: as the agent has it now (it may have been changed with
// /theme), else as its profile recorded it. Only claude has themes.
func themeColorFor(lay layout.Layout, name string, inst state.Instance, agent string) func() string {
	if agent != "" && agent != "claude" {
		return nil
	}
	return func() string {
		profileDir := lay.ProfileDir(inst.Profile)
		choice, ok := "", false
		// Asking the sandbox would start a stopped one again, every
		// minute the page is open: the profile's record stands in.
		if runningNow(context.Background(), name) {
			ctx, cancel := context.WithTimeout(context.Background(), themeReadTimeout)
			choice, ok = claudetheme.FromSandbox(ctx, name)
			cancel()
		}
		if !ok {
			if choice, ok = claudetheme.FromProfile(profileDir); !ok {
				return ""
			}
		}
		// The kit's themes, or one the profile adds.
		for _, dir := range []string{lay.DefaultsDir(), profileDir} {
			if color, ok := claudetheme.Accent(dir, choice); ok {
				return color
			}
		}
		return ""
	}
}

// conversationFor is the companion page's way of talking to the sandbox's
// agent, or nil when it has none: only claude can be driven this way so far.
func conversationFor(vibeHome, name string, inst state.Instance) companion.Converser {
	if agent := inst.Agent; agent != "" && agent != "claude" {
		return nil
	}
	return keepingTranscript(converse(name, inst.Target), sandboxTranscripts(name), state.TranscriptPath(vibeHome, name))
}

// startCompanion serves the companion page for the sandbox, as the user's
// settings say, and returns what stops it. It never stops the session
// starting: whatever goes wrong is reported, and the session goes on
// without the page.
//
// cancel ends what the page has under way (see companion.Server.Cancel), for
// 'vibe --cancel'; with no page served by this vibe, it ends nothing.
func startCompanion(lay layout.Layout, args cliargs.Args, name string) (stop func(), serving bool, cancel func() []string) {
	stop = func() {}
	cancel = func() []string { return nil }
	vibeHome := lay.Home
	inst, found, err := state.Load(vibeHome, name)
	if err != nil {
		note("companion: %v", err)
		return stop, false, cancel
	}
	setting := ""
	agent := inst.Agent
	if found && lay.ProfileExists(inst.Profile) {
		if merged, err := loadSettings(lay, inst.Profile); err == nil {
			setting = merged.Companion
			if agent == "" {
				agent = agentOf(merged)
			}
		}
	}
	mode, err := companionMode(args.NoCompanion, setting)
	if err != nil {
		note("%v — not serving the companion page", err)
		return stop, false, cancel
	}
	if mode == companionOff {
		return stop, false, cancel
	}

	token, err := state.CompanionToken(vibeHome, name)
	if err != nil {
		note("companion: %v", err)
		return stop, false, cancel
	}
	instances, err := state.List(vibeHome)
	if err != nil {
		note("companion: %v", err)
		return stop, false, cancel
	}
	// The Profiles page served on its own keeps its port, even while it
	// isn't being served.
	managePort := state.ManagePort(vibeHome)
	port, served, err := chooseCompanionPort(instances, name,
		func(p int) bool { return p == managePort || portalloc.InUse(p) },
		func(p int) bool { return companion.Running(p, name) })
	if err != nil {
		note("companion: %v", err)
		return stop, false, cancel
	}
	if found && inst.CompanionPort != port {
		inst.CompanionPort = port
		if err := state.Save(vibeHome, inst); err != nil {
			note("companion: could not remember its port: %v", err)
		}
	}
	url := fmt.Sprintf("http://localhost:%d/#%s", port, token)
	if served {
		note("companion page: %s (already being served by another vibe for this sandbox)", url)
		return stop, false, cancel
	}

	srv, err := companion.Start(companion.Config{
		Sandbox:                name,
		Agent:                  agent,
		Token:                  token,
		Port:                   port,
		Policy:                 &policy.Client{Sandbox: name},
		ClipboardURL:           func() string { return clipboardPageURL(vibeHome, name) },
		StartClipboard:         func() ([]string, error) { return startClipboard(name) },
		ClipboardTrouble:       func() string { return clipboardTrouble(name) },
		Workspace:              inst.Target,
		Converse:               conversationFor(vibeHome, name, inst),
		Prepare:                readyForTurn(name),
		SandboxGit:             sandboxGitFor(name, inst.Target),
		PreferredDefaultBranch: preferredDefaultBranch(lay.HomePath("settings.yaml")),
		RememberDefaultBranch:  rememberDefaultBranch(lay.HomePath("settings.yaml")),
		Wake:                   readyForTurn(name),
		History:                state.ConversationPath(vibeHome, name),
		ReviewTab:              reviewTabFor(vibeHome, name, inst),
		Admin:                  adminFor(lay, name, inst.Target),
		Manage:                 &companionManager{lay: lay, own: name},
		ThemeColor:             themeColorFor(lay, name, inst, agent),
		KeepAwake:              keepAwakeFor(name),
		Log:                    func(line string) { note("%s", line) },
	})
	if err != nil {
		note("companion: could not serve the page on port %d: %v", port, err)
		return stop, false, cancel
	}
	note("companion page: %s", srv.URL())
	if mode == companionOpen && !browser.Open(srv.URL()) {
		note("companion: no browser to open it in from here — open the URL above yourself")
	}
	return func() { srv.Close() }, true, srv.Cancel
}

// reviewTabFor is what the page's Review tab works on: the sandbox's folder,
// which is the same path on the host, and its reviews, kept on the host
// (see docs/plan-review-tab.md). Nil, and no tab, for a sandbox with no
// folder on record.
func reviewTabFor(vibeHome, name string, inst state.Instance) *companion.ReviewTab {
	if inst.Target == "" {
		return nil
	}
	noteBelowRepositoryTop(inst.Target)
	return &companion.ReviewTab{Workspace: inst.Target, Store: review.NewStore(state.ReviewPath(vibeHome, name))}
}

// noteBelowRepositoryTop warns when the sandbox's folder is inside a git
// repository but not its top: the Review tab reviews the whole repository,
// while in the sandbox, which has only the folder, git can't see it, so the
// agent can't review it.
func noteBelowRepositoryTop(folder string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	top, err := review.Root(ctx, folder)
	if err != nil {
		return
	}
	realTop, errTop := filepath.EvalSymlinks(top)
	realFolder, errFolder := filepath.EvalSymlinks(folder)
	if errTop != nil || errFolder != nil || realTop == realFolder {
		return
	}
	// git names the top resolved (macOS's /var is /private/var): it's named
	// as the user reaches it, the folder's path with the levels below the
	// top taken off.
	if rel, err := filepath.Rel(realTop, realFolder); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		top = folder
		for range strings.Split(rel, string(filepath.Separator)) {
			top = filepath.Dir(top)
		}
	}
	note("%s is inside the git repository at %s, not at its top: the Review tab reviews the whole repository, "+
		"but the agent can't use git in the sandbox, which only has this folder. For the best results, start vibe in %s.", folder, top, top)
}

// stoppedOnPurpose is set while the sandbox is stopped because someone
// stopped it (the page's Stop, 'vibe --stop', sbx under a terminal session),
// rather than sbx stopping it for being idle: the page coming back to the
// screen doesn't start it again then.
var stoppedOnPurpose atomic.Bool

// keepAwakeFor is what the companion page asks for while it's in view, and
// the page's server while the agent is at work:
// sbx stops a sandbox about a minute after nothing is attached to it, and
// one stopping and starting under the page reloads its clipboard pane. So a
// running sandbox is touched, to keep it awake while the page is in use (or
// the agent is at work: see companion.Server.keepAwakeIfWorking), and
// one sbx stopped while the page was out of sight is started again (with
// its start-up), ready for when it's used; what it did, if it says. One
// stopped on purpose is left stopped, and with a terminal attached there's
// nothing to do: the session keeps it awake.
func keepAwakeFor(name string) func() string {
	return func() string {
		attachedMu.Lock()
		busy := attached
		attachedMu.Unlock()
		if busy || stoppedOnPurpose.Load() {
			return ""
		}
		ctx, cancel := context.WithTimeout(context.Background(), statusWithin)
		defer cancel()
		if runningNow(ctx, name) {
			sbxrun.ExecSilentContext(ctx, name, "true")
			return ""
		}
		if err := vmCheck(); err != nil {
			return "sandbox '" + name + "' was stopped, and can't be started again for the page: " + err.Error()
		}
		startCtx, cancelStart := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancelStart()
		if !startSandboxForPage(startCtx, name) {
			return "sandbox '" + name + "' was stopped, and couldn't be started again for the page — 'sbx ls' says what state it is in"
		}
		return "sandbox '" + name + "' was stopped: started it again"
	}
}

// turnStartWithin bounds starting a stopped sandbox for a turn: a cold
// boot, with its start-up, is well within it.
const turnStartWithin = 3 * time.Minute

// readyForTurn is what the page's server does before each turn: starts the
// sandbox when it isn't running, saying so, and waits until it answers.
// sbx exec would start it itself, but can give up while it boots, failing
// the turn with sbx's "context deadline exceeded". A sandbox stopped on
// purpose is started too: sending a prompt asks for it.
func readyForTurn(name string) func(ctx context.Context, say func(string)) error {
	return func(ctx context.Context, say func(string)) error {
		check, cancel := context.WithTimeout(ctx, statusWithin)
		running := runningNow(check, name)
		cancel()
		if running {
			return nil
		}
		if err := vmCheck(); err != nil {
			return err
		}
		say("starting the sandbox (sbx stops one that's left idle) — the agent answers once it's up")
		start, cancel := context.WithTimeout(ctx, turnStartWithin)
		defer cancel()
		if !startSandboxForPage(start, name) {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fmt.Errorf("sandbox '%s' didn't start within %s — 'sbx ls' says what state it is in", name, turnStartWithin)
		}
		stoppedOnPurpose.Store(false)
		// A session (terminal or headless) waiting for the sandbox to be back
		// carries on, running its start-up again, as after the Admin tab's Start.
		hold.release()
		return nil
	}
}

// sandboxGitFor runs git in sandbox name on its folder, target (the same
// path there as here). It never prompts: with nobody to answer, a missing
// credential fails at once rather than waiting. Nil for a sandbox with no
// folder on record.
func sandboxGitFor(name, target string) func(ctx context.Context, args ...string) (string, error) {
	if target == "" {
		return nil
	}
	return func(ctx context.Context, args ...string) (string, error) {
		full := append([]string{"env", "GIT_TERMINAL_PROMPT=0", "GIT_SSH_COMMAND=ssh -o BatchMode=yes", "git", "-C", target}, args...)
		return sbxrun.ExecStdout(ctx, name, full...)
	}
}

// startSandboxForPage starts sandbox name for the page. A variable for
// tests.
var startSandboxForPage = sbxrun.StartContext
