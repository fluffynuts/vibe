package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"vibe/internal/cliargs"
	"vibe/internal/fscopy"
	"vibe/internal/gittoken"
	"vibe/internal/layout"
	"vibe/internal/pathresolve"
	"vibe/internal/profilegen"
	"vibe/internal/sbxrun"
	"vibe/internal/state"
)

// identityOutcome is how checking a sandbox's git author ended.
type identityOutcome int

const (
	// identityFine: the sandbox has an author already.
	identityFine identityOutcome = iota
	// identityGiven: the user's was put into the sandbox.
	identityGiven
	// identityDeclined: the user chose to leave the sandbox without one,
	// which is kept so they aren't asked again.
	identityDeclined
	// identityUnknown: it couldn't be settled this time (the sandbox couldn't be
	// asked, the host has none to offer, or it couldn't be set): the next
	// session looks again.
	identityUnknown
)

// identityDeps is what checkAgentIdentity asks of the world.
type identityDeps struct {
	// sandbox is the author the agent's global git config has, "" for what it lacks.
	sandbox func() (name, email string, err error)
	// host is the author the user's own global git config has: an error is
	// git not answering, which is not the same as there being none.
	host func() (name, email string, err error)
	// set puts into the agent's global git config whichever of name and email isn't "".
	set func(name, email string) error
	ask func(question string) bool
	say func(format string, a ...interface{})
}

// pushStatement says whether the agent can push, which is what makes a
// commit it makes something the user can still take back: a local commit can
// be thrown away, a pushed one can't without work.
func pushStatement(inst state.Instance) string {
	if inst.GitToken == "" {
		return "It has no GitHub token, so it can't push: what it commits stays on this machine until you push it."
	}
	if gittoken.Expired(inst.GitTokenExpires, timeNow()) {
		return "Its GitHub token has expired, so it can't push: what it commits stays on this machine until you push it (and until the token is renewed, with 'vibe --git-token')."
	}
	return "It has a GitHub token, so it can push" + rulesSuffix(inst.GitRules) + ": what it commits can reach GitHub without you."
}

// aPlainLine is whether s can go in a git config as it is: one line, no
// control characters.
func aPlainLine(s string) bool {
	return s == strings.TrimSpace(s) && !strings.ContainsFunc(s, func(r rune) bool { return r < 0x20 || r == 0x7f })
}

// The choices the gitAuthor setting holds: unset means ask.
const (
	// gitAuthorImport gives a sandbox missing a git author the user's own,
	// without asking.
	gitAuthorImport = "import"
	// gitAuthorLeave leaves it without, without asking.
	gitAuthorLeave = "leave"
)

// authorKind is what looking at the agent's git author found.
type authorKind int

const (
	// authorFine: the sandbox has an author already.
	authorFine authorKind = iota
	// authorLeft: the user has said to leave the sandbox as it is.
	authorLeft
	// authorUnknown: it couldn't be settled (see Why).
	authorUnknown
	// authorOffer: the sandbox lacks an author the user's own can fill in.
	authorOffer
)

// authorFinding is what assessAuthor found.
type authorFinding struct {
	Kind authorKind
	// Why, for authorUnknown, is what stopped it, in the user's terms.
	Why string
	// GiveName and GiveEmail are what would be put in the sandbox: only what
	// it lacks. Shown is the author the sandbox would have then.
	GiveName, GiveEmail string
	Shown               string
}

const howToGiveAnAuthor = "to give it one, run in the sandbox: git config --global user.name \"Your Name\" && git config --global user.email you@example.com"

// assessAuthor looks at the agent's git author, which git needs to commit:
// a sandbox may come without (sbx makes its ~/.gitconfig, and doesn't always
// carry the user's author in). preference is the gitAuthor setting.
func assessAuthor(d identityDeps, preference string) authorFinding {
	if preference == gitAuthorLeave {
		return authorFinding{Kind: authorLeft}
	}
	name, email, err := d.sandbox()
	if err != nil {
		return authorFinding{Kind: authorUnknown, Why: fmt.Sprintf("couldn't look at the agent's git author, so can't tell whether it can commit: %v", err)}
	}
	if name != "" && email != "" {
		return authorFinding{Kind: authorFine}
	}
	hostName, hostEmail, err := d.host()
	if err != nil {
		return authorFinding{Kind: authorUnknown, Why: fmt.Sprintf("couldn't read your own global git config, so can't offer your author: %v", err)}
	}
	if !aPlainLine(hostName) {
		hostName = ""
	}
	if !aPlainLine(hostEmail) {
		hostEmail = ""
	}
	found := authorFinding{Kind: authorOffer}
	if name == "" {
		found.GiveName = hostName
	}
	if email == "" {
		found.GiveEmail = hostEmail
	}
	if (name == "" && found.GiveName == "") || (email == "" && found.GiveEmail == "") {
		return authorFinding{Kind: authorUnknown, Why: "the agent can't commit: its git author is missing, and your own global git config doesn't have one to give it; " + howToGiveAnAuthor}
	}
	found.Shown = fmt.Sprintf("%s <%s>", firstOf(found.GiveName, name), firstOf(found.GiveEmail, email))
	return found
}

// checkAgentIdentity sees, at the terminal, that the agent can commit (see
// assessAuthor): when it can't, it says so, and whether the agent can push,
// and offers to give it the user's author, which it does only if the user
// agrees or has said to (preference). It says what it did.
func checkAgentIdentity(d identityDeps, preference, push string) identityOutcome {
	found := assessAuthor(d, preference)
	switch found.Kind {
	case authorFine:
		return identityFine
	case authorLeft:
		return identityDeclined
	case authorUnknown:
		d.say("WARNING: %s", found.Why)
		return identityUnknown
	}
	if preference != gitAuthorImport {
		d.say("WARNING: the agent has no git author name and email, so it can't commit. %s", push)
		if !d.ask(fmt.Sprintf("give it your global git author, %s?", found.Shown)) {
			d.say("left as it is: the agent can't commit; %s", howToGiveAnAuthor)
			return identityDeclined
		}
	}
	if err := d.set(found.GiveName, found.GiveEmail); err != nil {
		d.say("WARNING: couldn't give the agent a git author: %v", err)
		return identityUnknown
	}
	d.say("the agent commits as %s", found.Shown)
	return identityGiven
}

func firstOf(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// gitConfigValue runs a command that prints a git config value, and gives
// it trimmed. Git exits 1 for a key that isn't set, which is "", not a
// failure; anything else (git not there, an unreadable config) is.
func gitConfigValue(program string, args ...string) (string, error) {
	out, err := exec.Command(program, args...).Output()
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) && exit.ExitCode() == 1 {
			return "", nil
		}
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// hostGitAuthorFrom is the author get reads from the user's own global git config.
func hostGitAuthorFrom(get func(key string) (string, error)) (name, email string, err error) {
	if name, err = get("user.name"); err != nil {
		return "", "", err
	}
	if email, err = get("user.email"); err != nil {
		return "", "", err
	}
	return strings.TrimSpace(name), strings.TrimSpace(email), nil
}

// hostGitAuthor is the author the user's own global git config has.
func hostGitAuthor() (name, email string, err error) {
	return hostGitAuthorFrom(func(key string) (string, error) {
		return gitConfigValue("git", "config", "--global", key)
	})
}

// sandboxGitAuthorScript prints the agent's author after markers, as its
// login shell may say other things (a .bashrc that prints) which would
// otherwise be taken for the name.
const sandboxGitAuthorScript = `printf 'vibe-git-name=%s\nvibe-git-email=%s\n' "$(git config --global user.name)" "$(git config --global user.email)"`

// parseSandboxGitAuthor reads what sandboxGitAuthorScript printed, wherever
// among other output; an answer with no markers is an error, not "no author".
func parseSandboxGitAuthor(out string) (name, email string, err error) {
	var hasName, hasEmail bool
	for _, line := range strings.Split(out, "\n") {
		if v, ok := strings.CutPrefix(line, "vibe-git-name="); ok {
			name, hasName = strings.TrimSpace(v), true
		} else if v, ok := strings.CutPrefix(line, "vibe-git-email="); ok {
			email, hasEmail = strings.TrimSpace(v), true
		}
	}
	if !hasName || !hasEmail {
		return "", "", fmt.Errorf("an answer without the author in it: %.80q", out)
	}
	return name, email, nil
}

// How long the sandbox is given for what the author check asks of it: a
// session start waits on this, so it can't be left to wait for good.
const (
	authorCheckWithin = 30 * time.Second
	authorStartWithin = 90 * time.Second
	// authorSlowAfter is how long a look takes before the user is told what is
	// being waited on.
	authorSlowAfter = 3 * time.Second
)

func authorContext(within time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), within)
}

// reportIfSlow runs f, saying so (say) if it takes longer than after.
func reportIfSlow(after time.Duration, say func(), f func()) {
	timer := time.AfterFunc(after, say)
	defer timer.Stop()
	f()
}

// sandboxGitAuthor is the author the agent's global git config has.
func sandboxGitAuthor(ctx context.Context, name string) (author, email string, err error) {
	out, err := sbxrun.ExecStdout(ctx, name, asAgent(sandboxGitAuthorScript)...)
	if err != nil {
		return "", "", err
	}
	return parseSandboxGitAuthor(out)
}

// giveSandboxGitAuthor puts whichever of name and email isn't "" into the
// agent's global git config. The values are quoted for the shell the agent's
// login runs.
func giveSandboxGitAuthor(ctx context.Context, sandbox, name, email string) error {
	var cmds []string
	if name != "" {
		cmds = append(cmds, "git config --global user.name "+shellQuote(name))
	}
	if email != "" {
		cmds = append(cmds, "git config --global user.email "+shellQuote(email))
	}
	if out, err := sbxrun.ExecCapture(ctx, sandbox, asAgent(strings.Join(cmds, " && "))...); err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(out))
	}
	return nil
}

// keepGitAuthorChoice records choice (gitAuthorImport or gitAuthorLeave) in
// the profile, so it outlives vibe and is asked once for the folder. It has a
// file of its own, as the Claude theme does, and not settings.yaml: that is
// part of what composing the profile wrote, so a line vibe added there would
// make the profile look edited by hand, and updating it would be left to the
// user. It never makes a profile: a directory for one that isn't there would
// hide the real one.
func keepGitAuthorChoice(lay layout.Layout, profile, choice string) error {
	dir := lay.HomePath("profiles", profile)
	if dir == "" {
		return fmt.Errorf("there is no vibe home to keep the choice in")
	}
	if !lay.ProfileExists(profile) {
		return fmt.Errorf("profile '%s' isn't there to keep the choice in", profile)
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return fmt.Errorf("profile '%s' is vibe's own, which can't be written to: make a profile of your own (--profile) to keep the choice in", profile)
	}
	return fscopy.WriteWhole(filepath.Join(dir, profilegen.GitAuthorFile), []byte(choice+"\n"))
}

// gitAuthorPreference is the choice that applies to profile: what the user
// wrote as gitAuthor in a settings.yaml (the profile's over the global one),
// else the answer kept in the profile; "" asks.
func gitAuthorPreference(lay layout.Layout, profile string) (string, error) {
	merged, err := loadSettings(lay, profile)
	if err != nil {
		return "", err
	}
	if merged.GitAuthor != "" {
		return merged.GitAuthor, nil
	}
	path := filepath.Join(lay.ProfileDir(profile), profilegen.GitAuthorFile)
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if answer := strings.TrimSpace(string(data)); answer == gitAuthorImport || answer == gitAuthorLeave {
		return answer, nil
	}
	return "", fmt.Errorf("%s holds %q; it must be %q or %q (delete it to be asked again)", pathresolve.Tilde(path), strings.TrimSpace(string(data)), gitAuthorImport, gitAuthorLeave)
}

// gitAuthorDeps are the real world's answers for sandbox name. Each call to
// the sandbox is bounded, and says so when it is slow.
func gitAuthorDeps(name string, say func(string, ...interface{}), ask func(string) bool) identityDeps {
	slow := func() { say("  looking at the agent's git author, which is taking a while…") }
	return identityDeps{
		sandbox: func() (n, e string, err error) {
			ctx, cancel := authorContext(authorCheckWithin)
			defer cancel()
			reportIfSlow(authorSlowAfter, slow, func() { n, e, err = sandboxGitAuthor(ctx, name) })
			return n, e, err
		},
		host: hostGitAuthor,
		set: func(n, e string) (err error) {
			ctx, cancel := authorContext(authorCheckWithin)
			defer cancel()
			reportIfSlow(authorSlowAfter, slow, func() { err = giveSandboxGitAuthor(ctx, name, n, e) })
			return err
		},
		ask: ask,
		say: say,
	}
}

// terminalAuthor is what a terminal session's check needs of the world, so
// the check can be run with the world stood in for.
type terminalAuthor struct {
	// preference is the gitAuthor setting, or the answer kept (see gitAuthorPreference).
	preference func() (string, error)
	// start starts the sandbox, which can't be looked at while it's stopped.
	start func() bool
	deps  func(say func(string, ...interface{}), ask func(string) bool) identityDeps
	push  func() string
	// keep records the user's answer (gitAuthorImport or gitAuthorLeave).
	keep func(choice string) error
	// kept says what was kept, once it has been, and how to undo it.
	kept func(choice string) string
	say  func(format string, a ...interface{})
	// force is -f, which answers yes. canAsk says whether there is a terminal
	// to ask on; confirm asks.
	force   bool
	canAsk  func() bool
	confirm func(question string) bool
}

// run settles whether the agent can commit (see checkAgentIdentity), asking
// on the terminal, and keeps the answer. It never stops the session starting.
func (t terminalAuthor) run() {
	preference, err := t.preference()
	if err != nil {
		t.say("WARNING: couldn't read your git author choice, so can't tell whether the agent can commit: %s", err)
		return
	}
	if preference == gitAuthorLeave {
		return
	}
	if !t.start() {
		t.say("couldn't start the sandbox to look at its git author, so can't tell whether the agent can commit")
		return
	}
	// A question nobody could answer isn't a refusal, and isn't kept as one.
	unanswered := false
	ask := func(question string) bool {
		if !t.force && !t.canAsk() {
			unanswered = true
			t.say("  with a terminal, vibe would ask: %s", question)
			return false
		}
		return t.confirm(question)
	}
	outcome := checkAgentIdentity(t.deps(t.say, ask), preference, t.push())
	if unanswered || preference != "" {
		return
	}
	choice := map[identityOutcome]string{identityGiven: gitAuthorImport, identityDeclined: gitAuthorLeave}[outcome]
	if choice == "" {
		return
	}
	if err := t.keep(choice); err != nil {
		t.say("WARNING: couldn't keep your answer, so you'll be asked again: %s", err)
		return
	}
	t.say("%s", t.kept(choice))
}

// keptGitAuthorNote says that choice has been kept for a profile, and how to
// undo it: said whenever an answer is kept, so one made by -f, which the user
// didn't see asked, isn't a surprise later.
//
// The wording follows where the choice lives. It says the profile's own
// git-author file today; when the user's kept choices are consolidated into one
// user-preferences.yaml (docs/plan-user-preferences.md), this, and the readme's
// account of the file, must change with it: the path, and how to ask again.
func keptGitAuthorNote(lay layout.Layout, profile, choice string) string {
	file := filepath.Join(lay.HomePath("profiles", profile), profilegen.GitAuthorFile)
	settingsFile := pathresolve.Tilde(filepath.Join(lay.HomePath("profiles", profile), "settings.yaml"))
	again := fmt.Sprintf("To be asked again, delete %s.", pathresolve.Tilde(file))
	if choice == gitAuthorImport {
		return fmt.Sprintf("kept: every sandbox of profile '%s' that has no git author is given yours, without asking. %s To leave them without one, put `gitAuthor: leave` in %s (or in ~/.vibe/settings.yaml for every profile).", profile, again, settingsFile)
	}
	return fmt.Sprintf("kept: no sandbox of profile '%s' is given a git author, and you aren't asked again. %s To give it yours without asking, put `gitAuthor: import` in %s (or in ~/.vibe/settings.yaml for every profile).", profile, again, settingsFile)
}

// checkSandboxGitAuthor is run at a terminal session's start, before the
// agent is attached (see terminalAuthor.run). A headless session asks on the
// page instead (see authorDesk).
func checkSandboxGitAuthor(lay layout.Layout, args cliargs.Args, name string) {
	inst, found, err := state.Load(lay.Home, name)
	if err != nil || !found {
		return
	}
	terminalAuthor{
		preference: func() (string, error) { return gitAuthorPreference(lay, inst.Profile) },
		start: func() (started bool) {
			ctx, cancel := authorContext(authorStartWithin)
			defer cancel()
			reportIfSlow(authorSlowAfter, func() { note("  starting '%s' to look at its git author…", name) }, func() { started = sbxrun.Start(ctx, name) })
			return started
		},
		deps: func(say func(string, ...interface{}), ask func(string) bool) identityDeps {
			return gitAuthorDeps(name, say, ask)
		},
		push:    func() string { return pushStatement(inst) },
		keep:    func(choice string) error { return keepGitAuthorChoice(lay, inst.Profile, choice) },
		kept:    func(choice string) string { return keptGitAuthorNote(lay, inst.Profile, choice) },
		say:     func(format string, a ...interface{}) { note(format, a...) },
		force:   args.Force,
		canAsk:  interactive,
		confirm: func(question string) bool { return confirmDefault(args.Force, true, question) },
	}.run()
}
