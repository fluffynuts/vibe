package companion

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
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
	body := map[string]interface{}{"review": rv}
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
	if !s.startTurn(askPrompt(*thread, question)) {
		writeError(w, http.StatusConflict, "the agent is still answering the last prompt — ask once it's done")
		return
	}
	prompt, _ := s.convo.turn()
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
	if !s.startTurn(submitPrompt(rv, refs)) {
		writeError(w, http.StatusConflict, "the agent is still answering the last prompt — submit once it's done")
		return
	}
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
	ctx, cancel := context.WithTimeout(r.Context(), diffWithin)
	defer cancel()
	path := r.URL.Query().Get("path")
	read := review.NewTextOf
	if r.URL.Query().Get("side") == "old" {
		read = review.OldTextOf
	}
	text, err := read(ctx, s.cfg.ReviewTab.Workspace, rv, path)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, review.Highlight(path, text))
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
	}
	rv, err := s.cfg.ReviewTab.Store.Do(start, review.User)
	if err != nil {
		abandon(fmt.Errorf("the review couldn't be started, so this wasn't given to the agent: %w", err))
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
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

// reviewing wraps the Review tab's calls: there's no such tab without a
// review to show; changes get the same protections as the other tabs'.
func (s *Server) reviewing(next http.HandlerFunc) http.HandlerFunc {
	return s.authed(func(w http.ResponseWriter, r *http.Request) {
		if s.cfg.ReviewTab == nil {
			writeError(w, http.StatusNotFound, "this page has no review tab")
			return
		}
		if r.Method == http.MethodPost {
			if !sameOriginJSON(w, r) {
				return
			}
			r.Body = http.MaxBytesReader(w, r.Body, maxPrompt)
		}
		next(w, r)
	})
}
