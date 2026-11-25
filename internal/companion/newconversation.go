package companion

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"vibe/internal/review"
)

// What "New conversation" does besides forgetting the conversation: close
// the review, clear the clipboard, start a new branch (see
// docs/adr/2026-10-18-new-conversation.md). Git that changes the repository runs in
// the sandbox (Config.SandboxGit): git run on this machine would run hooks
// and config the sandbox can write.

// gitWithin bounds git in the sandbox reading or changing the repository
// there, which is quick; fetchWithin bounds a fetch from origin, which goes
// over the network. Both are backstops: a fetch stopped by one is only a
// check not made, said so.
const (
	gitWithin   = time.Minute
	fetchWithin = 2 * time.Minute
)

// closedForNew is the reason given for a review closed for a new
// conversation.
const closedForNew = "Closed for a new conversation."

type clipboardOffer struct {
	Sent        int    `json:"sent"`
	Unsent      int    `json:"unsent"`
	Unavailable string `json:"unavailable,omitempty"`
	// Retry says the page didn't answer, which is likely to pass (the sandbox's start-up script may
	// still be starting it): the dialog asks again.
	Retry bool `json:"retry,omitempty"`
}

type branchOffer struct {
	// Current is the branch checked out, "" when HEAD is detached.
	Current    string `json:"current"`
	Dirty      bool   `json:"dirty"`
	Conflicted bool   `json:"conflicted"`
	// Default is the default branch, without "origin/"; "" when it can't be
	// told, NoDefault saying why. DefaultSet says it was set for the
	// repository, origin not saying: it's branched from as it is here, and
	// DefaultEmpty says it has no commits yet to branch from.
	Default      string `json:"default,omitempty"`
	DefaultSet   bool   `json:"defaultSet,omitempty"`
	DefaultEmpty bool   `json:"defaultEmpty,omitempty"`
	NoDefault    string `json:"noDefault,omitempty"`
	Unavailable  string `json:"unavailable,omitempty"`
	// Branches are the local branches; Remote are origin's, as last fetched. The dialog says as a
	// name is typed that one is taken.
	Branches []string `json:"branches,omitempty"`
	Remote   []string `json:"remote,omitempty"`
}

// conversationNewOptions is what the dialog can offer: a POST, as it may
// wake the sandbox to find out.
func (s *Server) conversationNewOptions(w http.ResponseWriter, r *http.Request) {
	steps := s.newSteps()
	ctx := context.WithValue(r.Context(), gitRunsKey{}, &steps.gitRuns)
	body := map[string]interface{}{"review": s.reviewOpen()}
	hasClipboard, hasRepository := s.clipboardAt() != "", s.repositoryHere()
	steps.done("the review and the folder's git status on the host")
	if hasClipboard || hasRepository {
		asleep := ""
		if err := s.wake(ctx); err != nil {
			asleep = err.Error()
		}
		steps.done("waking the sandbox")
		if hasClipboard {
			body["clipboard"] = s.clipboardOffer(ctx, asleep)
			steps.done("the clipboard page")
		}
		if hasRepository {
			body["branch"] = s.branchOffer(ctx, asleep)
			steps.done("git in the sandbox")
		}
	}
	steps.logIfSlow("New conversation's options")
	writeJSON(w, http.StatusOK, body)
}

// gitRunsKey is the context key of a count of the times sandboxGit ran git in the sandbox, for steps.
type gitRunsKey struct{}

// steps times what a request does, step by step, for vibe's log when the whole takes longer than slowRequest: the request's own
// line (see timed) says that it was slow, this says where the time went.
type steps struct {
	s       *Server
	began   time.Time
	last    time.Time
	took    []string
	gitRuns atomic.Int32
}

func (s *Server) newSteps() *steps {
	now := s.clock()
	return &steps{s: s, began: now, last: now}
}

func (st *steps) done(what string) {
	now := st.s.clock()
	line := what + " " + now.Sub(st.last).Round(100*time.Millisecond).String()
	if what == "git in the sandbox" {
		if runs := st.gitRuns.Load(); runs == 1 {
			line += " (1 run)"
		} else {
			line += fmt.Sprintf(" (%d runs)", runs)
		}
	}
	st.took = append(st.took, line)
	st.last = now
}

func (st *steps) logIfSlow(what string) {
	if took := st.s.clock().Sub(st.began); took > slowRequest && st.s.cfg.Log != nil {
		st.s.cfg.Log(fmt.Sprintf("companion page: %s took %s: %s", what, took.Round(100*time.Millisecond), strings.Join(st.took, ", ")))
	}
}

func (s *Server) wake(ctx context.Context) error {
	if s.cfg.Wake == nil {
		return nil
	}
	return s.cfg.Wake(ctx, func(string) {})
}

// reviewOpen says there's a review to close. One that can't be read isn't
// offered: the Review tab says what's wrong with it.
func (s *Server) reviewOpen() bool {
	if s.cfg.ReviewTab == nil {
		return false
	}
	rv, err := s.cfg.ReviewTab.Store.Load()
	return err == nil && rv.Closed == "" && (!rv.Started.IsZero() || len(rv.Threads) > 0)
}

// repositoryHere says the sandbox's folder is in a git repository the
// sandbox's git can be asked about.
func (s *Server) repositoryHere() bool {
	if s.cfg.Workspace == "" || s.cfg.SandboxGit == nil {
		return false
	}
	read := s.cfg.Git
	if read == nil {
		read = readGit
	}
	return read(s.cfg.Workspace) != nil
}

// couldntConnect is whether err is the page not answering at all, which a moment may cure, rather
// than it answering with something that won't change.
func couldntConnect(err error) bool {
	var failed *url.Error
	return errors.As(err, &failed) && !errors.Is(err, context.Canceled)
}

func (s *Server) clipboardOffer(ctx context.Context, asleep string) clipboardOffer {
	if asleep != "" {
		return clipboardOffer{Unavailable: asleep}
	}
	items, err := clipboardItems(ctx, pasteClient, s.clipboardAt())
	if err != nil {
		return clipboardOffer{Unavailable: "The clipboard page couldn't be reached: " + err.Error(), Retry: couldntConnect(err)}
	}
	var offer clipboardOffer
	for _, item := range items {
		if item.sent() {
			offer.Sent++
		} else {
			offer.Unsent++
		}
	}
	return offer
}

func (s *Server) branchOffer(ctx context.Context, asleep string) branchOffer {
	if asleep != "" {
		return branchOffer{Unavailable: asleep}
	}
	repo, err := s.repoState(ctx)
	if err != nil {
		return branchOffer{Unavailable: "Git in the sandbox can't read the repository: " + err.Error()}
	}
	tree := repo.tree
	offer := branchOffer{Current: tree.branch, Dirty: tree.dirty, Conflicted: tree.conflicted, Branches: repo.local, Remote: repo.remote}
	if tree.conflicted {
		offer.Unavailable = "A merge has conflicts: resolve them before branching."
	}
	def, err := s.defaultBranch(ctx)
	switch {
	case err != nil:
		offer.NoDefault = err.Error()
	case def.Name == "":
		offer.NoDefault = def.Why
	default:
		offer.Default, offer.DefaultSet = def.Name, def.Set
		offer.DefaultEmpty = def.Set && !slices.Contains(repo.local, def.Name)
	}
	return offer
}

func (s *Server) hasLocalBranch(ctx context.Context, name string) bool {
	_, err := s.sandboxGit(ctx, gitWithin, "rev-parse", "--verify", "--quiet", "refs/heads/"+name)
	return err == nil
}

// sandboxGit runs git in the sandbox's folder, within limit; one it gave up
// on says what it waited for.
func (s *Server) sandboxGit(ctx context.Context, limit time.Duration, args ...string) (string, error) {
	if runs, ok := ctx.Value(gitRunsKey{}).(*atomic.Int32); ok {
		runs.Add(1)
	}
	ctx, cancel := context.WithTimeout(ctx, limit)
	defer cancel()
	out, err := s.cfg.SandboxGit(ctx, args...)
	if err != nil && errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return out, fmt.Errorf("git %s didn't finish within %s, so it was stopped", gitCommand(args), limit)
	}
	return out, err
}

type treeState struct {
	branch            string
	dirty, conflicted bool
}

// treeState is the branch checked out ("" when HEAD is detached), and whether the tree has changes, conflicts among them.
func (s *Server) treeState(ctx context.Context) (treeState, error) {
	repo, err := s.repoState(ctx)
	return repo.tree, err
}

// repoState is the tree's state and the branches, here and at origin as last fetched.
type repoState struct {
	tree          treeState
	local, remote []string
}

// repoStateAlias reads the repository in one run of git in the sandbox: each run is an sbx exec, which costs a second or more
// of sbx's own. The sandbox is asked to run git and nothing else, so the commands go as a git alias, which git runs with sh.
const (
	repoStateEnd   = "vibe-refs-follow"
	repoStateAlias = "!git status --porcelain=v2 --branch && echo " + repoStateEnd +
		" && git for-each-ref '--format=%(refname)' refs/heads refs/remotes/origin"
)

func (s *Server) repoState(ctx context.Context) (repoState, error) {
	out, err := s.sandboxGit(ctx, gitWithin, "-c", "alias.vibe-read-state="+repoStateAlias, "vibe-read-state")
	if err != nil {
		return repoState{}, err
	}
	status, refs, found := strings.Cut(out, repoStateEnd+"\n")
	if !found {
		return repoState{}, fmt.Errorf("git in the sandbox said something unexpected: %q", out)
	}
	var st repoState
	for _, line := range strings.Split(status, "\n") {
		switch {
		case strings.HasPrefix(line, "# branch.head "):
			if head := strings.TrimPrefix(line, "# branch.head "); head != "(detached)" {
				st.tree.branch = head
			}
		case strings.HasPrefix(line, "u "):
			st.tree.dirty, st.tree.conflicted = true, true
		case strings.HasPrefix(line, "1 "), strings.HasPrefix(line, "2 "), strings.HasPrefix(line, "? "):
			st.tree.dirty = true
		}
	}
	for _, ref := range strings.Fields(refs) {
		switch {
		case strings.HasPrefix(ref, "refs/heads/"):
			st.local = append(st.local, strings.TrimPrefix(ref, "refs/heads/"))
		case strings.HasPrefix(ref, "refs/remotes/origin/") && ref != "refs/remotes/origin/HEAD":
			st.remote = append(st.remote, strings.TrimPrefix(ref, "refs/remotes/origin/"))
		}
	}
	return st, nil
}

// gitCommand is the git command args run, past any -c settings before it.
func gitCommand(args []string) string {
	for len(args) > 1 && args[0] == "-c" {
		args = args[2:]
	}
	return args[0]
}

// defaultBranch is the folder's default branch, as the Review tab has it.
// Reading it changes nothing, so it's read here; an error is the
// repository not being read.
func (s *Server) defaultBranch(ctx context.Context) (review.Mainline, error) {
	ctx, cancel := context.WithTimeout(ctx, gitWithin)
	defer cancel()
	return review.DefaultBranch(ctx, s.cfg.Workspace)
}

// conversationNewFetch brings origin's default branch up to date, and says
// how the local one compares with it: checked false, saying why, when the
// fetch failed.
func (s *Server) conversationNewFetch(w http.ResponseWriter, r *http.Request) {
	if !s.repositoryHere() {
		writeError(w, http.StatusNotFound, "this sandbox's folder isn't a git repository")
		return
	}
	ctx := r.Context()
	mainline, err := s.defaultBranch(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if mainline.Name == "" {
		writeError(w, http.StatusConflict, mainline.Why)
		return
	}
	if mainline.Set {
		writeError(w, http.StatusConflict, "the default branch was set for this repository, as origin doesn't say: there's no copy at origin to check")
		return
	}
	def := mainline.Name
	if _, err := s.sandboxGit(ctx, fetchWithin, "fetch", "--no-tags", "origin", "+refs/heads/"+def+":refs/remotes/origin/"+def); err != nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{"checked": false, "why": err.Error()})
		return
	}
	if _, err := s.sandboxGit(ctx, gitWithin, "rev-parse", "--verify", "--quiet", "refs/heads/"+def); err != nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{"checked": true, "local": false})
		return
	}
	out, err := s.sandboxGit(ctx, gitWithin, "rev-list", "--left-right", "--count", "refs/heads/"+def+"...refs/remotes/origin/"+def)
	ahead, behind, ok := twoCounts(out)
	if err != nil || !ok {
		writeJSON(w, http.StatusOK, map[string]interface{}{"checked": false, "why": fmt.Sprintf("local %s couldn't be compared with origin's: %v", def, err)})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"checked": true, "local": true, "ahead": ahead, "behind": behind})
}

// twoCounts reads rev-list --left-right --count's "A\tB".
func twoCounts(out string) (left, right int, ok bool) {
	fields := strings.Fields(out)
	if len(fields) != 2 {
		return 0, 0, false
	}
	left, errLeft := strconv.Atoi(fields[0])
	right, errRight := strconv.Atoi(fields[1])
	return left, right, errLeft == nil && errRight == nil
}

type newBranch struct {
	Name string `json:"name"`
	// From is "here" or "default".
	From string `json:"from"`
	// Update fast-forwards the local default branch to origin's first.
	Update bool `json:"update"`
}

type newConversation struct {
	// Clipboard is "" (leave it), "sent" (the items sent already) or "all".
	Clipboard string     `json:"clipboard"`
	Review    bool       `json:"review"`
	Branch    *newBranch `json:"branch"`
}

// refused is a request that can't be done as asked, with the status to
// answer it with.
type refused struct {
	status int
	err    error
}

func (r refused) Error() string { return r.err.Error() }

func refuse(status int, format string, args ...interface{}) refused {
	return refused{status, fmt.Errorf(format, args...)}
}

// conversationNew begins the conversation anew, doing first what was asked
// with it. The branch comes first: if it can't be made, nothing else is
// done. A failure after it is said in the new conversation.
func (s *Server) conversationNew(w http.ResponseWriter, r *http.Request) {
	var req newConversation
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "expected a request")
		return
	}
	if req.Clipboard != "" && req.Clipboard != "sent" && req.Clipboard != "all" {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("clearing the clipboard is \"sent\" or \"all\", not %q", req.Clipboard))
		return
	}
	if s.convo.answeringNow() {
		writeError(w, http.StatusConflict, "stop the agent first")
		return
	}
	ctx := r.Context()
	var done, failed []string
	asleep := error(nil)
	if req.Branch != nil || req.Clipboard != "" {
		asleep = s.wake(ctx)
	}
	if req.Branch != nil {
		if asleep != nil {
			writeError(w, http.StatusConflict, asleep.Error())
			return
		}
		said, problem, err := s.makeBranch(ctx, *req.Branch)
		if err != nil {
			status := http.StatusInternalServerError
			var no refused
			if errors.As(err, &no) {
				status = no.status
			}
			writeError(w, status, err.Error())
			return
		}
		done = append(done, said)
		if problem != "" {
			failed = append(failed, problem)
		}
	}
	if req.Review && s.reviewOpen() {
		if _, err := s.cfg.ReviewTab.Store.Do(review.Request{Op: "close", Body: closedForNew}, review.User); err != nil {
			failed = append(failed, "The review couldn't be closed: "+err.Error())
		} else {
			done = append(done, "Closed the review.")
		}
	}
	if req.Clipboard != "" {
		if asleep != nil {
			failed = append(failed, "The clipboard couldn't be cleared: "+asleep.Error())
		} else if n, err := s.clearClipboard(ctx, req.Clipboard == "all"); err != nil {
			failed = append(failed, "The clipboard couldn't be cleared: "+err.Error())
		} else {
			done = append(done, fmt.Sprintf("Cleared the clipboard (%s).", items(n)))
		}
	}
	ended, ok := s.convo.supersede()
	if len(ended) > 0 {
		// Said in the new conversation: the old one, where supersede said it, is gone from the page.
		done = append(done, endedWords(ended))
	}
	if !ok || !s.convo.afresh(strings.Join(done, " "), failed) {
		msg := "stop the agent first"
		if len(done) > 0 {
			msg += " (already done: " + strings.Join(done, " ") + ")"
		}
		writeError(w, http.StatusConflict, msg)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func items(n int) string {
	if n == 1 {
		return "1 item"
	}
	return fmt.Sprintf("%d items", n)
}

// makeBranch makes the new branch and checks it out, saying what it did,
// and what it couldn't (bringing the local default branch up to date can
// fail without stopping the branch being made). An error means no branch
// was made.
func (s *Server) makeBranch(ctx context.Context, b newBranch) (said, problem string, err error) {
	if !s.repositoryHere() {
		return "", "", refuse(http.StatusBadRequest, "this sandbox's folder isn't a git repository")
	}
	if b.From != "here" && b.From != "default" {
		return "", "", refuse(http.StatusBadRequest, "a branch is made from \"here\" or \"default\", not %q", b.From)
	}
	if b.Name == "" {
		return "", "", refuse(http.StatusBadRequest, "Name the new branch.")
	}
	// Anything starting with - would be taken for one of git's options.
	if strings.HasPrefix(b.Name, "-") {
		return "", "", refuse(http.StatusBadRequest, "A branch name can't start with -.")
	}
	if _, err := s.sandboxGit(ctx, gitWithin, "check-ref-format", "refs/heads/"+b.Name); err != nil {
		return "", "", refuse(http.StatusBadRequest, "%q can't be a branch name (no spaces, and no ending in / or .lock, among git's rules).", b.Name)
	}
	if _, err := s.sandboxGit(ctx, gitWithin, "rev-parse", "--verify", "--quiet", "refs/heads/"+b.Name); err == nil {
		return "", "", refuse(http.StatusBadRequest, "There's already a branch called %s.", b.Name)
	}
	tree, err := s.treeState(ctx)
	if err != nil {
		return "", "", fmt.Errorf("git in the sandbox can't read the repository: %w", err)
	}
	if tree.conflicted {
		return "", "", refuse(http.StatusConflict, "A merge has conflicts: resolve them before branching.")
	}
	// start is what a branch from the default one starts at, as said;
	// startRef, as given to git.
	def, start, startRef := "", "", ""
	if b.From == "default" || b.Update {
		mainline, err := s.defaultBranch(ctx)
		if err != nil {
			return "", "", err
		}
		if mainline.Name == "" {
			return "", "", refuse(http.StatusConflict, "%s", mainline.Why)
		}
		def, start, startRef = mainline.Name, "origin/"+mainline.Name, "refs/remotes/origin/"+mainline.Name
		if mainline.Set {
			// Set for the repository, origin not saying: there's no copy at
			// origin, so it's the one here.
			if b.Update {
				return "", "", refuse(http.StatusBadRequest, "%s was set as the default branch, as origin doesn't say: there's no copy at origin to bring it up to date from.", def)
			}
			if !s.hasLocalBranch(ctx, def) {
				return "", "", refuse(http.StatusConflict, "%s has no commits yet, so there's nothing to branch from: branch from here.", def)
			}
			start, startRef = def, "refs/heads/"+def
		}
	}
	if b.From == "default" && tree.dirty {
		return "", "", refuse(http.StatusConflict, "The current branch has uncommitted changes, so %s's files can't be checked out over them. Commit or stash them, or branch from here.", start)
	}
	updated := false
	if b.Update {
		if _, err := s.sandboxGit(ctx, gitWithin, "rev-parse", "--verify", "--quiet", "refs/heads/"+def); err == nil {
			// Fetching from the repository itself moves the branch only as a
			// fast-forward, and never one checked out in a worktree.
			if _, err := s.sandboxGit(ctx, gitWithin, "fetch", "--no-tags", ".", "refs/remotes/origin/"+def+":refs/heads/"+def); err != nil {
				problem = fmt.Sprintf("Local %s wasn't brought up to date: %v", def, err)
			} else {
				updated = true
			}
		}
	}
	args := []string{"switch", "--no-track", "-c", b.Name}
	from := "from " + tree.branch
	if tree.branch == "" {
		from = "from the detached HEAD"
	}
	if b.From == "default" {
		args = append(args, startRef)
		from = "from " + start
	}
	if _, err := s.sandboxGit(ctx, gitWithin, args...); err != nil {
		if updated {
			return "", "", fmt.Errorf("the branch couldn't be made: %w (local %s was brought up to date)", err, def)
		}
		return "", "", fmt.Errorf("the branch couldn't be made: %w", err)
	}
	said = "Started on new branch " + b.Name + ", " + from
	if updated {
		said += " (local " + def + " brought up to date first)"
	}
	return said + ".", problem, nil
}

// clearClipboard deletes the clipboard page's items: those sent to the
// agent already, or all of them, saying how many went.
func (s *Server) clearClipboard(ctx context.Context, all bool) (int, error) {
	base := s.clipboardAt()
	if base == "" {
		return 0, errors.New("this sandbox has no clipboard page")
	}
	if all {
		var body struct {
			Deleted int `json:"deleted"`
		}
		if err := clipboardDelete(ctx, base+"/api/items", &body); err != nil {
			return 0, err
		}
		return body.Deleted, nil
	}
	list, err := clipboardItems(ctx, pasteClient, base)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, item := range list {
		if !item.sent() {
			continue
		}
		if err := clipboardDelete(ctx, fmt.Sprintf("%s/api/items/%d", base, item.ID), nil); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

func clipboardDelete(ctx context.Context, url string, out interface{}) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, url, nil)
	if err != nil {
		return err
	}
	res, err := pasteClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("the clipboard page said %s", res.Status)
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(out)
}
