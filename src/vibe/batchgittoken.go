package main

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"vibe/internal/cliargs"
	"vibe/internal/gittoken"
	"vibe/internal/sbxrun"
	"vibe/internal/state"
)

// listSandboxes is what sbx knows of; tests stand in for it.
var listSandboxes = sbxrun.List

// batchRepoOf is repoOf for the batch; tests stand in for it.
var batchRepoOf = repoOf

// batchToken is the one token a batch gives, and where it came from.
type batchToken struct {
	token, source string
	expires       time.Time
}

// batchOutcome is what happened to one sandbox of a batch.
type batchOutcome struct {
	name, folder           string
	given                  bool
	problem                string
	pushCheck              string
	oldExpires, newExpires time.Time
}

// batchCandidates are the sandboxes a batch can offer the token to: those
// vibe has a record of and sbx still knows, by name. A record sbx has lost is
// no sandbox to give anything to.
func batchCandidates(vibeHome string) ([]state.Instance, error) {
	statuses, err := listSandboxes(context.Background())
	if err != nil {
		return nil, err
	}
	known := map[string]bool{}
	for _, s := range statuses {
		known[s.Name] = true
	}
	records, err := state.List(vibeHome)
	if err != nil {
		return nil, err
	}
	var out []state.Instance
	for _, r := range records {
		if known[r.Name] {
			out = append(out, r)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// batchLabel is a sandbox's line in the checklist, with what its token is
// now, from vibe's own records: asking GitHub for each would be slow, and the
// answer isn't what the list is for.
func batchLabel(inst state.Instance, now time.Time) string {
	token := "no token"
	if inst.GitToken != "" {
		token = inst.GitToken + inParens(gittoken.DescribeExpiry(inst.GitTokenExpires, now))
	}
	where := inst.Target
	if inst.Profile != "" {
		where += " [" + inst.Profile + "]"
	}
	return fmt.Sprintf("%s — %s — %s", inst.Name, where, token)
}

// giveTokenToSandboxes gives tok to each of picked in turn, keeping the rules
// each has (or presetRules, when they were given). The token is checked
// against each sandbox's own repository, once per repository: one it can't
// push to is skipped, and the rest go on; one that fails stops none of the
// others.
func giveTokenToSandboxes(vibeHome string, picked []state.Instance, tok batchToken, presetRules []string) []batchOutcome {
	checks := map[string]gittoken.Report{}
	var outcomes []batchOutcome
	for _, inst := range picked {
		out := batchOutcome{name: inst.Name, folder: inst.Target, oldExpires: inst.GitTokenExpires}
		repo := batchRepoOf(inst.Target)
		expires := tok.expires
		switch {
		case repo == nil:
			out.pushCheck = "no GitHub remote, so not checked"
		default:
			rep, seen := checks[repo.String()]
			if !seen {
				rep = checkGitToken(tok.token, repo)
				checks[repo.String()] = rep
			}
			if problems := rep.Problems(); rep.Unreachable == nil && len(problems) > 0 {
				out.problem = problems[0]
				outcomes = append(outcomes, out)
				continue
			}
			if rep.Unreachable != nil {
				out.pushCheck = "GitHub couldn't be asked, so not checked"
			} else {
				out.pushCheck = "GitHub says it can push to " + repo.String()
			}
			if !rep.Expires.IsZero() {
				expires = rep.Expires
			}
		}
		rules := presetRules
		if len(rules) == 0 {
			rules = inst.GitRules
		}
		access := &gitAccess{token: tok.token, source: tok.source, rules: rules, expires: expires}
		note("'%s':", inst.Name)
		if err := setGitAccess(vibeHome, &inst, access, repo); err != nil {
			out.problem = err.Error()
			out.pushCheck = ""
		} else {
			out.given, out.newExpires = true, inst.GitTokenExpires
		}
		outcomes = append(outcomes, out)
	}
	return outcomes
}

// batchSummary says which sandboxes got the token and which didn't, and why,
// with the command that tries again for each that didn't. It never has the
// token in it.
func batchSummary(outcomes []batchOutcome, now time.Time) []string {
	var lines []string
	for _, o := range outcomes {
		if o.given {
			lines = append(lines, fmt.Sprintf("  ✔ %s: %s → %s%s", o.name,
				orNone(gittoken.DescribeExpiry(o.oldExpires, now)), orNone(gittoken.DescribeExpiry(o.newExpires, now)),
				prefixed(" — ", o.pushCheck)))
			continue
		}
		lines = append(lines,
			fmt.Sprintf("  ✘ %s: %s", o.name, o.problem),
			fmt.Sprintf("      'vibe --git-token' in %s tries again", o.folder))
	}
	return lines
}

func orNone(s string) string {
	if s == "" {
		return "no expiry"
	}
	return s
}

// batchRepositories are the distinct repositories of the sandboxes picked,
// sorted, and their owner when they share one.
func batchRepositories(picked []state.Instance) (repos []gittoken.Repo, names []string, owner string) {
	seen := map[string]bool{}
	for _, inst := range picked {
		if repo := batchRepoOf(inst.Target); repo != nil && !seen[repo.String()] {
			seen[repo.String()] = true
			repos = append(repos, *repo)
		}
	}
	sort.Slice(repos, func(i, j int) bool { return repos[i].String() < repos[j].String() })
	for i, r := range repos {
		names = append(names, r.String())
		switch {
		case i == 0:
			owner = r.Owner
		case owner != r.Owner:
			owner = ""
		}
	}
	return repos, names, owner
}

// askBatchToken asks which token to give the sandboxes picked: the default,
// a new one made on GitHub (with all their repositories to choose there),
// one the user has, or the one another sandbox has. It is checked, with
// the user asked what to do about a problem, against the first sandbox's
// repository; giveTokenToSandboxes checks it against the others. Quitting any
// question changes nothing.
func askBatchToken(vibeHome string, picked []state.Instance) (batchToken, bool) {
	now := timeNow()
	repos, names, owner := batchRepositories(picked)
	var first *gittoken.Repo
	if len(repos) > 0 {
		first = &repos[0]
	}
	skip := fixedSkip("give up: change nothing")

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
	donors := tokenDonors(vibeHome, "")

	const (
		useDefault = iota
		fromDonor
		guided
		paste
	)
	var labels []string
	var choices []int
	add := func(c int, label string) {
		choices = append(choices, c)
		labels = append(labels, label)
	}
	if haveDefault {
		add(useDefault, "your default token (from vibe --setup)"+inParens(gittoken.DescribeExpiry(defaultExpires, now)))
	}
	add(guided, "a new token for all of them (opens GitHub to make one)")
	add(paste, "a token you already have")
	if len(donors) > 0 {
		add(fromDonor, "the token another sandbox has")
	}
	idx, ok := chooseFrom(fmt.Sprintf("which token should the %d sandboxes get?", len(picked)), labels, 0)
	if !ok {
		return batchToken{}, false
	}

	var tok batchToken
	switch choices[idx] {
	case useDefault:
		tok = batchToken{token: defaultToken, source: gitTokenDefault, expires: defaultExpires}
		verdict, expires := vetGitToken(tok.token, first, true, skip)
		for verdict == vetRetry {
			verdict, expires = vetGitToken(tok.token, first, true, skip)
		}
		if verdict != vetUse {
			return batchToken{}, false
		}
		if !expires.IsZero() {
			tok.expires = expires
		}
	case fromDonor:
		donor, chosen := pickTokenDonor(donors)
		if !chosen {
			return batchToken{}, false
		}
		access, taken := accessFromDonor(donor, first, true, skip)
		if !taken {
			return batchToken{}, false
		}
		tok = batchToken{token: access.token, source: access.source, expires: access.expires}
	default:
		if choices[idx] == guided {
			description := "Lets the agents in the vibe sandboxes " + sandboxNames(picked) + " push."
			openTokenPageFor("vibe sandboxes", description, owner, pickTokenDays(tokenDays), names)
		}
		token, expires, entered := enterGitToken(first, skip)
		if !entered {
			return batchToken{}, false
		}
		tok = batchToken{token: token, source: gitTokenOwn, expires: expires}
	}

	if tok.source == gitTokenOwn && confirmDefault(false, false,
		"keep it as your default token too, so a renewal through 'vibe --setup' reaches these sandboxes together?") {
		if err := gittoken.Save(defaultPath, tok.token); err != nil {
			note("WARNING: %s — they hold it as their own", err)
		} else {
			if err := gittoken.SaveExpiry(defaultPath, tok.expires); err != nil {
				note("WARNING: %s", err)
			}
			tok.source = gitTokenDefault
			note("  sandboxes already using the default but not picked here keep the token they have")
		}
	}
	return tok, true
}

func sandboxNames(picked []state.Instance) string {
	names := make([]string, len(picked))
	for i, inst := range picked {
		names[i] = inst.Name
	}
	return strings.Join(names, ", ")
}

// doBatchGitToken is --batch-git-token: tick the sandboxes to give one token
// to, say which, and give it to them. Terminal only: a token is never typed
// into the page, and a script can't tick.
func doBatchGitToken(vibeHome string, args cliargs.Args) error {
	if args.Force || !interactive() {
		return fmt.Errorf("--batch-git-token asks which sandboxes to give the token to, so it needs a terminal")
	}
	presetRules, err := gittoken.ValidRules(strings.Split(args.GitRules, ","))
	if err != nil {
		return err
	}
	candidates, err := batchCandidates(vibeHome)
	if err != nil {
		return err
	}
	if len(candidates) == 0 {
		note("no sandboxes to give a token to")
		return nil
	}
	now := timeNow()
	labels := make([]string, len(candidates))
	for i, c := range candidates {
		labels[i] = batchLabel(c, now)
	}
	ticked, ok := multiFilterFrom("which sandboxes should get the token? (none are ticked to begin with)", labels)
	if !ok || len(ticked) == 0 {
		note("no sandbox picked: nothing changed")
		return nil
	}
	picked := make([]state.Instance, len(ticked))
	for i, idx := range ticked {
		picked[i] = candidates[idx]
	}
	tok, ok := askBatchToken(vibeHome, picked)
	if !ok {
		note("no token chosen: nothing changed")
		return nil
	}
	outcomes := giveTokenToSandboxes(vibeHome, picked, tok, presetRules)
	note("")
	failed := 0
	for _, o := range outcomes {
		if !o.given {
			failed++
		}
	}
	for _, line := range batchSummary(outcomes, timeNow()) {
		note("%s", line)
	}
	if failed > 0 {
		return fmt.Errorf("%d of %d sandboxes did not get the token", failed, len(outcomes))
	}
	return nil
}
