package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"vibe/internal/cliargs"
	"vibe/internal/gittoken"
	"vibe/internal/prompt"
	"vibe/internal/sbxrun"
	"vibe/internal/state"
)

// listSandboxes is what sbx knows of; tests stand in for it.
var listSandboxes = sbxrun.List

// batchRepoOf is repoOf for the batch; tests stand in for it.
var batchRepoOf = repoOf

// batchJobs is how many sandboxes a batch gives the token to at once unless
// --jobs says: each one that has to be started costs gigabytes of memory.
const batchJobs = 4

// batchToken is the one token a batch gives, and where it came from.
type batchToken struct {
	token, source string
	expires       time.Time
	// nowDefault says it was just stored as the default, so the sandboxes
	// that use the default and weren't picked are holding the one it replaced.
	nowDefault bool
}

// batchOutcome is what happened to one sandbox of a batch.
type batchOutcome struct {
	name, folder string
	given        bool
	problem      string
	pushCheck    string
	// inside is what a push checked from inside the sandbox found.
	inside                 insideCheck
	oldExpires, newExpires time.Time
}

// batchCandidates are the sandboxes a batch can offer the token to: those
// vibe has a record of and sbx still knows, by name: a record sbx has lost is
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

// giveTokenToSandboxes gives tok to each of picked, jobs at a time, keeping
// the rules each has (or presetRules, when they were given). The token is
// checked against each sandbox's own repository, once per repository: one it
// can't push to is skipped, and the rest go on; one that fails stops none of
// the others. Outcomes are in the order of picked. Each
// sandbox's lines are kept together and shown whole when it is done, so they
// don't interleave, and progress says what is under way meanwhile.
func giveTokenToSandboxes(vibeHome string, picked []state.Instance, tok batchToken, presetRules []string, jobs int, progress *batchProgress) []batchOutcome {
	jobs = max(1, min(jobs, len(picked)))
	checker := &repoChecker{token: tok.token, reports: map[string]*repoReport{}}
	outcomes := make([]batchOutcome, len(picked))
	work := make(chan int)
	var wg sync.WaitGroup
	for w := 0; w < jobs; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range work {
				var lines []string
				say := func(format string, a ...interface{}) { lines = append(lines, fmt.Sprintf(format, a...)) }
				name := picked[i].Name
				progress.started(name)
				outcomes[i] = giveTokenToSandbox(say, vibeHome, picked[i], tok, presetRules, checker)
				progress.finished(name, lines)
			}
		}()
	}
	for i := range picked {
		work <- i
	}
	close(work)
	wg.Wait()
	return outcomes
}

// batchProgress is what the user sees while a batch runs: on a terminal, a
// spinner line saying how many are done and which are under way, which each
// sandbox's lines are printed above as it finishes; elsewhere, just those
// lines. A nil *batchProgress shows nothing.
type batchProgress struct {
	mu     sync.Mutex
	total  int
	done   int
	active []string
	frame  int
	live   bool
	out    io.Writer
	width  func() int
	stop   chan struct{}
	ended  chan struct{}
	// spinning says begin started the goroutine that end has to wait for.
	spinning bool
}

func newBatchProgress(total int, out io.Writer, live bool, width func() int) *batchProgress {
	return &batchProgress{total: total, out: out, live: live, width: width, stop: make(chan struct{}), ended: make(chan struct{})}
}

// begin starts the spinner turning.
func (p *batchProgress) begin() {
	if p == nil || !p.live {
		return
	}
	p.spinning = true
	go func() {
		defer close(p.ended)
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-p.stop:
				return
			case <-ticker.C:
				p.mu.Lock()
				p.frame++
				p.draw()
				p.mu.Unlock()
			}
		}
	}()
}

// end stops the spinner and wipes its line.
func (p *batchProgress) end() {
	if p == nil {
		return
	}
	if p.spinning {
		p.spinning = false
		close(p.stop)
		<-p.ended
	}
	p.mu.Lock()
	p.wipe()
	p.mu.Unlock()
}

func (p *batchProgress) started(name string) {
	if p == nil {
		return
	}
	p.mu.Lock()
	p.active = append(p.active, name)
	p.draw()
	p.mu.Unlock()
}

// finished prints name's lines, above the spinner.
func (p *batchProgress) finished(name string, lines []string) {
	if p == nil {
		for _, line := range lines {
			note("%s", line)
		}
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	for i, a := range p.active {
		if a == name {
			p.active = append(p.active[:i], p.active[i+1:]...)
			break
		}
	}
	p.done++
	p.wipe()
	note("'%s': done (%d of %d)", name, p.done, p.total)
	for _, line := range lines {
		note("%s", line)
	}
	p.draw()
}

func (p *batchProgress) draw() {
	if p.live {
		fmt.Fprintf(p.out, "\r\x1b[K%s", progressLine(p.frame, p.done, p.total, p.active, p.width()))
	}
}

func (p *batchProgress) wipe() {
	if p.live {
		fmt.Fprint(p.out, "\r\x1b[K")
	}
}

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// progressLine is the spinner line: how many are done, and which are under
// way, cut to fit width.
func progressLine(frame, done, total int, active []string, width int) string {
	line := fmt.Sprintf("%s %d of %d done", spinnerFrames[frame%len(spinnerFrames)], done, total)
	if len(active) > 0 {
		line += " — working on " + strings.Join(active, ", ")
	}
	if r := []rune(line); width > 1 && len(r) > width-1 {
		line = string(r[:width-2]) + "…"
	}
	return line
}

// repoChecker asks GitHub about a token once per repository, however many
// sandboxes at once want the answer.
type repoChecker struct {
	token   string
	mu      sync.Mutex
	reports map[string]*repoReport
}

type repoReport struct {
	once   sync.Once
	report gittoken.Report
}

func (c *repoChecker) check(repo *gittoken.Repo) gittoken.Report {
	c.mu.Lock()
	entry, ok := c.reports[repo.String()]
	if !ok {
		entry = &repoReport{}
		c.reports[repo.String()] = entry
	}
	c.mu.Unlock()
	entry.once.Do(func() { entry.report = checkGitToken(c.token, repo) })
	return entry.report
}

func giveTokenToSandbox(say func(string, ...interface{}), vibeHome string, inst state.Instance, tok batchToken, presetRules []string, checker *repoChecker) batchOutcome {
	out := batchOutcome{name: inst.Name, folder: inst.Target, oldExpires: inst.GitTokenExpires}
	// What was listed is minutes old by now (the token was chosen since), and
	// the whole record is written back: a running vibe's changes to it, or a
	// --git-token's, would be reverted.
	current, found, err := state.Load(vibeHome, inst.Name)
	switch {
	case err != nil:
		out.problem = err.Error()
		return out
	case !found:
		out.problem = "vibe has no record of it any more"
		return out
	}
	inst = current
	out.oldExpires = inst.GitTokenExpires
	repo := batchRepoOf(inst.Target)
	expires := tok.expires
	if repo == nil {
		out.pushCheck = "no GitHub remote, so not checked"
	} else {
		rep := checker.check(repo)
		if problems := rep.Problems(); rep.Unreachable == nil && len(problems) > 0 {
			out.problem = problems[0]
			return out
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
	if out.inside, err = setGitAccessSaying(say, vibeHome, &inst, access, repo); err != nil {
		out.problem, out.pushCheck = err.Error(), ""
		return out
	}
	out.given, out.newExpires = true, inst.GitTokenExpires
	return out
}

// batchSummary says which sandboxes got the token and which didn't, and why,
// with the command that tries again for each that didn't. It never has the
// token in it.
func batchSummary(outcomes []batchOutcome, now time.Time) []string {
	var lines []string
	for _, o := range outcomes {
		if o.given {
			mark := "✔"
			if o.refusedInside() {
				mark = "⚠"
			}
			lines = append(lines, fmt.Sprintf("  %s %s: %s → %s%s%s", mark, o.name,
				orNone(gittoken.DescribeExpiry(o.oldExpires, now)), orNone(gittoken.DescribeExpiry(o.newExpires, now)),
				prefixed(" — ", o.pushCheck), prefixed(" — ", o.inside.said)))
			if o.refusedInside() {
				lines = append(lines, fmt.Sprintf("      'vibe --git-token' in %s tries again", o.folder))
			}
			continue
		}
		lines = append(lines,
			fmt.Sprintf("  ✘ %s: %s", o.name, o.problem),
			fmt.Sprintf("      'vibe --git-token' in %s tries again", o.folder))
	}
	return lines
}

// refusedInside says the sandbox got the token but GitHub wouldn't let it push, checked from inside.
func (o batchOutcome) refusedInside() bool {
	return o.inside.refused
}

// batchFailures is how many sandboxes a batch left unable to push: not given the token, or given it and refused from inside.
func batchFailures(outcomes []batchOutcome) int {
	n := 0
	for _, o := range outcomes {
		if !o.given || o.refusedInside() {
			n++
		}
	}
	return n
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
		if storeDefaultToken(vibeHome, tok.token, tok.expires) {
			tok.source = gitTokenDefault
			tok.nowDefault = true
		} else {
			note("  they hold it as their own")
		}
	}
	return tok, true
}

// namedSandboxes are the candidates --sandboxes names, in the order named. A name that isn't one of them is refused, with any
// others, before anything is given: a script that names a sandbox vibe doesn't have wants telling, not a partial batch.
func namedSandboxes(candidates []state.Instance, names []string) ([]state.Instance, error) {
	byName := map[string]state.Instance{}
	for _, c := range candidates {
		byName[c.Name] = c
	}
	var picked []state.Instance
	var unknown []string
	for _, name := range names {
		if inst, ok := byName[name]; ok {
			picked = append(picked, inst)
		} else {
			unknown = append(unknown, name)
		}
	}
	if len(unknown) > 0 {
		return nil, fmt.Errorf("--sandboxes: no sandbox called %s (vibe --list shows them): nothing was given", strings.Join(unknown, ", "))
	}
	return picked, nil
}

// batchTokenFromEnv is the token in the environment variable named, checked with GitHub against the first of the picked
// sandboxes' repositories, as a token typed in is (giveTokenToSandboxes checks it against the others).
func batchTokenFromEnv(variable string, picked []state.Instance) (batchToken, error) {
	repos, _, _ := batchRepositories(picked)
	var first *gittoken.Repo
	if len(repos) > 0 {
		first = &repos[0]
	}
	access, err := gitAccessFromEnv(variable, first, nil)
	if err != nil {
		return batchToken{}, err
	}
	return batchToken{token: access.token, source: access.source, expires: access.expires}, nil
}

func sandboxNamesOf(picked []state.Instance) []string {
	names := make([]string, len(picked))
	for i, inst := range picked {
		names[i] = inst.Name
	}
	return names
}

func sandboxNames(picked []state.Instance) string {
	return strings.Join(sandboxNamesOf(picked), ", ")
}

// doBatchGitToken is --batch-git-token: tick the sandboxes to give one token
// to, say which, and give it to them. A token is never typed into the page:
// on a terminal the sandboxes are ticked and the token asked for; a script
// names them (--sandboxes) and gives the token in a variable (--git-token-env),
// which never makes it the default.
func doBatchGitToken(vibeHome string, args cliargs.Args) error {
	named := len(args.Sandboxes) > 0
	asking := !args.Force && interactive()
	switch {
	case !named && !asking:
		return fmt.Errorf("--batch-git-token asks which sandboxes to give the token to, so it needs a terminal: a script names them with --sandboxes a,b,c (and gives the token with --git-token-env VAR)")
	case args.GitTokenEnv == "" && !asking:
		return fmt.Errorf("--batch-git-token asks which token to give, which needs a terminal: with none, give it in a variable with --git-token-env VAR")
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
	var picked []state.Instance
	if named {
		if picked, err = namedSandboxes(candidates, args.Sandboxes); err != nil {
			return err
		}
	} else {
		now := timeNow()
		labels := make([]string, len(candidates))
		for i, c := range candidates {
			labels[i] = batchLabel(c, now)
		}
		ticked, ok := multiFilterFrom("which sandboxes should get the token? (none are ticked to begin with; typing matches the folder and profile too)", labels)
		if !ok || len(ticked) == 0 {
			note("no sandbox picked: nothing changed")
			return nil
		}
		picked = make([]state.Instance, len(ticked))
		for i, idx := range ticked {
			picked[i] = candidates[idx]
		}
	}
	var tok batchToken
	if args.GitTokenEnv != "" {
		if tok, err = batchTokenFromEnv(args.GitTokenEnv, picked); err != nil {
			return err
		}
	} else {
		var ok bool
		if tok, ok = askBatchToken(vibeHome, picked); !ok {
			note("no token chosen: nothing changed")
			return nil
		}
	}
	jobs := args.Jobs
	if jobs == 0 {
		jobs = batchJobs
	}
	started := timeNow()
	note("giving the token to %d sandboxes, %d at a time...", len(picked), min(jobs, len(picked)))
	progress := newBatchProgress(len(picked), os.Stderr, prompt.IsTerminal(os.Stderr), func() int { return prompt.Width(os.Stderr, 80) })
	progress.begin()
	outcomes := giveTokenToSandboxes(vibeHome, picked, tok, presetRules, jobs, progress)
	progress.end()
	if tok.nowDefault {
		offerDefaultToken(vibeHome, tok.token, tok.expires, sandboxNamesOf(picked)...)
	}
	note("")
	failed := batchFailures(outcomes)
	for _, line := range batchSummary(outcomes, timeNow()) {
		note("%s", line)
	}
	note("took %s", timeNow().Sub(started).Round(time.Second))
	if failed > 0 {
		return fmt.Errorf("%d of %d sandboxes did not get the token, or can't push with it", failed, len(outcomes))
	}
	return nil
}
