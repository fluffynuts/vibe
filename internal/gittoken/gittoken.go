// Package gittoken is what lets a sandbox's agent push to GitHub, for the
// users who opt in: working out which repository a folder pushes to,
// sending the user to GitHub to make a token for just that repository,
// checking a token they paste, and the rules the agent is given for using
// it.
//
// It is opt-in and never required. Out of the box an agent in a vibe
// sandbox has no git credentials, so the developer commits and pushes — and
// so reads the code — themselves. A token is for the user who would rather
// hand the agent a plan that takes several commits and let it get on with
// it.
//
// The token never enters the sandbox: vibe hands it to sbx as a
// sandbox-scoped secret, and sbx's proxy adds it to the sandbox's requests
// to GitHub on the way out.
package gittoken

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Repo is a GitHub repository a folder pushes to.
type Repo struct {
	Owner, Name string
	// SSH is set when the folder's remote is an SSH URL, which the token
	// doesn't cover: sbx's proxy only sees HTTPS.
	SSH bool
}

func (r Repo) String() string { return r.Owner + "/" + r.Name }

// HTTPSURL is where the agent can push to the repository with the token.
func (r Repo) HTTPSURL() string {
	return "https://github.com/" + r.Owner + "/" + r.Name + ".git"
}

var (
	httpsRemote = regexp.MustCompile(`^https?://(?:[^@/]+@)?github\.com/([^/]+)/([^/]+?)(?:\.git)?/?$`)
	scpRemote   = regexp.MustCompile(`^(?:[^@/]+@)?github\.com:([^/]+)/([^/]+?)(?:\.git)?/?$`)
	sshRemote   = regexp.MustCompile(`^ssh://(?:[^@/]+@)?github\.com(?::\d+)?/([^/]+)/([^/]+?)(?:\.git)?/?$`)
)

// ParseRemote reads a GitHub repository out of a git remote URL, in any of
// the forms git takes: https://github.com/o/r(.git), git@github.com:o/r.git
// or ssh://git@github.com/o/r.git. ok is false for anything else, a remote
// on another host included.
func ParseRemote(remote string) (repo Repo, ok bool) {
	remote = strings.TrimSpace(remote)
	if m := httpsRemote.FindStringSubmatch(remote); m != nil {
		return Repo{Owner: m[1], Name: m[2]}, true
	}
	if m := scpRemote.FindStringSubmatch(remote); m != nil {
		return Repo{Owner: m[1], Name: m[2], SSH: true}, true
	}
	if m := sshRemote.FindStringSubmatch(remote); m != nil {
		return Repo{Owner: m[1], Name: m[2], SSH: true}, true
	}
	return Repo{}, false
}

// RemoteOf is the GitHub repository dir's origin remote points at. ok is
// false when dir isn't a git repository, has no origin, or its origin isn't
// on GitHub — a new project, often, that hasn't been pushed anywhere yet.
func RemoteOf(dir string) (repo Repo, ok bool) {
	out, err := exec.Command("git", "-C", dir, "remote", "get-url", "origin").Output()
	if err != nil {
		return Repo{}, false
	}
	return ParseRemote(string(out))
}

// Kind says what sort of token a token is, from its prefix
// (https://github.blog/2021-04-05-behind-githubs-new-authentication-token-formats/).
type Kind string

const (
	FineGrained Kind = "fine-grained personal access token"
	Classic     Kind = "classic personal access token"
	OAuth       Kind = "OAuth token (what 'gh auth token' prints)"
	App         Kind = "GitHub App token"
	Unknown     Kind = "token of a kind vibe doesn't recognise"
)

// KindOf tells what sort of token token is.
func KindOf(token string) Kind {
	switch {
	case strings.HasPrefix(token, "github_pat_"):
		return FineGrained
	case strings.HasPrefix(token, "ghp_"):
		return Classic
	case strings.HasPrefix(token, "gho_"):
		return OAuth
	case strings.HasPrefix(token, "ghu_"), strings.HasPrefix(token, "ghs_"):
		return App
	}
	return Unknown
}

// AccountWide says whether a token of kind k reaches every repository its
// account can: classic and OAuth tokens can't be limited to one.
func (k Kind) AccountWide() bool {
	return k == Classic || k == OAuth
}

// Valid says whether token looks like something worth sending to GitHub:
// non-empty, and nothing in it that couldn't be in a header.
func Valid(token string) error {
	if token == "" {
		return errors.New("no token given")
	}
	for _, r := range token {
		if r <= ' ' || r > '~' {
			return errors.New("that doesn't look like a GitHub token: it has spaces or other characters no token has")
		}
	}
	return nil
}

// tokenNameLimit is the longest name GitHub's token form takes.
const tokenNameLimit = 40

// Permissions are what a token vibe asks for may do: push commits (contents),
// open pull requests, and push changes to .github/workflows — without which
// GitHub turns away any push that touches a workflow, which is a confusing
// way for a first CI file to fail.
var Permissions = []string{"contents=write", "pull_requests=write", "workflows=write"}

// NewTokenURL is GitHub's form for a new fine-grained token, filled in from
// the URL (https://docs.github.com/en/authentication/keeping-your-account-and-data-secure/managing-your-personal-access-tokens):
// its name, description, resource owner (the account or organisation that
// owns the repository; empty leaves it at the user's own) and the
// permissions above, expiring in days. GitHub doesn't let a URL pick the
// repository, so the user still has to choose it on the page.
func NewTokenURL(name, description, owner string, days int) string {
	if len(name) > tokenNameLimit {
		name = name[:tokenNameLimit]
	}
	q := []string{
		"name=" + url.QueryEscape(name),
		"description=" + url.QueryEscape(description),
	}
	if owner != "" {
		q = append(q, "target_name="+url.QueryEscape(owner))
	}
	q = append(q, fmt.Sprintf("expires_in=%d", days))
	q = append(q, Permissions...)
	return "https://github.com/settings/personal-access-tokens/new?" + strings.Join(q, "&")
}

// TokensPageURL is GitHub's list of the user's fine-grained tokens, where
// one that has expired, or is about to, can be regenerated: a new value,
// with the same repositories and permissions.
const TokensPageURL = "https://github.com/settings/personal-access-tokens"

// Lifetimes are the choices offered for how long a new token lasts, in days.
var Lifetimes = []int{7, 30, 90}

// WarnWithin is how close to expiring a token has to be before vibe says
// so at every session start.
const WarnWithin = 3 * 24 * time.Hour

// Expired says whether a token expiring at expires has, by now. A zero
// expires is a token that never expires, or one whose expiry isn't known.
func Expired(expires, now time.Time) bool {
	return !expires.IsZero() && !now.Before(expires)
}

// Expiring says whether a token expiring at expires has less than
// WarnWithin to go, by now, or has already expired.
func Expiring(expires, now time.Time) bool {
	return !expires.IsZero() && expires.Sub(now) < WarnWithin
}

// DescribeExpiry says when a token expires, for the user: "expires
// 2026-10-16 (in 6 days)", "expired 2026-10-09", or "" when that isn't
// known.
func DescribeExpiry(expires, now time.Time) string {
	if expires.IsZero() {
		return ""
	}
	date := expires.Local().Format("2006-01-02 15:04")
	if Expired(expires, now) {
		return "expired " + date
	}
	return fmt.Sprintf("expires %s (in %s)", date, roughly(expires.Sub(now)))
}

// roughly is d to the nearest day, or hour when it's less than a day.
func roughly(d time.Duration) string {
	switch days := int((d + 12*time.Hour) / (24 * time.Hour)); {
	case d < 24*time.Hour:
		hours := int((d + 30*time.Minute) / time.Hour)
		if hours <= 1 {
			return "under an hour"
		}
		return fmt.Sprintf("%d hours", hours)
	case days == 1:
		return "1 day"
	default:
		return fmt.Sprintf("%d days", days)
	}
}

// Checker asks GitHub about a token. The zero value asks github.com.
type Checker struct {
	// APIBase and GitBase stand in for https://api.github.com and
	// https://github.com under test.
	APIBase, GitBase string
	Client           *http.Client
}

// Report is what GitHub said about a token.
type Report struct {
	// Unreachable is set when GitHub couldn't be asked at all; nothing
	// else is known then.
	Unreachable error
	// Rejected is set when GitHub doesn't accept the token: mistyped,
	// expired or revoked.
	Rejected bool
	// Login is the account the token belongs to.
	Login string
	// Expires is when the token stops working, or zero when GitHub says it
	// never does (or didn't say).
	Expires time.Time
	// Repo is the repository the token was checked against, if any, and
	// what it may do there: see it at all, and push to it.
	Repo            *Repo
	SeesRepo        bool
	CanPush         bool
	RepoUnreachable error
	// OtherRepos are other repositories of Repo's owner that a
	// fine-grained token was given too (see otherReachable) — all of them,
	// when it was left on GitHub's default of "All repositories". Only
	// asked about when the token can push to Repo. Untried is how many of
	// the owner's public repositories there were too many to try.
	OtherRepos []string
	Untried    int
}

// Check asks GitHub whether it accepts token and, when repo is given,
// whether the token can push to it.
//
// Pushing is asked of git's own endpoint rather than the API: the API's
// repository permissions are the account's, not the token's, so a read-only
// token on the user's own repository would look like it could push. Asking
// for git-receive-pack's advertisement is the first thing `git push` does,
// and GitHub only answers it for a token that may push. It changes nothing.
func (c Checker) Check(token string, repo *Repo) Report {
	var rep Report
	resp, err := c.get(c.apiBase()+"/user", func(h http.Header) {
		h.Set("Authorization", "Bearer "+token)
		h.Set("Accept", "application/vnd.github+json")
	})
	if err != nil {
		rep.Unreachable = err
		return rep
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusUnauthorized:
		rep.Rejected = true
		return rep
	default:
		rep.Unreachable = fmt.Errorf("GitHub answered %s", resp.Status)
		return rep
	}
	var user struct {
		Login string `json:"login"`
	}
	json.NewDecoder(io.LimitReader(resp.Body, 64*1024)).Decode(&user)
	rep.Login = user.Login
	rep.Expires = parseExpiry(resp.Header.Get("GitHub-Authentication-Token-Expiration"))

	if repo == nil {
		return rep
	}
	rep.Repo = repo
	status, err := c.receivePack(token, *repo)
	if err != nil {
		rep.RepoUnreachable = err
		return rep
	}
	switch status {
	case http.StatusOK:
		rep.SeesRepo, rep.CanPush = true, true
	case http.StatusForbidden:
		rep.SeesRepo = true
	case http.StatusNotFound, http.StatusUnauthorized:
	default:
		rep.RepoUnreachable = fmt.Errorf("GitHub answered %d %s", status, http.StatusText(status))
	}
	if rep.CanPush && KindOf(token) == FineGrained {
		rep.OtherRepos, rep.Untried = c.otherReachable(token, *repo)
	}
	return rep
}

// Limits on how hard otherReachable looks: how many pages of the
// account's repositories it reads (100 a page), how many of the owner's
// public repositories it tries pushing to, and how many at once. They're
// vars so tests can lower them.
var (
	listPages   = 10
	probeLimit  = 300
	probeWorker = 8
)

// otherReachable says which of repo's owner's other repositories the token
// was given too.
//
// GitHub can't be asked which repositories a fine-grained token was given.
// The account's repository list, read with the token, holds every
// repository the token can read — and every token can read every public
// repository, the user's organisations' included. So that list is read for
// the owner's repositories alone, and:
//
//   - a private one the token can see is one it was given: there is no
//     other way it could read it;
//   - a public one, it could see either way, so the token tries pushing to
//     it, as receivePack does — which only a token given it can.
//
// The public ones are tried several at a time, but there may be many: past
// probeLimit, untried says how many weren't. Anything going wrong says
// nothing about a repository: this is a warning, never a reason to turn a
// token down.
func (c Checker) otherReachable(token string, repo Repo) (others []string, untried int) {
	var public []Repo
	for page := 1; page <= listPages; page++ {
		repos, ok := c.listRepos(token, page)
		if !ok {
			break
		}
		for _, r := range repos {
			other := Repo{Owner: r.Owner.Login, Name: r.Name}
			if !strings.EqualFold(other.Owner, repo.Owner) || strings.EqualFold(other.Name, repo.Name) {
				continue
			}
			if r.Private {
				others = append(others, other.String())
			} else {
				public = append(public, other)
			}
		}
		if len(repos) < 100 {
			break
		}
	}
	if len(public) > probeLimit {
		untried = len(public) - probeLimit
		public = public[:probeLimit]
	}

	pushable := make([]bool, len(public))
	next := make(chan int)
	var wg sync.WaitGroup
	for w := 0; w < probeWorker; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range next {
				status, err := c.receivePack(token, public[i])
				pushable[i] = err == nil && status == http.StatusOK
			}
		}()
	}
	for i := range public {
		next <- i
	}
	close(next)
	wg.Wait()
	for i, ok := range pushable {
		if ok {
			others = append(others, public[i].String())
		}
	}
	return others, untried
}

type listedRepo struct {
	Name    string `json:"name"`
	Private bool   `json:"private"`
	Owner   struct {
		Login string `json:"login"`
	} `json:"owner"`
}

// listRepos reads one page of the account's repositories, with the token.
func (c Checker) listRepos(token string, page int) ([]listedRepo, bool) {
	resp, err := c.get(fmt.Sprintf("%s/user/repos?per_page=100&page=%d", c.apiBase(), page), func(h http.Header) {
		h.Set("Authorization", "Bearer "+token)
		h.Set("Accept", "application/vnd.github+json")
	})
	if err != nil {
		return nil, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, false
	}
	var repos []listedRepo
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8*1024*1024)).Decode(&repos); err != nil {
		return nil, false
	}
	return repos, true
}

// receivePack asks for git-receive-pack's advertisement for repo with
// token, and returns GitHub's answer: 200 when the token may push.
func (c Checker) receivePack(token string, repo Repo) (int, error) {
	refs := fmt.Sprintf("%s/%s/%s.git/info/refs?service=git-receive-pack", c.gitBase(), repo.Owner, repo.Name)
	resp, err := c.get(refs, func(h http.Header) {
		h.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("x-access-token:"+token)))
	})
	if err != nil {
		return 0, err
	}
	resp.Body.Close()
	return resp.StatusCode, nil
}

// TooBroad says, when the token was given repositories besides the one it
// was checked against, which, as a sentence; "" when it wasn't.
func (r Report) TooBroad() string {
	if len(r.OtherRepos) == 0 {
		return ""
	}
	shown := r.OtherRepos
	more := ""
	if len(shown) > 5 {
		more = fmt.Sprintf(" and %d more", len(shown)-5)
		shown = shown[:5]
	}
	return fmt.Sprintf("the token reaches other repositories of %s too (%s%s): only %s should be selected",
		r.Repo.Owner, strings.Join(shown, ", "), more, r.Repo)
}

// Problems are what's wrong with the token for pushing, as sentences; none
// means it's good to use. Warnings that don't stop it being used (an
// account-wide token, one that never expires) aren't among them.
func (r Report) Problems() []string {
	var p []string
	switch {
	case r.Unreachable != nil:
		p = append(p, fmt.Sprintf("couldn't ask GitHub about the token: %s", r.Unreachable))
	case r.Rejected:
		p = append(p, "GitHub doesn't accept this token: it's mistyped, expired or revoked")
	case r.Repo == nil:
	case r.RepoUnreachable != nil:
		p = append(p, fmt.Sprintf("couldn't ask GitHub about %s: %s", r.Repo, r.RepoUnreachable))
	case !r.SeesRepo:
		p = append(p, fmt.Sprintf("the token can't see %s: on GitHub, give it access to that repository", r.Repo))
	case !r.CanPush:
		p = append(p, fmt.Sprintf("the token can see %s but not push to it: give it Contents read and write", r.Repo))
	}
	return p
}

func parseExpiry(v string) time.Time {
	v = strings.TrimSpace(v)
	for _, layout := range []string{"2006-01-02 15:04:05 MST", "2006-01-02 15:04:05 -0700", time.RFC3339} {
		if t, err := time.Parse(layout, v); err == nil {
			return t
		}
	}
	return time.Time{}
}

func (c Checker) get(u string, headers func(http.Header)) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "vibe")
	headers(req.Header)
	client := c.Client
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	// Never follow a redirect: it would carry the token somewhere else.
	cp := *client
	cp.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return cp.Do(req)
}

func (c Checker) apiBase() string {
	if c.APIBase != "" {
		return strings.TrimRight(c.APIBase, "/")
	}
	return "https://api.github.com"
}

func (c Checker) gitBase() string {
	if c.GitBase != "" {
		return strings.TrimRight(c.GitBase, "/")
	}
	return "https://github.com"
}

// Rules the user can set on how the agent pushes, besides never
// force-pushing, which it is always told.
const (
	RuleFeatureBranch   = "feature-branch"
	RuleNoDefaultBranch = "no-default-branch"
)

// RuleLabels describes each rule, for the question that offers them.
var RuleLabels = map[string]string{
	RuleFeatureBranch:   "only work in a feature branch",
	RuleNoDefaultBranch: "never push the default branch (main/master)",
}

// RuleOrder is the order rules are offered and written in.
var RuleOrder = []string{RuleFeatureBranch, RuleNoDefaultBranch}

// SandboxFile is where, in the sandbox, vibe writes the agent's
// instructions for pushing. The kit's own instructions (KitInstructions)
// point the agent at it, since a token can be given to a sandbox long after
// the kit made it.
const SandboxFile = "/home/agent/.config/vibe/git-push.md"

// KitInstructions is the part of every sandbox's agent instructions about
// pushing: whether the agent may push is settled by SandboxFile being
// there, which a sandbox without a token never has.
const KitInstructions = "## Pushing to GitHub\n\n" +
	"If `" + SandboxFile + "` exists, the user has given this sandbox a GitHub token: " +
	"read that file before you commit or push, and follow it. If it doesn't exist, " +
	"this sandbox has no GitHub credentials from vibe.\n"

// Instructions is the content of SandboxFile for a sandbox whose token
// pushes to repo (nil when the folder had no GitHub remote), with rules,
// expiring at expires (zero when that isn't known).
func Instructions(repo *Repo, rules []string, expires time.Time) string {
	var b strings.Builder
	b.WriteString("# Pushing to GitHub from this sandbox\n\n")
	b.WriteString("The user has given this sandbox a GitHub token, so that you can commit and push " +
		"as you work through what they've asked for, without stopping to ask each time. " +
		"The token never enters the sandbox: the sandbox's proxy adds it to requests to GitHub, " +
		"so `git push` over HTTPS and `gh` both work, though `gh auth status` says you aren't logged in.\n\n")
	if repo != nil {
		fmt.Fprintf(&b, "The token is for %s.", repo)
		if repo.SSH {
			fmt.Fprintf(&b, " This folder's `origin` remote is an SSH URL, which the token doesn't cover: "+
				"push over HTTPS instead, with `git push %s <branch>`, and leave the remote as it is.", repo.HTTPSURL())
		}
		b.WriteString("\n\n")
	}
	if !expires.IsZero() {
		fmt.Fprintf(&b, "The token expires %s.\n\n", expires.UTC().Format("2006-01-02 15:04 MST"))
	}
	b.WriteString("If a push or `gh` fails because GitHub won't accept the credentials, the token has most likely " +
		"expired or been revoked: stop, and tell the user to run `vibe --git-token` on their machine to renew it. " +
		"Don't look for other credentials, or other ways to push.\n\n")
	b.WriteString("Rules the user has set:\n\n")
	b.WriteString("- Never force-push (no `--force`, `--force-with-lease` or `+` refspecs), and never rewrite history that has been pushed.\n")
	has := map[string]bool{}
	for _, r := range rules {
		has[r] = true
	}
	if has[RuleFeatureBranch] {
		b.WriteString("- Only work in a feature branch: make one before your first commit, and never commit to the default branch.\n")
	}
	if has[RuleNoDefaultBranch] {
		b.WriteString("- Never push the default branch (main/master): push your branch and open a pull request with `gh pr create` instead.\n")
	}
	return b.String()
}

// ValidRules checks every rule is one vibe knows, and returns them in
// RuleOrder.
func ValidRules(rules []string) ([]string, error) {
	want := map[string]bool{}
	for _, r := range rules {
		r = strings.TrimSpace(r)
		if r == "" {
			continue
		}
		if _, ok := RuleLabels[r]; !ok {
			return nil, fmt.Errorf("unknown git rule %q: expected %s", r, strings.Join(RuleOrder, ", "))
		}
		want[r] = true
	}
	var out []string
	for _, r := range RuleOrder {
		if want[r] {
			out = append(out, r)
		}
	}
	return out, nil
}

// Load reads a token stored by Save. ok is false when there is none.
func Load(path string) (token string, ok bool, err error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("reading git token: %w", err)
	}
	token = strings.TrimSpace(string(data))
	return token, token != "", nil
}

// Save stores token at path, readable by the user alone.
func Save(path, token string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("creating git token dir: %w", err)
	}
	// Written new (CreateTemp makes it 0600), then renamed, so the token
	// never sits in a file someone else could read, even briefly —
	// WriteFile would keep an existing file's mode.
	tmp, err := os.CreateTemp(filepath.Dir(path), ".token-*")
	if err != nil {
		return fmt.Errorf("writing git token: %w", err)
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.WriteString(token + "\n"); err != nil {
		tmp.Close()
		return fmt.Errorf("writing git token: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("writing git token: %w", err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("writing git token: %w", err)
	}
	return nil
}

// Remove deletes a token stored by Save, and the expiry SaveExpiry kept
// beside it; there being none is fine.
func Remove(path string) error {
	for _, p := range []string{path, expiryPath(path)} {
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("removing git token: %w", err)
		}
	}
	return nil
}

func expiryPath(tokenPath string) string { return tokenPath + ".expires" }

// SaveExpiry keeps, beside the token Save stored at tokenPath, when it
// expires — so it can be checked without asking GitHub. A zero expires
// (never, or not known) removes what was kept.
func SaveExpiry(tokenPath string, expires time.Time) error {
	if expires.IsZero() {
		if err := os.Remove(expiryPath(tokenPath)); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("removing git token expiry: %w", err)
		}
		return nil
	}
	if err := os.WriteFile(expiryPath(tokenPath), []byte(expires.UTC().Format(time.RFC3339)+"\n"), 0o600); err != nil {
		return fmt.Errorf("writing git token expiry: %w", err)
	}
	return nil
}

// LoadExpiry is what SaveExpiry kept beside the token at tokenPath: zero
// when nothing was, or it can't be read.
func LoadExpiry(tokenPath string) time.Time {
	data, err := os.ReadFile(expiryPath(tokenPath))
	if err != nil {
		return time.Time{}
	}
	t, _ := time.Parse(time.RFC3339, strings.TrimSpace(string(data)))
	return t
}
