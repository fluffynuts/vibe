package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"vibe/internal/browser"
	"vibe/internal/claudetheme"
	"vibe/internal/cliargs"
	"vibe/internal/companion"
	"vibe/internal/kitspec"
	"vibe/internal/layout"
	"vibe/internal/nugetfetch"
	"vibe/internal/packagereq"
	"vibe/internal/pathresolve"
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

// companionNoOpenEnv, set to 1, has the page served but not opened in a
// browser, whatever the setting says: for the vibe a restart from the page
// starts, whose page is already open (and would be reloaded under what the
// user is reading, with another tab saying it is already open). It is set by
// vibe itself, not by a person, so it isn't a flag.
const companionNoOpenEnv = "VIBE_COMPANION_NO_OPEN"

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
		if os.Getenv(companionNoOpenEnv) == "1" {
			return companionServe, nil
		}
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
const startClipboardScript = kitspec.AgentHome + "/.local/bin/start-clipboard"

// startClipboard is the companion page's "start it" for a clipboard page
// that hasn't come up: it runs what on-start would have, and waits for it
// (start-clipboard itself gives the server ten seconds to listen).
func startClipboard(name string) ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	args := []string{"exec", name, "--", startClipboardScript}
	if out, err := sbxrun.ExecCapture(ctx, name, startClipboardScript); err != nil {
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

// themeDeskFor is the agent's theme for the companion page: its colour, and the picker. Only claude has themes.
func themeDeskFor(lay layout.Layout, name string, inst state.Instance, agent string) *themeDesk {
	if agent != "" && agent != "claude" {
		return nil
	}
	return &themeDesk{lay: lay, name: name, inst: inst}
}

// themeColorOf is desk's colour for the page, nil without a desk.
func themeColorOf(desk *themeDesk) func() string {
	if desk == nil {
		return nil
	}
	return desk.color
}

// themePickerOf is desk as the page's picker: a nil interface without one.
func themePickerOf(desk *themeDesk) companion.ThemePicker {
	if desk == nil {
		return nil
	}
	return desk
}

// themeColorFor reads the colour of the theme the sandbox's agent uses, for
// the companion page (see themeDesk.color).
func themeColorFor(lay layout.Layout, name string, inst state.Instance, agent string) func() string {
	return themeColorOf(themeDeskFor(lay, name, inst, agent))
}

// themeAccent is the accent colour of a theme, from the kit's themes or one the profile adds; "" for none.
func themeAccent(lay layout.Layout, profileDir, choice string) string {
	for _, dir := range []string{lay.DefaultsDir(), profileDir} {
		if color, ok := claudetheme.Accent(dir, choice); ok {
			return color
		}
	}
	return ""
}

// themeFromSandbox reads the theme the sandbox's agent has; a variable for tests.
var themeFromSandbox = claudetheme.FromSandbox

// themeCheckEvery is how often, at most, the sandbox is asked which theme its agent has: /theme can change it at any time,
// but seldom does, and asking can be slow.
const themeCheckEvery = 30 * time.Second

// themeDesk is one sandbox's agent's theme, for the companion page: the colour the page takes, and the picker that changes
// it, in the sandbox while it runs and in its profile, which the next build of it starts from. Both answer at once, from
// what was last found (or the profile's record, before anything was): asking the sandbox can be slow, so it is done in the
// background, one at a time, at most every themeCheckEvery.
type themeDesk struct {
	lay  layout.Layout
	name string
	inst state.Instance

	mu sync.Mutex
	// seen is what the last check found: the theme, and whether the sandbox was running. found says there was one.
	seen     themeSeen
	found    bool
	checking bool
	checked  time.Time
}

type themeSeen struct {
	choice string
	live   bool
}

func (d *themeDesk) known() []string { return claudetheme.Available(d.lay.DefaultsDir()) }

// now is what is known of the theme, a check of the sandbox being started if one is due; checking says none has been
// made yet.
func (d *themeDesk) now() (seen themeSeen, checking bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if !d.checking && (d.checked.IsZero() || time.Since(d.checked) >= themeCheckEvery) {
		d.checking = true
		go d.check()
	}
	if d.found {
		return d.seen, false
	}
	// Asking a stopped sandbox would start it again: until the sandbox has been asked, the profile's record stands in.
	choice, _ := claudetheme.FromProfile(d.lay.ProfileDir(d.inst.Profile))
	return themeSeen{choice: choice}, true
}

// check asks the sandbox which theme its agent has, if it is running (asking would start a stopped one), else takes the
// profile's record.
func (d *themeDesk) check() {
	running := runningNow(context.Background(), d.name)
	choice, ok := "", false
	if running {
		ctx, cancel := context.WithTimeout(context.Background(), themeReadTimeout)
		choice, ok = themeFromSandbox(ctx, d.name)
		cancel()
	}
	if !ok {
		choice, _ = claudetheme.FromProfile(d.lay.ProfileDir(d.inst.Profile))
	}
	d.mu.Lock()
	d.seen, d.found, d.checking, d.checked = themeSeen{choice: choice, live: running}, true, false, time.Now()
	d.mu.Unlock()
}

// isChecking says a check of the sandbox is under way.
func (d *themeDesk) isChecking() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.checking
}

// hasChecked says the sandbox has been checked at least once.
func (d *themeDesk) hasChecked() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.found
}

// color is the accent colour of the agent's theme, for the page: "" for none.
func (d *themeDesk) color() string {
	seen, _ := d.now()
	if seen.choice == "" {
		return ""
	}
	return themeAccent(d.lay, d.lay.ProfileDir(d.inst.Profile), seen.choice)
}

func (d *themeDesk) Themes(ctx context.Context) companion.ThemeState {
	known := d.known()
	seen, checking := d.now()
	current, _ := claudetheme.Resolve(seen.choice, known)
	options := claudetheme.Choices(known)
	// A theme Claude has built in (picked with /theme) is not the kit's to offer, but it is the one in force.
	if !slices.Contains(options, current) {
		options = append(options, current)
	}
	return companion.ThemeState{Current: current, Options: options, Live: seen.live, Checking: checking}
}

func (d *themeDesk) SetTheme(ctx context.Context, choice string) (companion.ThemeSet, error) {
	offered := false
	for _, o := range claudetheme.Choices(d.known()) {
		offered = offered || o == choice
	}
	if !offered {
		return companion.ThemeSet{}, companion.ErrNoSuchTheme
	}
	running := runningNow(ctx, d.name)
	if running {
		if err := claudetheme.SetInSandbox(ctx, d.name, choice); err != nil {
			return companion.ThemeSet{}, err
		}
	}
	profileDir := d.lay.ProfileDir(d.inst.Profile)
	if err := claudetheme.SaveToProfile(profileDir, choice); err != nil {
		return companion.ThemeSet{}, fmt.Errorf("the theme was set, but couldn't be recorded in the profile (a rebuild would lose it): %w", err)
	}
	// Known now, without asking the sandbox again.
	d.mu.Lock()
	d.seen, d.found, d.checked = themeSeen{choice: choice, live: running}, true, time.Now()
	d.mu.Unlock()
	return companion.ThemeSet{Live: running, Color: themeAccent(d.lay, profileDir, choice)}, nil
}

// hasConversation says the sandbox's agent can be driven by the page: only
// claude can so far (see docs/plan-conversation-other-agents.md).
func hasConversation(inst state.Instance) bool {
	return inst.Agent == "" || inst.Agent == "claude"
}

// conversationFor is the companion page's way of talking to the sandbox's
// agent, or nil when it has none: only claude can be driven this way so far.
func conversationFor(vibeHome, name string, inst state.Instance, choice profileChoice) companion.StartAgent {
	if !hasConversation(inst) {
		return nil
	}
	return keepingTranscript(startAgent(name, inst.Target, choice.forTurn), sandboxTranscripts(name), state.TranscriptPath(vibeHome, name))
}

// choiceFor is the page's way of choosing the model and effort of sandbox
// name's agent, or nil when it has none to choose (see conversationFor).
func choiceFor(vibeHome, name string, inst state.Instance, kept profileChoice) *companion.Choice {
	if !hasConversation(inst) {
		return nil
	}
	return sandboxChoice(name, inst.Target, kept, state.ModelOptionsPath(vibeHome, name))
}

// permissionModeFor is the Admin tab's choice of the agent's permission mode, for a sandbox whose agent the page talks with.
func permissionModeFor(inst state.Instance, kept profileChoice) *companion.PermissionModeChoice {
	if !hasConversation(inst) {
		return nil
	}
	return kept.permissionModeChoice()
}

// startCompanion serves the companion page for the sandbox, as the user's
// settings say, and returns what stops it. It never stops the session
// starting: whatever goes wrong is reported, and the session goes on
// without the page.
//
// cancel ends what the page has under way (see companion.Server.Cancel), for
// 'vibe --cancel'; with no page served by this vibe, it ends nothing.
func startCompanion(lay layout.Layout, args cliargs.Args, name string) (stop func(), serving bool, cancel func() []string, authors *authorDesk) {
	stop = func() {}
	cancel = func() []string { return nil }
	vibeHome := lay.Home
	inst, found, err := state.Load(vibeHome, name)
	if err != nil {
		note("companion: %v", err)
		return stop, false, cancel, nil
	}
	setting := ""
	agent := inst.Agent
	var desk *packagereq.Desk
	var workspaceConfig companion.PackageConfig
	var packageSync *nugetfetch.Sync
	if found && lay.ProfileExists(inst.Profile) {
		if merged, err := loadSettings(lay, inst.Profile); err == nil {
			setting = merged.Companion
			desk = packageDeskFor(lay, name, inst, merged)
			if desk != nil {
				workspaceConfig = workspaceConfigFor(lay.Home, name, inst, merged)
			}
			packageSync = packageSyncFor(lay, name, inst, merged)
			if desk != nil {
				desk.Overview = packagesOverview(desk, packageSync)
			}
			if agent == "" {
				agent = agentOf(merged)
			}
		}
	}
	mode, err := companionMode(args.NoCompanion, setting)
	if err != nil {
		note("%v — not serving the companion page", err)
		return stop, false, cancel, nil
	}
	if mode == companionOff {
		return stop, false, cancel, nil
	}

	token, err := state.CompanionToken(vibeHome, name)
	if err != nil {
		note("companion: %v", err)
		return stop, false, cancel, nil
	}
	instances, err := state.List(vibeHome)
	if err != nil {
		note("companion: %v", err)
		return stop, false, cancel, nil
	}
	// The Profiles page served on its own keeps its port, even while it
	// isn't being served.
	managePort := state.ManagePort(vibeHome)
	port, served, err := chooseCompanionPort(instances, name,
		func(p int) bool { return p == managePort || portalloc.InUse(p) },
		func(p int) bool { return companion.Running(p, name) })
	if err != nil {
		note("companion: %v", err)
		return stop, false, cancel, nil
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
		return stop, false, cancel, nil
	}

	authors = authorDeskFor(args, found, lay, name, inst)
	var gitAuthor *companion.GitAuthor
	if authors != nil {
		gitAuthor = &companion.GitAuthor{State: authors.State, Decide: authors.Decide}
	}
	// One for the page's colour and its picker: they share what was last found of the theme.
	theme := themeDeskFor(lay, name, inst, agent)
	srv, err := companion.Start(companion.Config{
		Sandbox:                name,
		Agent:                  agent,
		Token:                  token,
		Port:                   port,
		Policy:                 &policy.Client{Sandbox: name},
		RulesChanged:           func() error { return keepRules(vibeHome, name) },
		ClipboardURL:           func() string { return clipboardPageURL(vibeHome, name) },
		StartClipboard:         func() ([]string, error) { return startClipboard(name) },
		ClipboardTrouble:       func() string { return clipboardTrouble(name) },
		Workspace:              inst.Target,
		StartAgent:             conversationFor(vibeHome, name, inst, profileChoice{lay: lay, profile: inst.Profile}),
		Choice:                 choiceFor(vibeHome, name, inst, profileChoice{lay: lay, profile: inst.Profile}),
		PermissionMode:         permissionModeFor(inst, profileChoice{lay: lay, profile: inst.Profile}),
		Prepare:                readyForTurn(name),
		SandboxGit:             sandboxGitFor(name, inst.Target),
		PullRequests:           pullRequestsFor(name, inst.Target),
		BranchPullRequest:      ghBranchPullRequest,
		ExpiredGitTokens:       func() []string { return expiredForDot(vibeHome, timeNow(), sbxKnows) },
		PreferredDefaultBranch: preferredDefaultBranch(lay.HomePath("settings.yaml")),
		RememberDefaultBranch:  rememberDefaultBranch(lay.HomePath("settings.yaml")),
		Wake:                   readyForTurn(name),
		History:                state.ConversationPath(vibeHome, name),
		ReviewTab:              reviewTabFor(vibeHome, name, inst),
		Admin:                  adminFor(lay, name, inst.Target),
		GitAuthor:              gitAuthor,
		SignIn:                 signInFor(name, agent),
		SignInStart:            signInStartFor(name, agent),
		SignInAPIKey:           signInAPIKeyFor(agent),
		Packages:               packageDesk(desk),
		PackageConfig:          workspaceConfig,
		PackageSync:            packageSyncOf(packageSync),
		PackageSyncAuto:        packageSyncAuto(lay, inst.Profile),
		Usage:                  usageFor(vibeHome, name),
		Manage:                 &companionManager{lay: lay, own: name},
		Power:                  &powerDesk{lay: lay, own: name},
		ThemeColor:             themeColorOf(theme),
		Themes:                 themePickerOf(theme),
		KeepAwake:              keepAwakeFor(name),
		SbxSignedOut:           sbxrun.NotSignedInSeen,
		Log:                    func(line string) { note("%s", line) },
	})
	if err != nil {
		note("companion: could not serve the page on port %d: %v", port, err)
		return stop, false, cancel, nil
	}
	note("companion page: %s", srv.URL())
	if mode == companionOpen && !browser.Open(srv.URL()) {
		note("companion: no browser to open it in from here — open the URL above yourself")
	}
	return func() { srv.Close() }, true, srv.Cancel, authors
}

// reviewTabFor is what the page's Review tab works on: the sandbox's folder,
// which is the same path on the host, and its reviews, kept on the host
// (see docs/adr/2026-10-15-review-tab.md). Nil, and no tab, for a sandbox with no
// folder on record.
func reviewTabFor(vibeHome, name string, inst state.Instance) *companion.ReviewTab {
	if inst.Target == "" {
		return nil
	}
	noteBelowRepositoryTop(inst.Target)
	store := review.NewStore(state.ReviewPath(vibeHome, name))
	store.AgentFile = agentMayComment(inst.Target)
	return &companion.ReviewTab{Workspace: inst.Target, Store: store}
}

// agentMayComment says why the agent may not comment on a file: when it's
// outside folder. The repository the folder is in can be one above it (the
// folder's own .git gone, say), whose other files are the host's, and none
// of the agent's to ask about.
func agentMayComment(folder string) func(file string) error {
	return func(file string) error {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		in, err := review.InFolder(ctx, folder, file)
		switch {
		case err != nil:
			return fmt.Errorf("couldn't tell whether %s is in the sandbox's folder: %w", file, err)
		case !in:
			return fmt.Errorf("%s isn't in the sandbox's folder: only files there can be commented on", file)
		}
		return nil
	}
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
	if pathresolve.Within(realTop, realFolder) {
		rel, _ := filepath.Rel(realTop, realFolder)
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
			// sbx lists it as running, which it only does signed in.
			sbxrun.NoteSignedIn()
			sbxrun.ExecSilent(ctx, name, "true")
			return ""
		}
		if err := vmCheck(); err != nil {
			return "sandbox '" + name + "' was stopped, and can't be started again for the page: " + err.Error()
		}
		startCtx, cancelStart := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancelStart()
		started, err := startOncePerPage(startCtx, name)
		if !started {
			// Another wake of the page's was starting it: that one says how it went.
			return ""
		}
		if err != nil {
			return "sandbox '" + name + "' was stopped, and couldn't be started again for the page: " + startFailure(err)
		}
		// As when a turn starts it (readyForTurn) or the Admin tab's Start does: a session held for the sandbox to be
		// back runs its start-up again, which rewrites what vibe tells the sandbox (the published URLs, the agent's
		// instructions, the timezone) for this boot.
		hold.release()
		return "sandbox '" + name + "' was stopped: started it again"
	}
}

// pageStart is the page's start of the sandbox under way, if any.
var pageStart struct {
	mu  sync.Mutex
	now *sandboxStart
}

// sandboxStart is one start: done is closed when it has ended, err then its outcome.
type sandboxStart struct {
	done chan struct{}
	err  error
}

// startOncePerPage starts sandbox name for the page, one start at a time: asked again while one is under way (New
// conversation's Start pressed while its options wake the sandbox, a turn while keeping it awake starts it), it waits for that
// one's outcome rather than running sbx's start twice at once. started says this call ran it.
func startOncePerPage(ctx context.Context, name string) (started bool, err error) {
	pageStart.mu.Lock()
	if under := pageStart.now; under != nil {
		pageStart.mu.Unlock()
		joinedStart()
		select {
		case <-under.done:
			return false, under.err
		case <-ctx.Done():
			return false, ctx.Err()
		}
	}
	mine := &sandboxStart{done: make(chan struct{})}
	pageStart.now = mine
	pageStart.mu.Unlock()
	mine.err = startSandboxForPage(ctx, name)
	pageStart.mu.Lock()
	pageStart.now = nil
	pageStart.mu.Unlock()
	close(mine.done)
	return true, mine.err
}

// joinedStart is told when a start joins one under way (tests wait on it).
var joinedStart = func() {}

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
		if _, err := startOncePerPage(start, name); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			var signedOut *sbxrun.NotSignedInError
			if errors.As(err, &signedOut) {
				return fmt.Errorf("sbx isn't signed in to Docker, so sandbox '%s' can't start. Sign in from the Admin tab%s, then send your prompt again%s", name, signedOut.Hint(), signedOut.LastResortNote())
			}
			return fmt.Errorf("sandbox '%s' didn't start: %s", name, startFailure(err))
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
		inside, err := sbxrun.FolderInSandbox(ctx, name, target)
		if err != nil {
			return "", err
		}
		full := append([]string{"env", "GIT_TERMINAL_PROMPT=0", "GIT_SSH_COMMAND=ssh -o BatchMode=yes", "git", "-C", inside}, args...)
		return sbxrun.ExecStdout(ctx, name, full...)
	}
}

// pullRequestsFor lists a repository's pull requests with sandbox name's git (its access: a public repository needs none), never
// starting it to ask; nil for a sandbox with no folder on record.
func pullRequestsFor(name, target string) func(ctx context.Context, repo string) ([]int, error) {
	run := sandboxGitFor(name, target)
	if run == nil {
		return nil
	}
	return companion.PullRequestsFrom(func(ctx context.Context) bool { return runningNow(ctx, name) }, run)
}

// startFailure is why a sandbox couldn't be started, for the page: sbx being signed out is said as that, and where to put
// it right (the sign-in flow's own address and code are the Admin tab's to show), and anything else in sbx's own words.
func startFailure(err error) string {
	var signedOut *sbxrun.NotSignedInError
	if errors.As(err, &signedOut) {
		return "sbx is signed out of Docker. Sign in from the Admin tab" + signedOut.Hint() + " (or run: sbx login)" + signedOut.LastResortNote()
	}
	return err.Error() + " (what sbx printed is in " + wakeLogFile() + ")"
}

// startSandboxForPage starts sandbox name for the page. A variable for
// tests.
var startSandboxForPage = sbxrun.StartChecked

// packageSyncOf is sync for the page's Config, which takes none as nil: a nil
// *Sync in an interface isn't.
func packageSyncOf(sync *nugetfetch.Sync) companion.PackageSync {
	if sync == nil {
		return nil
	}
	return sync
}

// packageDesk is desk for the page's Config, which takes no desk as nil: a
// nil *Desk in an interface isn't.
func packageDesk(desk *packagereq.Desk) companion.PackageDesk {
	if desk == nil {
		return nil
	}
	return desk
}
