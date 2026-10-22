package companion

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// fakeAuthor is the host's git author question, as far as the page sees it.
type fakeAuthor struct {
	state   GitAuthorState
	answers []string
	err     error
	kept    string
}

func (f *fakeAuthor) config() *GitAuthor {
	return &GitAuthor{
		State:  func() GitAuthorState { return f.state },
		Decide: func(answer string) (string, error) { f.answers = append(f.answers, answer); return f.kept, f.err },
	}
}

func authorServer(f *fakeAuthor) *Server {
	s := newServer(&fakeSbx{})
	s.cfg.GitAuthor = f.config()
	return s
}

func pageState(t *testing.T, s *Server) map[string]interface{} {
	t.Helper()
	rec := do(t, s, req{path: "/api/state", headers: authed(nil)})
	var out map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestTheStateCarriesTheQuestionWhileThereIsOne(t *testing.T) {
	f := &fakeAuthor{state: GitAuthorState{Phase: "ask", Shown: "H <h@x>", Push: "It can't push."}}
	got := pageState(t, authorServer(f))["gitAuthor"]
	want := map[string]interface{}{"phase": "ask", "shown": "H <h@x>", "push": "It can't push."}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("gitAuthor = %v, want %v", got, want)
	}
}

func TestTheStateSaysNothingOfItWhenNothingIsAsked(t *testing.T) {
	if got, there := pageState(t, authorServer(&fakeAuthor{}))["gitAuthor"]; there && fmt.Sprint(got) != "map[]" {
		t.Errorf("gitAuthor = %v", got)
	}
	if got, there := pageState(t, newServer(&fakeSbx{}))["gitAuthor"]; there && fmt.Sprint(got) != "map[]" {
		t.Errorf("a server with no such question: gitAuthor = %v", got)
	}
}

func answer(t *testing.T, s *Server, body string) (int, string) {
	t.Helper()
	rec := do(t, s, post("/api/git-author", body))
	return rec.Code, rec.Body.String()
}

func TestAnAnswerIsPassedOn(t *testing.T) {
	for _, a := range []string{"give", "leave", "later", "fix", "dismiss"} {
		f := &fakeAuthor{}
		if code, body := answer(t, authorServer(f), `{"answer":"`+a+`"}`); code != http.StatusOK || strings.Join(f.answers, ",") != a {
			t.Errorf("%s: %d %s, answers %v", a, code, body, f.answers)
		}
	}
}

func TestAnAnswerThatIsNotOneOfTheFiveIsRefusedWithoutAskingTheHost(t *testing.T) {
	for _, body := range []string{`{"answer":"yes"}`, `{"answer":""}`, `{}`, `not json`} {
		f := &fakeAuthor{}
		if code, _ := answer(t, authorServer(f), body); code != http.StatusBadRequest || len(f.answers) != 0 {
			t.Errorf("%s: %d, answers %v", body, code, f.answers)
		}
	}
}

func TestAnAnswerThatMakesNoSenseNowIsAConflictAndOneThatFailsIsAnError(t *testing.T) {
	f := &fakeAuthor{err: fmt.Errorf("%w: it was answered already", ErrAnswerNotNow)}
	code, body := answer(t, authorServer(f), `{"answer":"give"}`)
	if code != http.StatusConflict || !strings.Contains(body, "answered already") {
		t.Errorf("%d %s", code, body)
	}
	f = &fakeAuthor{err: errors.New("no room")}
	code, body = answer(t, authorServer(f), `{"answer":"give"}`)
	if code != http.StatusInternalServerError || !strings.Contains(body, "no room") {
		t.Errorf("%d %s", code, body)
	}
}

func TestWithNoSuchQuestionThereIsNothingToAnswer(t *testing.T) {
	if code, _ := answer(t, newServer(&fakeSbx{}), `{"answer":"give"}`); code != http.StatusNotFound {
		t.Errorf("%d", code)
	}
}

func TestWhatTheHostKeptIsSaidInTheAnswer(t *testing.T) {
	f := &fakeAuthor{kept: "kept: delete the file to be asked again"}
	rec := do(t, authorServer(f), post("/api/git-author", `{"answer":"give"}`))
	var got struct{ Kept string }
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &got) != nil || got.Kept != f.kept {
		t.Errorf("%d %s", rec.Code, rec.Body)
	}
	rec = do(t, authorServer(&fakeAuthor{}), post("/api/git-author", `{"answer":"later"}`))
	if strings.Contains(rec.Body.String(), "kept") && !strings.Contains(rec.Body.String(), `"kept":""`) {
		t.Errorf("nothing was kept, yet: %s", rec.Body)
	}
}
