package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"vibe/internal/browser"
	"vibe/internal/cliargs"
	"vibe/internal/gittoken"
	"vibe/internal/prompt"
	"vibe/internal/sbxrun"
	"vibe/internal/state"
)

// gitAccess is a GitHub token a sandbox's agent pushes with, and the rules
// it's given for using it. A sandbox without one has a nil *gitAccess —
// which is how every sandbox starts, unless the user says otherwise.
type gitAccess struct {
	token string
	// source is "default" (the token --setup stored) or "own" (one given
	// for this sandbox); see state.Instance.GitToken.
	source string
	rules  []string
	// expires is when the token stops working; zero when it never does,
	// or GitHub didn't say.
	expires time.Time
}

const (
	gitTokenDefault = "default"
	gitTokenOwn     = "own"
)

// The lifetime first offered for a new token: a week for one sandbox's,
// long enough for a plan the agent works through over a few days without
// being a standing credential; a month for the default, which outlives any
// one plan.
const (
	tokenDays        = 7
	defaultTokenDays = 30
)

// timeNow is the clock expiry is judged by; tests stand in for it.
var timeNow = time.Now

// checkGitToken asks GitHub about a token; tests stand in for it.
var checkGitToken = func(token string, repo *gittoken.Repo) gittoken.Report {
	return gittoken.Checker{}.Check(token, repo)
}

// setGitHubSecret, removeGitHubSecret, writeGitInstructions and
// removeGitInstructions reach into sbx and the sandbox; tests stand in for
// them.
var (
	setGitHubSecret    = sbxrun.SetGitHubSecret
	removeGitHubSecret = sbxrun.RemoveGitHubSecret
	// writeGitInstructions writes the agent's rules for pushing into a
	// running sandbox. A stopped one is left alone: every session start
	// writes them afresh (see syncGitInstructions), so it gets them then.
	writeGitInstructions = func(name, content string) error {
		if !sbxrun.Reachable(name) {
			return nil
		}
		return sbxrun.ExecInput(name, content, "sh", "-c",
			"mkdir -p \"$(dirname "+gittoken.SandboxFile+")\" && cat > "+gittoken.SandboxFile)
	}
	removeGitInstructions = func(name string) error {
		if !sbxrun.Reachable(name) {
			return nil
		}
		if out, err := sbxrun.ExecCapture(name, "rm", "-f", gittoken.SandboxFile); err != nil {
			return fmt.Errorf("%w: %s", err, strings.TrimSpace(out))
		}
		return nil
	}
)

// repoOf is the GitHub repository target pushes to, or nil when it has no
// GitHub remote yet.
func repoOf(target string) *gittoken.Repo {
	if repo, ok := gittoken.RemoteOf(target); ok {
		return &repo
	}
	return nil
}

// pickGitAccess settles whether a new sandbox's agent gets a token to push
// with. It never insists: no is the default answer, and -f or no terminal
// means no, unless --git-token-env gave one.
func pickGitAccess(vibeHome string, args cliargs.Args, name, target string) (*gitAccess, error) {
	presetRules, err := gittoken.ValidRules(strings.Split(args.GitRules, ","))
	if err != nil {
		return nil, err
	}
	if args.NoGitToken {
		return nil, nil
	}
	repo := repoOf(target)
	if args.GitTokenEnv != "" {
		return gitAccessFromEnv(args.GitTokenEnv, repo, presetRules)
	}
	if args.Force || !interactive() {
		return nil, nil
	}
	access, _, err := askGitAccess(vibeHome, name, repo, nil, presetRules)
	return access, err
}

// gitAccessFromEnv is --git-token-env: the token in the named variable,
// checked with GitHub, with the rules --git-rules gave. There's no one to
// ask whether to use a token GitHub turns down, so that's an error.
func gitAccessFromEnv(variable string, repo *gittoken.Repo, rules []string) (*gitAccess, error) {
	token := strings.TrimSpace(os.Getenv(variable))
	if err := gittoken.Valid(token); err != nil {
		return nil, fmt.Errorf("--git-token-env %s: %w", variable, err)
	}
	verdict, expires := vetGitToken(token, repo, false, "")
	if verdict != vetUse {
		return nil, fmt.Errorf("--git-token-env %s: not using that token (see above) — fix it, or leave --git-token-env off", variable)
	}
	return &gitAccess{token: token, source: gitTokenOwn, rules: rules, expires: expires}, nil
}

// gitChoice is what the user picked for a sandbox's token.
type gitChoice int

const (
	gitNone gitChoice = iota
	gitKeep
	gitRenew
	gitDefault
	gitGuided
	gitPaste
)

// askGitAccess asks whether the agent in sandbox name may push, and with
// which token: none, the default --setup stored, one made just for repo
// (vibe opens GitHub's form for it), or one the user already has. current is
// what the sandbox has now, for --git-token: then renewing it is offered
// too, and is the default when it has expired or is about to; otherwise
// keeping it is, and none takes it away. Quitting a question answers it the
// way that changes nothing.
//
// A renewal is left to the caller (see renewGitAccess): askGitAccess only
// says it was chosen.
func askGitAccess(vibeHome, name string, repo *gittoken.Repo, current *state.Instance, presetRules []string) (*gitAccess, gitChoice, error) {
	now := timeNow()
	defaultPath := state.DefaultGitTokenPath(vibeHome)
	defaultToken, haveDefault, err := gittoken.Load(defaultPath)
	if err != nil {
		note("WARNING: %s", err)
	}
	defaultExpires := gittoken.LoadExpiry(defaultPath)
	if haveDefault && gittoken.Expired(defaultExpires, now) {
		note("your default GitHub token %s — 'vibe --setup' renews it", gittoken.DescribeExpiry(defaultExpires, now))
		haveDefault = false
	}
	has := current != nil && current.GitToken != ""

	var labels []string
	var choices []gitChoice
	add := func(c gitChoice, label string) {
		choices = append(choices, c)
		labels = append(labels, label)
	}
	def := 0
	if has {
		add(gitRenew, "renew it: regenerate it on GitHub, keeping its repositories and permissions")
		add(gitKeep, "keep the token it has"+inParens(gittoken.DescribeExpiry(current.GitTokenExpires, now)))
		add(gitNone, "take its token away: you commit and push yourself")
		if !gittoken.Expiring(current.GitTokenExpires, now) {
			def = 1
		}
	} else {
		add(gitNone, "no: you commit and push yourself")
	}
	if haveDefault {
		add(gitDefault, "yes, with your default token (from vibe --setup)"+inParens(gittoken.DescribeExpiry(defaultExpires, now)))
	}
	if repo != nil {
		add(gitGuided, fmt.Sprintf("yes, with a new token just for %s (opens GitHub to make one)", repo))
	} else {
		add(gitGuided, "yes, with a new token (opens GitHub to make one)")
	}
	add(gitPaste, "yes, with a token you already have")

	question := fmt.Sprintf("should the agent in '%s' be able to push to GitHub? (optional)", name)
	if repo == nil {
		note("'%s' has no GitHub remote yet, so vibe can't check a token can push to it", name)
	}
	idx, ok := chooseFrom(question, labels, def)
	if !ok {
		return nil, keepOrNone(has), nil
	}

	access := &gitAccess{}
	switch choices[idx] {
	case gitNone, gitKeep, gitRenew:
		return nil, choices[idx], nil
	case gitDefault:
		access.token, access.source = defaultToken, gitTokenDefault
		verdict, expires := vetGitToken(access.token, repo, true, skipLabel(has))
		for verdict == vetRetry {
			verdict, expires = vetGitToken(access.token, repo, true, skipLabel(has))
		}
		if verdict == vetSkip {
			return nil, keepOrNone(has), nil
		}
		access.expires = expires
		if access.expires.IsZero() {
			access.expires = defaultExpires
		}
	case gitGuided, gitPaste:
		if choices[idx] == gitGuided {
			days := pickTokenDays(tokenDays)
			openTokenPage(fmt.Sprintf("vibe %s", name), tokenDescription(name, repo), repoOwner(repo), days, repo)
		}
		if access.token, access.expires, ok = enterGitToken(repo, skipLabel(has)); !ok {
			return nil, keepOrNone(has), nil
		}
		access.source = gitTokenOwn
	}
	rules := presetRules
	if has && len(rules) == 0 {
		rules = current.GitRules
	}
	access.rules = pickGitRules(rules)
	return access, choices[idx], nil
}

func inParens(s string) string {
	if s == "" {
		return ""
	}
	return " (" + s + ")"
}

// skipLabel is how the choice to give up on a token with a problem is put:
// it leaves the token the sandbox already had, or none.
func skipLabel(has bool) string {
	if has {
		return "keep the token it has"
	}
	return "continue without a GitHub token"
}

// keepOrNone is what giving up on a new token leaves: the token the sandbox
// already had, or none.
func keepOrNone(has bool) gitChoice {
	if has {
		return gitKeep
	}
	return gitNone
}

func repoOwner(repo *gittoken.Repo) string {
	if repo == nil {
		return ""
	}
	return repo.Owner
}

func tokenDescription(name string, repo *gittoken.Repo) string {
	if repo == nil {
		return fmt.Sprintf("Lets the agent in the vibe sandbox '%s' push.", name)
	}
	return fmt.Sprintf("Lets the agent in the vibe sandbox '%s' push to %s.", name, repo)
}

// pickTokenDays asks how long a new token should last, from
// gittoken.Lifetimes, starting on def. Quitting takes def.
func pickTokenDays(def int) int {
	labels := make([]string, len(gittoken.Lifetimes))
	start := 0
	for i, days := range gittoken.Lifetimes {
		labels[i] = strconv.Itoa(days) + " days"
		if days == def {
			start = i
		}
	}
	idx, ok := chooseFrom("how long should the token last? (vibe reminds you as it nears its end, and renewing takes a minute)", labels, start)
	if !ok {
		return def
	}
	return gittoken.Lifetimes[idx]
}

// openTokenPage sends the user to GitHub's form for a new fine-grained
// token, filled in as far as GitHub lets a URL fill it. What's left for them
// to do there comes first, before the page is opened and takes their eye:
// above all, picking the repository, which no URL can — the form starts on
// "All repositories", and a token left on it passes every check vibe makes
// of whether it can push.
func openTokenPage(tokenName, description, owner string, days int, repo *gittoken.Repo) {
	note("on GitHub's page for the new token:")
	note("  1. under Repository access, change %s to %s,", paint(colorRed, `"All repositories"`), paint(colorGreen, `"Only select repositories"`))
	if repo != nil {
		note("     and choose %s — GitHub won't let vibe pick it for you", paint(colorYellow, repo.String()))
	} else {
		note("     and choose %s — GitHub won't let vibe pick them for you", paint(colorYellow, "the repositories the agent may push to"))
	}
	note("  2. check the permissions: %s", paint(colorPink, "Contents, Pull requests and Workflows, read and write"))
	note("  3. %s", paint(colorCyan, "Generate token, copy it, and paste it here"))
	waitForEnter("press enter to open the page")
	u := gittoken.NewTokenURL(tokenName, description, owner, days)
	note("make the token on GitHub: %s", u)
	if !browser.Open(u) {
		note("  (no browser to open it in from here — open the URL above yourself)")
	}
}

// Colours paint picks out the steps on GitHub's token page with: the
// choice to leave in red, the one to make in green, the repository in
// yellow, the permissions in pink and the last step in cyan.
const (
	colorRed    = "31"
	colorGreen  = "32"
	colorYellow = "33"
	colorPink   = "95"
	colorCyan   = "36"
)

// paint colours s for vibe's output, which goes to stderr — when that's a
// terminal, and NO_COLOR (https://no-color.org) isn't set.
func paint(color, s string) string {
	if os.Getenv("NO_COLOR") != "" || !prompt.IsTerminal(os.Stderr) {
		return s
	}
	return "\x1b[" + color + "m" + s + "\x1b[0m"
}

// waitForEnter shows question and waits for enter, on the terminal; with no
// terminal, it goes straight on.
func waitForEnter(question string) {
	reader, closeFn, ok := ttyReader()
	if !ok {
		return
	}
	defer closeFn()
	fmt.Fprintf(os.Stderr, "vibe: %s ", question)
	reader.ReadString('\n')
}

// openRenewPage sends the user to GitHub's list of their tokens, to
// regenerate the one named tokenName: a new value with the same
// repositories and permissions, so there's nothing to pick again.
func openRenewPage(tokenName string) {
	note("renew the token on GitHub: %s", gittoken.TokensPageURL)
	if !browser.Open(gittoken.TokensPageURL) {
		note("  (no browser to open it in from here — open the URL above yourself)")
	}
	note("  click the token (if vibe sent you to make it, it's called %q),", tokenName)
	note("  then Regenerate token, pick how long it should last, and paste the new one here;")
	note("  if it's gone from the list, give up here, and 'vibe --git-token' makes a new one")
}

// enterGitToken reads a token from the terminal, showing a * for each
// character, then checks it with GitHub — asking again, when it has a
// problem, for as long as the user wants to try again. ok is false when the
// user gave up: an empty line, Ctrl-C, or skip (skipLabel says what that
// leaves). expires is when GitHub says the token stops working.
func enterGitToken(repo *gittoken.Repo, skip string) (token string, expires time.Time, ok bool) {
	for {
		tty, closeFn, opened := ttyRW()
		if !opened {
			return "", time.Time{}, false
		}
		fmt.Fprint(os.Stderr, "vibe: paste the token (just enter to give up): ")
		token, read := tty.ReadMasked()
		closeFn()
		token = strings.TrimSpace(token)
		if !read || token == "" {
			note("  no token given — leaving it")
			return "", time.Time{}, false
		}
		if err := gittoken.Valid(token); err != nil {
			note("  %s — try again", err)
			continue
		}
		if half := len(token) / 2; len(token)%2 == 0 && token[:half] == token[half:] {
			note("  that's the same token twice over (pasted twice?) — try again")
			continue
		}
		verdict, expires := vetGitToken(token, repo, true, skip)
		switch verdict {
		case vetUse:
			return token, expires, true
		case vetSkip:
			return "", time.Time{}, false
		}
	}
}

// vetVerdict is what's to be done with a token once GitHub has been asked
// about it.
type vetVerdict int

const (
	vetUse vetVerdict = iota
	vetRetry
	vetSkip
)

// vetGitToken checks a token with GitHub, says what it found, and returns
// what's to be done with it, and when it expires. A token that reaches
// every repository its account can is fine to use, but said loudly. With
// ask, the user picks what's done with a token with a problem: try again,
// use it anyway, or skip (put as skip says); without, it isn't used —
// except when GitHub couldn't be asked at all, which only warns, since that
// says nothing against the token.
func vetGitToken(token string, repo *gittoken.Repo, ask bool, skip string) (vetVerdict, time.Time) {
	kind := gittoken.KindOf(token)
	if kind.AccountWide() {
		note("  WARNING: that's a %s: it can reach every repository your account can,", kind)
		note("  not just this one — a fine-grained token limited to one repository is safer")
	} else if kind == gittoken.Unknown {
		note("  WARNING: that's not a token whose kind vibe recognises")
	}
	note("  checking the token with GitHub...")
	rep := checkGitToken(token, repo)
	if rep.Unreachable == nil && !rep.Rejected {
		if rep.Login != "" {
			note("  ✔ GitHub accepts it (%s)", rep.Login)
		} else {
			note("  ✔ GitHub accepts it")
		}
		if rep.Expires.IsZero() {
			if kind == gittoken.FineGrained {
				note("  WARNING: it never expires")
			}
		} else {
			note("  it %s", gittoken.DescribeExpiry(rep.Expires, timeNow()))
			if gittoken.Expiring(rep.Expires, timeNow()) {
				note("  WARNING: that's soon — vibe will remind you to renew it at every start")
			}
		}
		if rep.CanPush {
			note("  ✔ it can push to %s", rep.Repo)
		}
	}
	if rep.Untried > 0 {
		note("  (%s has too many public repositories to check them all: %d weren't)", rep.Repo.Owner, rep.Untried)
	}
	// A token that can push, but to more than this repository, is turned
	// down only by the user: it works, it's just broader than it should be.
	if broad := rep.TooBroad(); broad != "" {
		note("  WARNING: %s", broad)
		note("  on GitHub, edit the token and change Repository access to %s:", paint(colorGreen, `"Only select repositories"`))
		note("  that changes what it can reach without changing the token itself")
		if ask {
			idx, ok := chooseFrom("what now?", []string{
				"I've narrowed it on GitHub: check it again",
				"use it as it is",
				"try a different token",
				skip,
			}, 0)
			switch {
			case !ok || idx == 3:
				return vetSkip, rep.Expires
			case idx == 0:
				return vetGitToken(token, repo, ask, skip)
			case idx == 2:
				return vetRetry, rep.Expires
			}
		}
	}
	problems := rep.Problems()
	if len(problems) == 0 {
		return vetUse, rep.Expires
	}
	for _, p := range problems {
		note("  %s", p)
	}
	if ask {
		idx, ok := chooseFrom("what now?", []string{"try again", "use it anyway", skip}, 0)
		switch {
		case !ok || idx == 2:
			return vetSkip, rep.Expires
		case idx == 1:
			return vetUse, rep.Expires
		}
		return vetRetry, rep.Expires
	}
	if rep.Unreachable != nil {
		return vetUse, rep.Expires
	}
	return vetSkip, rep.Expires
}

// pickGitRules asks which rules the agent is given for pushing, besides
// never force-pushing, which it always is. preset are the ones ticked to
// start with; quitting keeps them.
func pickGitRules(preset []string) []string {
	ticked := map[string]bool{}
	for _, r := range preset {
		ticked[r] = true
	}
	labels := make([]string, len(gittoken.RuleOrder))
	checked := make([]bool, len(gittoken.RuleOrder))
	for i, r := range gittoken.RuleOrder {
		labels[i] = gittoken.RuleLabels[r]
		checked[i] = ticked[r]
	}
	note("the agent will be told never to force-push")
	idxs, ok := checklistNoConfirmFrom("anything else it should be told about pushing? (space ticks, enter answers)", labels, checked)
	if !ok {
		return preset
	}
	rules := make([]string, len(idxs))
	for i, idx := range idxs {
		rules[i] = gittoken.RuleOrder[idx]
	}
	return rules
}

// giveGitAccess hands sandbox name its token, as the sandbox's own sbx
// secret, writes its agent's instructions for pushing into it, and keeps
// the token on the host for a rebuild. It returns what it recorded: nil
// (and a warning) when sbx wouldn't take the token, since a sandbox without
// one is fine — just not what was asked for.
func giveGitAccess(vibeHome, name string, access *gitAccess, repo *gittoken.Repo) *gitAccess {
	if access == nil {
		return nil
	}
	if err := setGitHubSecret(name, access.token); err != nil {
		note("WARNING: could not give the sandbox its GitHub token, so the agent can't push: %s", err)
		note("  'vibe --git-token' tries again")
		return nil
	}
	if err := gittoken.Save(state.GitTokenPath(vibeHome, name), access.token); err != nil {
		note("WARNING: %s — a rebuild of '%s' will start without its token", err, name)
	}
	if err := writeGitInstructions(name, gittoken.Instructions(repo, access.rules, access.expires)); err != nil {
		note("WARNING: could not tell the agent its rules for pushing (%s): %s", gittoken.SandboxFile, err)
	}
	note("  github:    the agent can push (%s token%s)", access.source, prefixed(", ", gittoken.DescribeExpiry(access.expires, timeNow())))
	return access
}

func prefixed(prefix, s string) string {
	if s == "" {
		return ""
	}
	return prefix + s
}

// takeGitAccess takes sandbox name's token away: its sbx secret, its
// instructions for pushing, and the copy kept on the host.
func takeGitAccess(vibeHome, name string) {
	if err := removeGitHubSecret(name); err != nil {
		note("WARNING: %s", err)
	}
	if err := removeGitInstructions(name); err != nil {
		note("WARNING: could not remove %s from the sandbox: %s", gittoken.SandboxFile, err)
	}
	if err := gittoken.Remove(state.GitTokenPath(vibeHome, name)); err != nil {
		note("WARNING: %s", err)
	}
}

// syncGitInstructions writes the agent's rules for pushing into a sandbox
// that has a token — afresh at every session start, so they carry the
// token's latest expiry, and reach a sandbox that was stopped when it was
// given one — and takes them away from one that hasn't.
func syncGitInstructions(vibeHome, name string) error {
	inst, found, err := state.Load(vibeHome, name)
	if err != nil {
		return err
	}
	if !found || inst.GitToken == "" {
		return removeGitInstructions(name)
	}
	return writeGitInstructions(name, gittoken.Instructions(repoOf(inst.Target), inst.GitRules, inst.GitTokenExpires))
}

// recordedGitAccess is the token and rules sandbox name was given, for a
// rebuild to hand on to the new sandbox; nil when it had none, or its token
// has gone missing from the host. An expired one is still handed on: the
// session start that follows offers to renew it.
func recordedGitAccess(vibeHome, name string) *gitAccess {
	inst, found, err := state.Load(vibeHome, name)
	if err != nil || !found || inst.GitToken == "" {
		return nil
	}
	token, ok, err := gittoken.Load(state.GitTokenPath(vibeHome, name))
	if err != nil || !ok {
		note("WARNING: '%s' had a GitHub token, but vibe's copy of it is gone — the rebuilt sandbox won't have one ('vibe --git-token' gives it one)", name)
		return nil
	}
	return &gitAccess{token: token, source: inst.GitToken, rules: inst.GitRules, expires: inst.GitTokenExpires}
}

// forgetGitSecret takes away the sbx secret of a sandbox being deleted,
// when vibe gave it one: sbx may keep a sandbox-scoped secret after the
// sandbox has gone, and a new sandbox of the same name would inherit it.
func forgetGitSecret(vibeHome, name string) {
	inst, found, err := state.Load(vibeHome, name)
	if err != nil || !found || inst.GitToken == "" {
		return
	}
	removeGitHubSecret(name)
}

// checkGitTokenExpiry is run at every session start: it says when the
// sandbox's token is about to expire, and when it has, offers to renew it
// there and then — the agent can't push until it is. It never stops the
// session starting.
func checkGitTokenExpiry(vibeHome string, args cliargs.Args, name string) {
	inst, found, err := state.Load(vibeHome, name)
	if err != nil || !found || inst.GitToken == "" {
		return
	}
	now := timeNow()
	switch {
	case gittoken.Expired(inst.GitTokenExpires, now):
		note("WARNING: the GitHub token for '%s' %s, so the agent can't push", name, gittoken.DescribeExpiry(inst.GitTokenExpires, now))
		if args.Force || !interactive() || !confirmDefault(false, true, "renew it now?") {
			note("  'vibe --git-token' renews it")
			return
		}
		if err := renewGitAccess(vibeHome, &inst, repoOf(inst.Target)); err != nil {
			note("WARNING: %s", err)
		}
	case gittoken.Expiring(inst.GitTokenExpires, now):
		note("the GitHub token for '%s' %s — 'vibe --git-token' renews it", name, gittoken.DescribeExpiry(inst.GitTokenExpires, now))
	}
}

// renewGitAccess replaces the sandbox's token with a regenerated one,
// keeping its rules. Renewing the default token is renewing it for every
// sandbox given it, so they're offered it too.
func renewGitAccess(vibeHome string, inst *state.Instance, repo *gittoken.Repo) error {
	tokenName := "vibe " + inst.Name
	if inst.GitToken == gitTokenDefault {
		tokenName = "vibe default"
	}
	openRenewPage(tokenName)
	token, expires, ok := enterGitToken(repo, "keep the token it has")
	if !ok {
		note("'%s' keeps the token it has", inst.Name)
		return nil
	}
	access := &gitAccess{token: token, source: inst.GitToken, rules: inst.GitRules, expires: expires}
	if err := setGitAccess(vibeHome, inst, access, repo); err != nil {
		return err
	}
	if inst.GitToken == gitTokenDefault {
		shareDefaultToken(vibeHome, token, expires, inst.Name)
	}
	return nil
}

// shareDefaultToken stores token as the default, and offers it to every
// sandbox that was given the default, but sandbox except: the token they
// hold is the one it replaces, which — regenerated — no longer works. It
// says whether the default was stored.
func shareDefaultToken(vibeHome, token string, expires time.Time, except string) bool {
	path := state.DefaultGitTokenPath(vibeHome)
	if err := gittoken.Save(path, token); err != nil {
		note("WARNING: %s", err)
		return false
	}
	if err := gittoken.SaveExpiry(path, expires); err != nil {
		note("WARNING: %s", err)
	}
	instances, err := state.List(vibeHome)
	if err != nil {
		note("WARNING: %s", err)
		return true
	}
	var sharing []state.Instance
	var names []string
	for _, inst := range instances {
		if inst.GitToken == gitTokenDefault && inst.Name != except {
			sharing = append(sharing, inst)
			names = append(names, inst.Name)
		}
	}
	if len(sharing) == 0 {
		return true
	}
	if !interactive() || !confirmDefault(false, true, fmt.Sprintf("give the new default token to the sandboxes using the default too (%s)?", strings.Join(names, ", "))) {
		note("  they keep the token they have: 'vibe --git-token' in each one's folder renews it")
		return true
	}
	for i := range sharing {
		inst := &sharing[i]
		access := &gitAccess{token: token, source: gitTokenDefault, rules: inst.GitRules, expires: expires}
		note("'%s':", inst.Name)
		if err := setGitAccess(vibeHome, inst, access, repoOf(inst.Target)); err != nil {
			note("WARNING: %s", err)
		}
	}
	return true
}

// doGitToken is --git-token: give the sandbox for target a token, renew or
// change the one it has, or take it away — asking, or as --git-token-env and
// --no-git-token say.
func doGitToken(vibeHome string, args cliargs.Args, name, target string) error {
	if args.GitTokenEnv != "" && args.NoGitToken {
		return fmt.Errorf("--git-token-env and --no-git-token contradict each other")
	}
	if !sbxrun.Exists(name) {
		return fmt.Errorf("no sandbox '%s' yet — running vibe here creates one, and asks about a token as it does", name)
	}
	inst, found, err := state.Load(vibeHome, name)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("vibe has no record of sandbox '%s' — 'vibe -r' rebuilds it as vibe's own", name)
	}
	repo := repoOf(target)

	if args.NoGitToken {
		return setGitAccess(vibeHome, &inst, nil, repo)
	}
	presetRules, err := gittoken.ValidRules(strings.Split(args.GitRules, ","))
	if err != nil {
		return err
	}
	if args.GitTokenEnv != "" {
		if len(presetRules) == 0 {
			presetRules = inst.GitRules
		}
		access, err := gitAccessFromEnv(args.GitTokenEnv, repo, presetRules)
		if err != nil {
			return err
		}
		return setGitAccess(vibeHome, &inst, access, repo)
	}
	if args.Force || !interactive() {
		return fmt.Errorf("--git-token asks what to do, so it needs a terminal — or --git-token-env VAR, or --no-git-token")
	}
	if inst.GitToken != "" {
		note("'%s' has a GitHub token (%s%s); the agent is told never to force-push%s", name, inst.GitToken,
			prefixed(", ", gittoken.DescribeExpiry(inst.GitTokenExpires, timeNow())), rulesSuffix(inst.GitRules))
	}
	access, choice, err := askGitAccess(vibeHome, name, repo, &inst, presetRules)
	if err != nil {
		return err
	}
	switch choice {
	case gitKeep:
		note("'%s' keeps its token", name)
		return nil
	case gitRenew:
		return renewGitAccess(vibeHome, &inst, repo)
	case gitNone:
		if inst.GitToken == "" {
			note("'%s' stays without a token", name)
			return nil
		}
		return setGitAccess(vibeHome, &inst, nil, repo)
	}
	if access == nil {
		return nil
	}
	return setGitAccess(vibeHome, &inst, access, repo)
}

// setGitAccess gives an existing sandbox access (nil takes its token away)
// and records it.
func setGitAccess(vibeHome string, inst *state.Instance, access *gitAccess, repo *gittoken.Repo) error {
	if access == nil {
		takeGitAccess(vibeHome, inst.Name)
		inst.GitToken, inst.GitRules, inst.GitTokenExpires = "", nil, time.Time{}
		note("took '%s''s GitHub token away: you commit and push yourself", inst.Name)
		return state.Save(vibeHome, *inst)
	}
	given := giveGitAccess(vibeHome, inst.Name, access, repo)
	if given == nil {
		return fmt.Errorf("'%s' has no new GitHub token", inst.Name)
	}
	inst.GitToken, inst.GitRules, inst.GitTokenExpires = given.source, given.rules, given.expires
	return state.Save(vibeHome, *inst)
}

func rulesSuffix(rules []string) string {
	var labels []string
	for _, r := range rules {
		if l, ok := gittoken.RuleLabels[r]; ok {
			labels = append(labels, l)
		}
	}
	if len(labels) == 0 {
		return ""
	}
	return ", and to " + strings.Join(labels, ", and to ")
}

// setUpDefaultGitToken is --setup's question about a default token: one
// that new sandboxes can be given, so the user needn't make one each time.
// Storing one gives it to no sandbox — each new one still asks, and no is
// still the default answer there. A default that's renewed or replaced is
// offered to the sandboxes already using it.
func setUpDefaultGitToken(vibeHome string) {
	path := state.DefaultGitTokenPath(vibeHome)
	_, has, err := gittoken.Load(path)
	if err != nil {
		note("WARNING: %s", err)
	}
	now := timeNow()
	expires := gittoken.LoadExpiry(path)
	var labels, actions []string
	def := 0
	if has {
		if d := gittoken.DescribeExpiry(expires, now); d != "" {
			note("your default GitHub token %s", d)
		}
		labels = []string{
			"keep the default token you have",
			"renew it: regenerate it on GitHub, keeping its repositories and permissions",
			"replace it with a new one (opens GitHub to make one)",
			"replace it with one you already have",
			"remove it",
		}
		actions = []string{"keep", "renew", "guided", "paste", "remove"}
		if gittoken.Expiring(expires, now) {
			def = 1
		}
	} else {
		labels = []string{"no default token: you commit and push yourself", "yes, a new one (opens GitHub to make one)", "yes, one you already have"}
		actions = []string{"keep", "guided", "paste"}
	}
	idx, ok := chooseFrom("store a default GitHub token, which a new sandbox can give its agent to push with? (optional — each new sandbox still asks)", labels, def)
	if !ok {
		return
	}
	switch actions[idx] {
	case "keep":
		return
	case "remove":
		if err := gittoken.Remove(path); err != nil {
			note("WARNING: %s", err)
			return
		}
		note("  removed the default GitHub token; sandboxes already given it keep it")
		return
	case "renew":
		openRenewPage("vibe default")
	case "guided":
		openTokenPage("vibe default", "Lets the agents in vibe sandboxes push.", "", pickTokenDays(defaultTokenDays), nil)
	}
	token, expires, ok := enterGitToken(nil, "go on without changing the default token")
	if !ok {
		return
	}
	note("  saving the default GitHub token in %s (only you can read it)", displayPath(path))
	shareDefaultToken(vibeHome, token, expires, "")
}
