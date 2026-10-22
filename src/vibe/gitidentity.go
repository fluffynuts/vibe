package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"vibe/internal/cliargs"
	"vibe/internal/fscopy"
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
	// host is the author the user's own global git config has.
	host func() (name, email string)
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
	hostName, hostEmail := d.host()
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

// hostGitAuthor is the author the user's own global git config has.
func hostGitAuthor() (name, email string) {
	get := func(key string) string {
		out, err := exec.Command("git", "config", "--global", key).Output()
		if err != nil {
			return ""
		}
		return strings.TrimSpace(string(out))
	}
	return get("user.name"), get("user.email")
}

// sandboxGitAuthor is the author the agent's global git config has.
func sandboxGitAuthor(ctx context.Context, name string) (author, email string, err error) {
	out, err := sbxrun.ExecStdout(ctx, name, asAgent(`printf '%s\n%s\n' "$(git config --global user.name)" "$(git config --global user.email)"`)...)
	if err != nil {
		return "", "", err
	}
	lines := strings.Split(out, "\n")
	if len(lines) < 2 {
		return "", "", fmt.Errorf("an answer that wasn't two lines: %.80q", out)
	}
	return strings.TrimSpace(lines[0]), strings.TrimSpace(lines[1]), nil
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
	if !lay.ProfileExists(profile) {
		return fmt.Errorf("profile '%s' isn't there to keep the choice in", profile)
	}
	return fscopy.WriteWhole(filepath.Join(lay.ProfileDir(profile), profilegen.GitAuthorFile), []byte(choice+"\n"))
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

// gitAuthorDeps are the real world's answers for sandbox name, whose agent
// works in ctx's reach.
func gitAuthorDeps(ctx context.Context, name string, say func(string, ...interface{}), ask func(string) bool) identityDeps {
	return identityDeps{
		sandbox: func() (string, string, error) { return sandboxGitAuthor(ctx, name) },
		host:    hostGitAuthor,
		set:     func(n, e string) error { return giveSandboxGitAuthor(ctx, name, n, e) },
		ask:     ask,
		say:     say,
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
	}
}

// checkSandboxGitAuthor is run at a terminal session's start, before the
// agent is attached (see terminalAuthor.run). A headless session asks on the
// page instead (see authorDesk).
func checkSandboxGitAuthor(lay layout.Layout, args cliargs.Args, name string) {
	inst, found, err := state.Load(lay.Home, name)
	if err != nil || !found {
		return
	}
	ctx := context.Background()
	terminalAuthor{
		preference: func() (string, error) { return gitAuthorPreference(lay, inst.Profile) },
		start:      func() bool { return sbxrun.Start(ctx, name) },
		deps: func(say func(string, ...interface{}), ask func(string) bool) identityDeps {
			return gitAuthorDeps(ctx, name, say, ask)
		},
		push:    func() string { return pushStatement(inst) },
		keep:    func(choice string) error { return keepGitAuthorChoice(lay, inst.Profile, choice) },
		say:     func(format string, a ...interface{}) { note(format, a...) },
		force:   args.Force,
		canAsk:  interactive,
		confirm: func(question string) bool { return confirmDefault(args.Force, true, question) },
	}.run()
}
