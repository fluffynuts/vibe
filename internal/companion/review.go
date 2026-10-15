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
		}
	}
	writeJSON(w, http.StatusOK, body)
}

// diffOf is the diff rv is of: a commit's, the folder's against a branch,
// or the folder as it is.
func diffOf(ctx context.Context, dir string, rv review.Review) ([]review.File, error) {
	switch {
	case rv.Commit != "":
		return review.Commit(ctx, dir, rv.Commit)
	case rv.Ref != "":
		return review.Against(ctx, dir, rv.Ref)
	}
	return review.WorkingTree(ctx, dir)
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
	if _, err := s.cfg.ReviewTab.Store.Do(review.Request{Op: "reply", Thread: req.Thread, Body: question}, review.User); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "asked, but the question couldn't be kept on the comment: " + err.Error()})
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]bool{"ok": true})
}

// askPrompt is the turn asking the agent question about thread t.
func askPrompt(t review.Thread, question string) string {
	where := "the whole review"
	if t.File != "" {
		lines := fmt.Sprintf("line %d", t.Line)
		if t.EndLine > t.Line {
			lines = fmt.Sprintf("lines %d-%d", t.Line, t.EndLine)
		}
		side := ""
		if t.Side == "old" {
			side = " (of the old file)"
		}
		where = t.File + ", " + lines + side
	}
	var said strings.Builder
	for _, c := range t.Comments {
		said.WriteString("- " + c.Author + ": " + strings.ReplaceAll(c.Body, "\n", "\n  ") + "\n")
	}
	return fmt.Sprintf("On the Review tab, I'm asking about review comment #%d, on %s. What's been said so far:\n\n%s\n"+
		"My question:\n\n%s\n\n"+
		"Answer me on that comment, not here:\n\n    vibe-review reply --thread %d --body-file - <<'BODY'\n    <your answer>\n    BODY\n\n"+
		"If what I say settles it, resolve it instead, saying how: vibe-review resolve --thread %d --body \"...\". "+
		"Keep to this comment, and don't change any code unless I ask you to.",
		t.ID, where, said.String(), question, t.ID, t.ID)
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
