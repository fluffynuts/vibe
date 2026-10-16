package companion

import (
	"context"
	"encoding/json"
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

// diffWithin bounds working out a diff: git on even a large repository is
// well within it.
const diffWithin = 30 * time.Second

// reviewState is the review, with what the agent has dropped since taken
// in; with ?diff=1, the diff too, worked out afresh. Polls leave the diff
// out: git on a large repository every few seconds would be costly.
func (s *Server) reviewState(w http.ResponseWriter, r *http.Request) {
	rv, err := s.cfg.ReviewTab.Store.Take()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
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
		files, err := diffOf(ctx, s.cfg.ReviewTab.Workspace, rv)
		if err != nil {
			body["diffError"] = err.Error()
		} else {
			if files == nil {
				files = []review.File{}
			}
			body["diff"] = files
			// Not while the agent is still reviewing: what it says next would go
			// into a closed review, out of sight. The next look after closes it.
			agentAtIt := rv.State == review.InProgress && s.convo.working()
			if len(files) == 0 && rv.Closed == "" && !agentAtIt && (!rv.Started.IsZero() || len(rv.Threads) > 0) {
				closed, err := s.cfg.ReviewTab.Store.Do(review.Request{Op: "close", Body: overBecause(rv)}, review.User)
				if err != nil {
					writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "the review has nothing left in it, but couldn't be closed: " + err.Error()})
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
func diffOf(ctx context.Context, dir string, rv review.Review) ([]review.File, error) {
	switch {
	case rv.Commit != "":
		return review.Commit(ctx, dir, rv.Commit)
	case rv.Unpushed:
		return review.Unpushed(ctx, dir, rv.Ref)
	case rv.Ref != "":
		return review.Against(ctx, dir, rv.Ref)
	}
	return review.WorkingTree(ctx, dir)
}

// reviewFile is a file's lines, for the tab to show more of its context
// than the diff has.
func (s *Server) reviewFile(w http.ResponseWriter, r *http.Request) {
	rv, err := s.cfg.ReviewTab.Store.Load()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), diffWithin)
	defer cancel()
	lines, err := review.FileLines(ctx, s.cfg.ReviewTab.Workspace, rv, r.URL.Query().Get("path"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"lines": lines})
}

// reviewDo is the user replying to, resolving, dismissing or reopening a
// thread.
func (s *Server) reviewDo(w http.ResponseWriter, r *http.Request) {
	var req review.Request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "expected a request"})
		return
	}
	rv, err := s.cfg.ReviewTab.Store.Do(req, review.User)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
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
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "expected {\"thread\": n, \"body\": ...}"})
		return
	}
	question := strings.TrimSpace(req.Body)
	if question == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "type what you want to ask first"})
		return
	}
	if s.cfg.Converse == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "this sandbox's agent can't be talked to from the page"})
		return
	}
	rv, err := s.cfg.ReviewTab.Store.Load()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	var thread *review.Thread
	for i := range rv.Threads {
		if rv.Threads[i].ID == req.Thread {
			thread = &rv.Threads[i]
		}
	}
	if thread == nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("there's no comment #%d", req.Thread)})
		return
	}
	if !s.startTurn(askPrompt(*thread, question)) {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "the agent is still answering the last prompt — ask once it's done"})
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
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "asked, but the question couldn't be kept on the comment: " + err.Error()})
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]bool{"ok": true})
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
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "this sandbox's agent can't be talked to from the page"})
		return
	}
	rv, err := s.cfg.ReviewTab.Store.Load()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	refs := review.Unsent(rv)
	if len(refs) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "there's nothing new to send"})
		return
	}
	if !s.startTurn(submitPrompt(rv, refs)) {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "the agent is still answering the last prompt — submit once it's done"})
		return
	}
	if err := s.cfg.ReviewTab.Store.MarkSent(refs); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "sent, but they couldn't be marked as sent, so Submit would send them again: " + err.Error()})
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]interface{}{"ok": true, "sent": len(refs)})
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
				mark := ""
				if fresh[id][i] {
					mark = " (new)"
				}
				b.WriteString("- " + c.Author + mark + ": " + strings.ReplaceAll(c.Body, "\n", "\n  ") + "\n")
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
		said.WriteString("- " + c.Author + ": " + strings.ReplaceAll(c.Body, "\n", "\n  ") + "\n")
	}
	return fmt.Sprintf("On the Review tab, I'm asking about review comment #%d, on %s. What's been said so far:\n\n%s\n"+
		"My question:\n\n%s\n\n"+
		"Answer me on that comment, not here:\n\n    vibe-review reply --thread %d --body-file - <<'BODY'\n    <your answer>\n    BODY\n\n"+
		"If what I say settles it, resolve it instead, saying how: vibe-review resolve --thread %d --body \"...\". "+
		"Keep to this comment, and don't change any code unless I ask you to.",
		t.ID, where(t), said.String(), question, t.ID, t.ID)
}

// reviewOptions is the reviews the page can start on the folder as it is.
func (s *Server) reviewOptions(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), diffWithin)
	defer cancel()
	o, err := review.Options(ctx, s.cfg.ReviewTab.Workspace)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
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
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": `expected {"kind": "uncommitted", "branch" or "unpushed", "agent": true or false}`})
		return
	}
	if req.Agent && s.cfg.Converse == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "this sandbox's agent can't be talked to from the page: show the diff instead"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), diffWithin)
	defer cancel()
	o, err := review.Options(ctx, s.cfg.ReviewTab.Workspace)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	start := review.Request{Op: "start", Reviewing: req.Agent}
	what := "the uncommitted changes"
	switch req.Kind {
	case "uncommitted":
		if !o.Uncommitted {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "there are no uncommitted changes to review"})
			return
		}
	case "branch":
		if o.DefaultBranch == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "there's no default branch to review against: none is main or master, and origin doesn't say"})
			return
		}
		start.Ref, what = o.DefaultBranch, "this branch's changes against "+o.DefaultBranch+", from where they parted (uncommitted ones too)"
	case "unpushed":
		if o.Upstream == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "there are no commits waiting to be pushed"})
			return
		}
		start.Ref, start.Unpushed, what = o.Upstream, true, "the commits not yet pushed to "+o.Upstream
	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("there's no review of kind %q: uncommitted, branch or unpushed", req.Kind)})
		return
	}
	// The review first, so the agent is never told of one that isn't
	// there; a busy agent then has the last one put back.
	last, err := s.cfg.ReviewTab.Store.Load()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	rv, err := s.cfg.ReviewTab.Store.Do(start, review.User)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if req.Agent && !s.startTurn(startPrompt(what)) {
		if err := s.cfg.ReviewTab.Store.Put(last); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "the agent is still answering the last prompt, and the last review couldn't be put back: " + err.Error()})
			return
		}
		writeJSON(w, http.StatusConflict, map[string]string{"error": "the agent is still answering the last prompt — start the review once it's done, or show the diff for now"})
		return
	}
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

// reviewing wraps the Review tab's calls: there's no such tab without a
// review to show; changes get the same protections as the other tabs'.
func (s *Server) reviewing(next http.HandlerFunc) http.HandlerFunc {
	return s.authed(func(w http.ResponseWriter, r *http.Request) {
		if s.cfg.ReviewTab == nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "this page has no review tab"})
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
