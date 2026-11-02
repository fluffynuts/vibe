package companion

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"runtime"
	"strings"
	"sync/atomic"
	"time"

	"vibe/internal/review"
)

// ReviewTab is what the Review tab works on: the diff of Workspace, the
// sandbox's folder as the host sees it, and the review kept in Store.
type ReviewTab struct {
	Workspace string
	Store     *review.Store
}

// reviewTakeEvery is how often vibe takes in the agent's requests itself,
// page or not: vibe-review's start waits for its review to be taken in.
var reviewTakeEvery = 2 * time.Second

// takeInbox takes in the agent's requests every so often, until done is
// closed. One that fails isn't lost: it's taken in again the next time.
// Failing is said in vibe's own output (once, until it works again), as no
// page may be open to say it.
func (s *Server) takeInbox(done <-chan struct{}, every time.Duration) {
	tick := time.NewTicker(every)
	defer tick.Stop()
	failing := ""
	for {
		select {
		case <-done:
			return
		case <-tick.C:
			_, err := s.cfg.ReviewTab.Store.Take()
			switch {
			case err == nil:
				failing = ""
			case err.Error() != failing && s.cfg.Log != nil:
				failing = err.Error()
				s.cfg.Log("the agent's review requests can't be taken in: " + failing)
			}
		}
	}
}

// diffWithin bounds working out a diff: git on even a large repository is
// well within it.
const diffWithin = 30 * time.Second

// colourWithin bounds colouring a file, past which it's shown without:
// Chroma's lexers can be made to take without end. A variable for tests.
var colourWithin = 15 * time.Second

// branchWords says a branch as the page does: its name, or a detached HEAD.
func branchWords(branch string) string {
	if branch == "" {
		return "a detached HEAD"
	}
	return "'" + branch + "'"
}

// reviewQuiet is how long a review in progress may go untouched by the agent, with no turn running, before the tab says so: the agent
// may have stopped, or be writing from a terminal session, which is no turn of the page's to see. Vibe can't tell those apart, so
// nothing is dropped for it. A variable for tests.
var reviewQuiet = 3 * time.Minute

// branchLookEvery is how often at most the branch is looked up by the Review tab's polls (a lookup starts the helper process); asking
// for the diff always looks.
var branchLookEvery = 5 * time.Second

// reviewQuietSince is how long the agent has left the review alone: since it last touched it, or since it began.
func (s *Server) reviewQuietSince(rv review.Review) time.Duration {
	last := rv.AgentAt
	if last.IsZero() || rv.Started.After(last) {
		last = rv.Started
	}
	return s.clock().Sub(last)
}

// reviewQuietMinutes is, for a review in progress that no turn of the conversation's agent is running and the agent has left alone for
// more than reviewQuiet, the whole minutes it has been quiet; else 0. It is no claim that anything stopped: a review the agent
// starts itself, in an ordinary turn, or writes from a terminal, is written by something vibe doesn't see (see agentAtWorkOnReview).
func (s *Server) reviewQuietMinutes(rv review.Review) int {
	if rv.State != review.InProgress || s.convo.working() {
		return 0
	}
	if quiet := s.reviewQuietSince(rv); quiet > reviewQuiet {
		return int(quiet / time.Minute)
	}
	return 0
}

// reviewWriting says a turn is running that is writing the review in progress: one vibe began for it, or one running while the agent
// has touched it lately (not a turn for something else while an old review sits there).
func (s *Server) reviewWriting(rv review.Review) bool {
	return rv.State == review.InProgress && s.convo.working() && (s.agentAtWorkOnReview() || s.reviewQuietSince(rv) <= reviewQuiet)
}

// currentBranch is the branch the review's folder is on, looked up at most every branchLookEvery unless force. The lock is held only
// to read and write what was last seen, never across the lookup (a git on a stalled disk can hang for diffWithin): polls that come
// while one is under way share it, and each stops waiting when its own ctx ends. The shared lookup is bounded by the server's life and
// diffWithin, not by whichever poll began it, so that poll giving up doesn't fail the others. force looks for itself, joining none.
func (s *Server) currentBranch(ctx context.Context, force bool) (string, error) {
	seen := &s.branchSeen
	seen.mu.Lock()
	if !force && seen.ok && s.clock().Sub(seen.at) < branchLookEvery {
		branch := seen.branch
		seen.mu.Unlock()
		return branch, nil
	}
	if force {
		seen.mu.Unlock()
		return s.lookBranch(ctx)
	}
	flight := seen.flight
	if flight == nil {
		flight = &branchFlight{done: make(chan struct{})}
		seen.flight = flight
		go func() {
			lookCtx, cancel := context.WithTimeout(s.lifeCtx, diffWithin)
			defer cancel()
			flight.branch, flight.err = s.lookBranch(lookCtx)
			seen.mu.Lock()
			seen.flight = nil
			seen.mu.Unlock()
			close(flight.done)
		}()
	}
	seen.mu.Unlock()
	select {
	case <-flight.done:
		return flight.branch, flight.err
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

// lookBranch asks for the branch and remembers the answer and when it was had.
func (s *Server) lookBranch(ctx context.Context) (string, error) {
	branch, err := s.branchLookup(ctx, s.cfg.ReviewTab.Workspace)
	if err != nil {
		return "", err
	}
	seen := &s.branchSeen
	seen.mu.Lock()
	seen.at, seen.branch, seen.ok = s.clock(), branch, true
	seen.mu.Unlock()
	return branch, nil
}

// watchBranch ties the review to the branch the folder is on: the first time it is looked at, if it wasn't noted when it was
// started, and from then on the review closes itself when the folder is on another (a review of a feature is confusing once the
// folder is back on master, after it is released). It is kept, to be reopened. Not while a turn is running, as with a review that
// has nothing left in it (CloseOver): what the agent says next would go into a closed review, out of sight; the next look after
// closes it. A branch that can't be read leaves it as it is. force looks the branch up whatever the last look was.
func (s *Server) watchBranch(ctx context.Context, rv review.Review, force bool) review.Review {
	if rv.Started.IsZero() || rv.Closed != "" {
		return rv
	}
	store := s.cfg.ReviewTab.Store
	now, err := s.currentBranch(ctx, force)
	if err != nil {
		return rv
	}
	if !rv.BranchKnown {
		if noted, err := store.SetBranch(rv.Started, now); err == nil {
			return noted
		}
		return rv
	}
	if now == rv.Branch || s.convo.working() {
		return rv
	}
	why := "The last review was closed when the branch for this workspace switched to " + branchWords(now) + ". The review was of " +
		branchWords(rv.Branch) + ". It is kept: reopen it whenever you like."
	if closed, _, err := store.CloseOnBranch(rv.Started, now, why); err == nil {
		return closed
	}
	return rv
}

// noteBranch notes the branch the folder is on for the review just started (or reopened); one that can't be read is left for the
// first look to note.
func (s *Server) noteBranch(ctx context.Context, rv review.Review) review.Review {
	now, err := s.currentBranch(ctx, true)
	if err != nil {
		return rv
	}
	if noted, err := s.cfg.ReviewTab.Store.SetBranch(rv.Started, now); err == nil {
		return noted
	}
	return rv
}

// reviewState is the review, with what the agent has dropped since taken
// in; with ?diff=1, the diff too, worked out afresh (without whitespace
// changes, with &whitespace=ignore). Polls leave the diff
// out: git on a large repository every few seconds would be costly.
func (s *Server) reviewState(w http.ResponseWriter, r *http.Request) {
	rv, err := s.cfg.ReviewTab.Store.Take()
	if err != nil {
		writeStoreError(w, err)
		return
	}
	{
		ctx, cancel := context.WithTimeout(r.Context(), diffWithin)
		rv = s.watchBranch(ctx, rv, r.URL.Query().Get("diff") == "1")
		cancel()
	}
	body := map[string]interface{}{"review": rv, "agentAtWork": s.agentAtWorkOnReview(), "quietMinutes": s.reviewQuietMinutes(rv)}
	if ask := s.answeringAsk(); ask != nil {
		body["ask"] = ask
	}
	if n, ok := s.pastesWaiting(); ok {
		body["pastes"] = n
	}
	if r.URL.Query().Get("diff") == "1" {
		ctx, cancel := context.WithTimeout(r.Context(), diffWithin)
		defer cancel()
		folder := s.cfg.ReviewTab.Workspace
		how := review.Diffing{IgnoreSpace: r.URL.Query().Get("whitespace") == "ignore"}
		files, err := diffOf(ctx, folder, rv, how)
		if err != nil {
			body["diffError"] = err.Error()
		} else {
			if files == nil {
				files = []review.File{}
			}
			body["diff"] = files
			// Comments follow their code when lines above them came or went.
			if moves := review.Moves(ctx, folder, rv); len(moves) > 0 {
				if err := s.cfg.ReviewTab.Store.Move(moves); err != nil {
					writeError(w, http.StatusInternalServerError, "comments couldn't be moved to where their code is now: "+err.Error())
					return
				}
				if rv, err = s.cfg.ReviewTab.Store.Load(); err != nil {
					writeError(w, http.StatusInternalServerError, err.Error())
					return
				}
				body["review"] = rv
			}
			// Not while the agent is at work (reviewing, answering a Submit
			// or an Ask): what it says next would go into a closed review, out
			// of sight. The next look after closes it. Nor one the user
			// reopened, nor one started while the diff was worked out
			// (CloseOver).
			// Shown without whitespace changes, a diff can be empty with
			// changes still there: whether it's over is the whole diff's to say.
			over := len(files) == 0
			if over && how.IgnoreSpace {
				all, err := diffOf(ctx, folder, rv, review.Diffing{})
				over = err == nil && len(all) == 0
			}
			if over && rv.Closed == "" && !s.convo.working() && (!rv.Started.IsZero() || len(rv.Threads) > 0) {
				closed, _, err := s.cfg.ReviewTab.Store.CloseOver(rv.Started, overBecause(rv))
				if err != nil {
					writeError(w, http.StatusInternalServerError, "the review has nothing left in it, but couldn't be closed: "+err.Error())
					return
				}
				body["review"] = closed
			}
		}
	}
	writeJSON(w, http.StatusOK, body)
}

// answeringAsk is the thread the user last asked the agent about, and
// whether it's still answering, while that Ask is the latest prompt; else nil.
func (s *Server) answeringAsk() map[string]interface{} {
	s.asked.mu.Lock()
	thread, asked := s.asked.thread, s.asked.prompt
	s.asked.mu.Unlock()
	prompt, answering := s.convo.turn()
	if thread == 0 || prompt != asked {
		return nil
	}
	return map[string]interface{}{"thread": thread, "answering": answering}
}

// overBecause is why review rv, its diff now empty, closed by itself, and
// how many of its comments were still open.
func overBecause(rv review.Review) string {
	why := "Closed by itself: its changes have all been committed (or put away)."
	switch {
	case rv.Commit != "":
		why = "Closed by itself: the commit changes nothing."
	case rv.Unpushed:
		why = "Closed by itself: its commits have all been pushed to " + rv.Ref + "."
	case rv.Ref != "":
		why = "Closed by itself: there's nothing here " + rv.Ref + " hasn't got (merged?)."
	}
	open := 0
	for _, t := range rv.Threads {
		if t.Status == review.Open {
			open++
		}
	}
	switch {
	case open == 1:
		why += " 1 comment was still open."
	case open > 1:
		why += fmt.Sprintf(" %d comments were still open.", open)
	}
	return why
}

// diffOf is the diff rv is of: a commit's, the commits not yet pushed to
// its upstream, the folder's against a branch, or the folder as it is.
func diffOf(ctx context.Context, dir string, rv review.Review, how review.Diffing) ([]review.File, error) {
	switch {
	case rv.Commit != "":
		return review.Commit(ctx, dir, rv.Commit, how)
	case rv.Unpushed:
		return review.Unpushed(ctx, dir, rv.Ref, how)
	case rv.Ref != "":
		return review.Against(ctx, dir, rv.Ref, how)
	}
	return review.WorkingTree(ctx, dir, how)
}

// reviewFile is a file's lines, for the tab to show more of its context
// than the diff has.
func (s *Server) reviewFile(w http.ResponseWriter, r *http.Request) {
	rv, err := s.cfg.ReviewTab.Store.Load()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), diffWithin)
	defer cancel()
	lines, err := review.FileLines(ctx, s.cfg.ReviewTab.Workspace, rv, r.URL.Query().Get("path"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"lines": lines})
}

// reviewDo is the user replying to, resolving, dismissing or reopening a
// thread.
func (s *Server) reviewDo(w http.ResponseWriter, r *http.Request) {
	var req review.Request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "expected a request")
		return
	}
	rv, err := s.cfg.ReviewTab.Store.Do(req, review.User)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.As(err, &review.Refused{}) {
			status = http.StatusBadRequest
		}
		writeError(w, status, err.Error())
		return
	}
	if req.Op == "reopen-review" {
		// Tied to the branch the folder is on now, which is the one it closes itself for leaving; if that can't be read now, the first look does it.
		ctx, cancel := context.WithTimeout(r.Context(), diffWithin)
		rv = s.noteBranch(ctx, rv)
		cancel()
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"review": rv})
}

// reviewAsk is the user asking the agent about a thread: a turn, telling
// the agent the thread and how to answer on it. The question is kept as the
// user's reply once the turn has begun; refused, nothing is kept.
func (s *Server) reviewAsk(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Thread int    `json:"thread"`
		Body   string `json:"body"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "expected {\"thread\": n, \"body\": ...}")
		return
	}
	question := strings.TrimSpace(req.Body)
	if question == "" {
		writeError(w, http.StatusBadRequest, "type what you want to ask first")
		return
	}
	if s.cfg.Converse == nil {
		writeError(w, http.StatusNotFound, "this sandbox's agent can't be talked to from the page")
		return
	}
	rv, err := s.cfg.ReviewTab.Store.Load()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	var thread *review.Thread
	for i := range rv.Threads {
		if rv.Threads[i].ID == req.Thread {
			thread = &rv.Threads[i]
		}
	}
	if thread == nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("there's no comment #%d", req.Thread))
		return
	}
	launch, _, ok := s.claimTurn(askPrompt(*thread, question))
	if !ok {
		writeError(w, http.StatusConflict, "the agent is still answering the last prompt — ask once it's done")
		return
	}
	prompt := s.reviewBegan()
	launch()
	s.asked.mu.Lock()
	s.asked.thread, s.asked.prompt = req.Thread, prompt
	s.asked.mu.Unlock()
	after, err := s.cfg.ReviewTab.Store.Do(review.Request{Op: "reply", Thread: req.Thread, Body: question}, review.User)
	if err == nil {
		// The agent is given the whole thread: Submit to agent mustn't send
		// any of it again.
		err = s.cfg.ReviewTab.Store.MarkSent(unsentOn(after, req.Thread))
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "asked, but the question couldn't be kept on the comment: "+err.Error())
		return
	}
	// The review as it now is, the question on it: the page takes it, so
	// a poll that set out before can't take it away.
	now, err := s.cfg.ReviewTab.Store.Load()
	if err != nil {
		writeJSON(w, http.StatusAccepted, map[string]interface{}{"ok": true})
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]interface{}{"ok": true, "review": now})
}

// unsentOn is the user's unsent comments on thread id.
func unsentOn(rv review.Review, id int) []review.Ref {
	var out []review.Ref
	for _, ref := range review.Unsent(rv) {
		if ref.Thread == id {
			out = append(out, ref)
		}
	}
	return out
}

// reviewSubmit sends the agent the user's comments it hasn't been given, as
// a turn; they're sent once it has begun. Refused, they stay unsent.
func (s *Server) reviewSubmit(w http.ResponseWriter, r *http.Request) {
	if s.cfg.Converse == nil {
		writeError(w, http.StatusNotFound, "this sandbox's agent can't be talked to from the page")
		return
	}
	rv, err := s.cfg.ReviewTab.Store.Load()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	refs := review.Unsent(rv)
	if len(refs) == 0 {
		writeError(w, http.StatusBadRequest, "there's nothing new to send")
		return
	}
	launch, _, ok := s.claimTurn(submitPrompt(rv, refs))
	if !ok {
		writeError(w, http.StatusConflict, "the agent is still answering the last prompt — submit once it's done")
		return
	}
	s.reviewBegan()
	launch()
	if err := s.cfg.ReviewTab.Store.MarkSent(refs); err != nil {
		writeError(w, http.StatusInternalServerError, "sent, but they couldn't be marked as sent, so Submit would send them again: "+err.Error())
		return
	}
	body := map[string]interface{}{"ok": true, "sent": len(refs)}
	if now, err := s.cfg.ReviewTab.Store.Load(); err == nil {
		body["review"] = now
	}
	writeJSON(w, http.StatusAccepted, body)
}

// submitPrompt is the turn giving the agent the user's new comments
// (refs): each thread they're on, where it is, the code it's about, and
// everything said on it, the new marked as new.
func submitPrompt(rv review.Review, refs []review.Ref) string {
	fresh := map[int]map[int]bool{}
	var order []int
	for _, ref := range refs {
		if fresh[ref.Thread] == nil {
			fresh[ref.Thread] = map[int]bool{}
			order = append(order, ref.Thread)
		}
		fresh[ref.Thread][ref.Comment] = true
	}
	var b strings.Builder
	b.WriteString("On the Review tab, I've left review comments for you. Here's each comment thread with something new of mine on it:\n")
	for _, id := range order {
		for _, t := range rv.Threads {
			if t.ID != id {
				continue
			}
			b.WriteString(fmt.Sprintf("\n## #%d, on %s\n", t.ID, where(t)))
			if t.Code != "" {
				b.WriteString("\nThe code it's on:\n\n" + indent(t.Code) + "\n")
			}
			b.WriteString("\n")
			for i, c := range t.Comments {
				b.WriteString(saidLine(c, fresh[id][i]))
			}
		}
	}
	b.WriteString("\nWork through each new comment of mine, in turn. Change code only where a comment asks you to (\"please fix\", say): " +
		"then make the change and resolve its thread, saying what you did: vibe-review resolve --thread <n> --body-file - <<'BODY' ... BODY. " +
		"Where a comment asks a question, or tells you not to change code, answer it and change nothing: " +
		"vibe-review reply --thread <n> --body-file - <<'BODY' ... BODY. If you disagree, or need more from me, reply and leave the thread open. " +
		"Answer on the threads, not here; here, just say briefly what you did.")
	return b.String()
}

// saidLine is comment c as a line of a thread for the agent, marked new
// when fresh: edited since the agent saw it, with what it said then.
func saidLine(c review.Comment, fresh bool) string {
	text := func(s string) string { return strings.ReplaceAll(s, "\n", "\n  ") }
	switch {
	case fresh && len(c.Was) > 0:
		return "- " + c.Author + " (edited since you saw it): " + text(c.Body) + "\n  It said: " + text(c.Was[len(c.Was)-1]) + "\n"
	case fresh:
		return "- " + c.Author + " (new): " + text(c.Body) + "\n"
	}
	return "- " + c.Author + ": " + text(c.Body) + "\n"
}

// where is where thread t is, in words.
func where(t review.Thread) string {
	if t.File == "" {
		return "the whole review"
	}
	lines := fmt.Sprintf("line %d", t.Line)
	if t.EndLine > t.Line {
		lines = fmt.Sprintf("lines %d-%d", t.Line, t.EndLine)
	}
	side := ""
	if t.Side == "old" {
		side = " (of the old file)"
	}
	return t.File + ", " + lines + side
}

func indent(text string) string {
	return "    " + strings.ReplaceAll(text, "\n", "\n    ")
}

// askPrompt is the turn asking the agent question about thread t.
func askPrompt(t review.Thread, question string) string {
	var said strings.Builder
	for _, c := range t.Comments {
		said.WriteString(saidLine(c, c.Author == review.User && !c.Sent))
	}
	return fmt.Sprintf("On the Review tab, I'm asking about review comment #%d, on %s. What's been said so far:\n\n%s\n"+
		"My question:\n\n%s\n\n"+
		"Answer me on that comment, not here:\n\n    vibe-review reply --thread %d --body-file - <<'BODY'\n    <your answer>\n    BODY\n\n"+
		"If what I say settles it, resolve it instead, saying how: vibe-review resolve --thread %d --body \"...\". "+
		"Keep to this comment, and don't change any code unless I ask you to.",
		t.ID, where(t), said.String(), question, t.ID, t.ID)
}

// reviewHighlight is a file coloured, on one side of the review's diff
// (?side=new or old): the whole file, so what spans lines is coloured on
// each.
func (s *Server) reviewHighlight(w http.ResponseWriter, r *http.Request) {
	rv, err := s.cfg.ReviewTab.Store.Load()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	path := r.URL.Query().Get("path")
	if why := s.colours.off(); why != "" {
		writeJSON(w, http.StatusOK, review.Highlighted{Skipped: why})
		return
	}
	select {
	case s.colours.slots <- struct{}{}:
		defer func() { <-s.colours.slots }()
	case <-r.Context().Done():
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), colourWithin)
	defer cancel()
	h, err := review.Colour(ctx, s.cfg.ReviewTab.Workspace, rv, path, r.URL.Query().Get("side") == "old")
	switch {
	case ctx.Err() == context.DeadlineExceeded:
		s.colours.tooLong.Add(1)
		writeJSON(w, http.StatusOK, review.Highlighted{Skipped: fmt.Sprintf("took too long to colour (over %s)", colourWithin)})
	case err != nil:
		writeError(w, http.StatusBadRequest, err.Error())
	default:
		writeJSON(w, http.StatusOK, h)
	}
}

// colourAtOnce is how many files are coloured at a time, each in a helper
// that may take a core for all of colourWithin: half the machine's cores, so
// the rest are left for everything else.
var colourAtOnce = halfOf(runtime.NumCPU())

// halfOf is half of cpus, and at least one.
func halfOf(cpus int) int { return max(1, cpus/2) }

// colourGiveUpAfter is how many files taking too long to colour turn
// colouring off: a file can be built to take without end, and the sandbox
// writes them.
const colourGiveUpAfter = 5

// colourer bounds the colouring of files.
type colourer struct {
	slots   chan struct{}
	tooLong atomic.Int32
}

func newColourer(atOnce int) *colourer { return &colourer{slots: make(chan struct{}, atOnce)} }

// off is why colouring is turned off, or "" while it isn't.
func (c *colourer) off() string {
	if n := c.tooLong.Load(); n >= colourGiveUpAfter {
		return fmt.Sprintf("colouring is turned off until vibe restarts: %d files took too long to colour, which a file can be built to do", n)
	}
	return ""
}

// reviewOptions is the reviews the page can start on the folder as it is.
func (s *Server) reviewOptions(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), diffWithin)
	defer cancel()
	o, err := review.Options(ctx, s.cfg.ReviewTab.Workspace)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, o)
}

// reviewStart starts a review from the page, of what kind says: the
// uncommitted changes, this branch against the default one, or the commits
// not yet pushed; the agent asked to review it (a turn), or just the diff,
// shown at once either way.
func (s *Server) reviewStart(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Kind  string `json:"kind"`
		Agent bool   `json:"agent"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, `expected {"kind": "uncommitted", "branch" or "unpushed", "agent": true or false}`)
		return
	}
	if req.Agent && s.cfg.Converse == nil {
		writeError(w, http.StatusNotFound, "this sandbox's agent can't be talked to from the page: show the diff instead")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), diffWithin)
	defer cancel()
	o, err := review.Options(ctx, s.cfg.ReviewTab.Workspace)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	start := review.Request{Op: "start", Reviewing: req.Agent}
	what := "the uncommitted changes"
	switch req.Kind {
	case "uncommitted":
		if !o.Uncommitted {
			writeError(w, http.StatusBadRequest, "there are no uncommitted changes to review")
			return
		}
	case "branch":
		if o.DefaultBranch == "" {
			why := o.NoDefaultBranch
			if why == "" {
				why = "You're on the default branch: there's nothing to review against it."
			}
			writeError(w, http.StatusBadRequest, why)
			return
		}
		start.Ref, what = o.DefaultBranch, "this branch's changes against "+o.DefaultBranch+", from where they parted (uncommitted ones too)"
	case "unpushed":
		if o.Upstream == "" {
			writeError(w, http.StatusBadRequest, "there are no commits waiting to be pushed")
			return
		}
		start.Ref, start.Unpushed, what = o.Upstream, true, "the commits not yet pushed to "+o.Upstream
	default:
		writeError(w, http.StatusBadRequest, fmt.Sprintf("there's no review of kind %q: uncommitted, branch or unpushed", req.Kind))
		return
	}
	// A review that can't be read refuses before the agent's turn is claimed
	// (claiming ends what the agent left running).
	if _, err := s.cfg.ReviewTab.Store.Load(); err != nil {
		writeStoreError(w, err)
		return
	}
	// The agent's turn first, then the review, then the agent is given it:
	// a busy agent leaves the review as it was, and the agent never hears of
	// one that isn't there.
	launch, abandon := func() {}, func(error) {}
	if req.Agent {
		var ok bool
		if launch, abandon, ok = s.claimTurn(startPrompt(what)); !ok {
			writeError(w, http.StatusConflict, "the agent is still answering the last prompt — start the review once it's done, or show the diff for now")
			return
		}
		s.reviewBegan()
	}
	rv, err := s.cfg.ReviewTab.Store.Do(start, review.User)
	if err != nil {
		abandon(fmt.Errorf("the review couldn't be started, so this wasn't given to the agent: %w", err))
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	rv = s.noteBranch(ctx, rv)
	launch()
	status := http.StatusOK
	if req.Agent {
		status = http.StatusAccepted
	}
	writeJSON(w, status, map[string]interface{}{"review": rv})
}

// startPrompt is the turn asking the agent to review what, a review the
// page has started.
func startPrompt(what string) string {
	return "On the Review tab, I've started a review of " + what + ": please review it with your vibe-review skill. " +
		"It's already started, so don't run vibe-review start (that would replace it): begin at the skill's step 2, " +
		"with vibe-review diff, and finish with vibe-review done. Comment on the tab, not here; here, just say briefly what you found."
}

// reviewAfresh sets aside a review that can't be read, keeping it in
// history/, and begins an empty one (see review.Store.StartAfresh).
func (s *Server) reviewAfresh(w http.ResponseWriter, r *http.Request) {
	rv, err := s.cfg.ReviewTab.Store.StartAfresh()
	if err != nil {
		status := http.StatusInternalServerError
		if errors.As(err, &review.Refused{}) {
			status = http.StatusBadRequest
		}
		writeError(w, status, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"review": rv})
}

// writeStoreError answers with the store's failure: one whose review can't
// be read for what's in it says so (code review-unreadable), so the page
// can offer to start afresh.
func writeStoreError(w http.ResponseWriter, err error) {
	body := map[string]string{"error": err.Error()}
	if errors.As(err, &review.Unreadable{}) {
		body["code"] = "review-unreadable"
	}
	writeJSON(w, http.StatusInternalServerError, body)
}

// reviewBegan notes the turn just claimed as the review's, and gives its
// prompt.
func (s *Server) reviewBegan() int {
	prompt, _ := s.convo.turn()
	s.reviewTurn.mu.Lock()
	s.reviewTurn.prompt = prompt
	s.reviewTurn.mu.Unlock()
	return prompt
}

// agentAtWorkOnReview says the agent is at a turn the review began:
// answering it, or still at work on what it left running.
func (s *Server) agentAtWorkOnReview() bool {
	s.reviewTurn.mu.Lock()
	began := s.reviewTurn.prompt
	s.reviewTurn.mu.Unlock()
	prompt, _ := s.convo.turn()
	return began != 0 && prompt == began && s.convo.working()
}

// stopAndDropReview stops the agent's turn and drops the review it was writing, saying why: the one place both closing the review
// and stopping the agent do it, since a review half written is never kept (it would only be started again). It says what really
// happened: stopped is whether a turn was found to stop, and the review is dropped whenever it is still in progress after the stop
// (the turn may have ended already without finishing it); one that was finished in the meantime is left as it is.
func (s *Server) stopAndDropReview(why string) (rv review.Review, stopped, dropped bool, err error) {
	store := s.cfg.ReviewTab.Store
	stopped = s.convo.stop()
	if rv, err = store.Load(); err != nil || rv.State != review.InProgress {
		return rv, stopped, false, err
	}
	rv, err = store.Drop(why)
	return rv, stopped, err == nil, err
}

// reviewClose closes the review. The agent at work on it is stopped, when
// the user has agreed to (stop), as the page warned them: while it's still
// writing the review, which is then dropped (it would only be started
// again), or while comments are still open (fixing them, say), the review
// kept to reopen. With nothing left open, the agent is left to finish. A
// turn the review didn't begin is never stopped.
func (s *Server) reviewClose(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Stop bool `json:"stop"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, `expected {"stop": true or false}`)
		return
	}
	store := s.cfg.ReviewTab.Store
	rv, err := store.Load()
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writing := rv.State == review.InProgress
	open := false
	for _, t := range rv.Threads {
		open = open || t.Status == review.Open
	}
	stopping := (s.agentAtWorkOnReview() || s.reviewWriting(rv)) && (writing || open)
	if stopping && !req.Stop {
		writeError(w, http.StatusConflict, "the agent is at work on this review, and closing it now stops the agent: it was left open")
		return
	}
	dropped := false
	if writing && stopping {
		rv, _, dropped, err = s.stopAndDropReview("You closed it while the agent was still writing it, so it was dropped: start another whenever you like.")
	} else if writing {
		// In progress with nothing vibe can see running (the agent crashed, the sandbox stopped, it forgot `done`, it is writing from a
		// terminal, or its turn ended since the page warned): asked first, like one being written, and dropped only when confirmed.
		if !req.Stop {
			writeError(w, http.StatusConflict, "this review is still in progress, and closing it drops it: it was left open")
			return
		}
		rv, err = store.Drop("You closed it before the agent had finished it, so it was dropped: start another whenever you like.")
		dropped = err == nil
	}
	if err == nil && !dropped {
		// Closed, and kept: finished, or open comments the agent was fixing (which is stopped), or finished while being closed.
		if stopping && !writing {
			s.convo.stop()
		}
		why := "You closed it."
		if stopping && !writing {
			why = "You closed it, stopping the agent's work on it."
		}
		rv, err = store.Do(review.Request{Op: "close", Body: why}, review.User)
	}
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"review": rv})
}
