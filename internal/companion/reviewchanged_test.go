package companion

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"vibe/internal/review"
)

// The page's diff is a snapshot: it is worked out when the tab is first shown and on Refresh, and the agent, answering a round of
// review, can change the code after. The page asks whether it has, once the agent's turn is over, with the key of the diff it
// holds: read-only, so that asking moves no comment and closes no review (a diff request does both).

type changedResult struct {
	Changed bool   `json:"changed"`
	Key     string `json:"key"`
	Error   string `json:"error"`
}

func getChanged(t *testing.T, s *Server, query string) (changedResult, int) {
	t.Helper()
	rec := do(t, s, req{path: "/api/review/changed?" + query, headers: authed(nil)})
	var r changedResult
	if rec.Code == http.StatusOK {
		if err := json.Unmarshal(rec.Body.Bytes(), &r); err != nil {
			t.Fatal(err)
		}
	}
	return r, rec.Code
}

func diffKeyOf(t *testing.T, s *Server, extra string) string {
	t.Helper()
	body := do(t, s, req{path: "/api/review?diff=1" + extra, headers: authed(nil)}).Body.Bytes()
	var r struct {
		DiffKey string `json:"diffKey"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		t.Fatal(err)
	}
	return r.DiffKey
}

func workspaceOf(s *Server) string { return s.cfg.ReviewTab.Workspace }

func TestTheDiffComesWithAKeyAndAPollDoesNot(t *testing.T) {
	s, _ := reviewTabServer(t)
	if key := diffKeyOf(t, s, ""); key == "" {
		t.Error("the diff has no key")
	}
	var poll map[string]json.RawMessage
	json.Unmarshal(do(t, s, req{path: "/api/review", headers: authed(nil)}).Body.Bytes(), &poll)
	if _, there := poll["diffKey"]; there {
		t.Error("a poll, which leaves the diff out, carries a key for it")
	}
}

func TestTheCodeIsSaidUnchangedWhileTheKeyStillStandsForTheDiff(t *testing.T) {
	s, _ := reviewTabServer(t)
	key := diffKeyOf(t, s, "")
	got, code := getChanged(t, s, "key="+key)
	if code != http.StatusOK || got.Changed || got.Key != key {
		t.Errorf("unchanged code: %+v (%d), want changed false and key %q", got, code, key)
	}
}

func TestTheCodeIsSaidChangedOnceTheDiffIsNotTheOneTheKeyStoodFor(t *testing.T) {
	s, _ := reviewTabServer(t)
	key := diffKeyOf(t, s, "")
	os.WriteFile(filepath.Join(workspaceOf(s), "a.txt"), []byte("one\ntwo\nthree\n"), 0o644)
	got, code := getChanged(t, s, "key="+key)
	if code != http.StatusOK || !got.Changed || got.Key == "" || got.Key == key {
		t.Errorf("changed code: %+v (%d), want changed true and a key other than %q", got, code, key)
	}
	// A new file is a change too, as is the diff going empty (everything committed).
	os.WriteFile(filepath.Join(workspaceOf(s), "b.txt"), []byte("new\n"), 0o644)
	if got, _ := getChanged(t, s, "key="+key); !got.Changed {
		t.Error("a new file wasn't a change")
	}
}

func TestAskingWhetherTheCodeChangedMovesNoCommentAndClosesNoReview(t *testing.T) {
	s, store := reviewTabServer(t)
	if _, err := store.Do(review.Request{Op: "comment", File: "a.txt", Line: 2, Code: "two", Body: "why two?"}, review.Agent); err != nil {
		t.Fatal(err)
	}
	key := diffKeyOf(t, s, "")
	// A line comes in above it: a diff request would move the comment to line 3.
	os.WriteFile(filepath.Join(workspaceOf(s), "a.txt"), []byte("zero\none\ntwo\n"), 0o644)

	if got, _ := getChanged(t, s, "key="+key); !got.Changed {
		t.Fatal("the code wasn't said changed")
	}
	rv, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if rv.Threads[0].Line != 2 {
		t.Errorf("asking moved the comment to line %d", rv.Threads[0].Line)
	}
	// The same change, with the diff asked for, does move it: the check is the read-only one.
	getReview(t, s, "/api/review?diff=1")
	if rv, _ = store.Load(); rv.Threads[0].Line != 3 {
		t.Errorf("a diff request left the comment on line %d, not 3: this test isn't showing the difference", rv.Threads[0].Line)
	}

	// Everything committed: the diff is empty, which a diff request closes the review over; asking doesn't.
	gitIn(t, workspaceOf(s), "add", "-A")
	gitIn(t, workspaceOf(s), "commit", "-q", "-m", "all of it")
	if got, _ := getChanged(t, s, "key="+key); !got.Changed {
		t.Error("an empty diff wasn't said to be a change")
	}
	if rv, _ = store.Load(); rv.Closed != "" {
		t.Errorf("asking closed the review: %q", rv.Closed)
	}
}

// A diff that leaves whitespace out is asked about as that diff: a committed line changed only in its whitespace is nothing to it
// and something to the whole diff, so each key is the other's change.
func TestTheKeyOfADiffWithoutWhitespaceChangesIsAskedAboutWithoutThem(t *testing.T) {
	s, _ := reviewTabServer(t)
	// Only whitespace differs from what is committed ("one\n").
	os.WriteFile(filepath.Join(workspaceOf(s), "a.txt"), []byte("one \n"), 0o644)
	ignoring := diffKeyOf(t, s, "&whitespace=ignore")
	all := diffKeyOf(t, s, "")
	if ignoring == all {
		t.Fatal("the two diffs have the same key: this test isn't showing the difference")
	}
	for what, c := range map[string]struct {
		query   string
		changed bool
	}{
		"the diff without whitespace, asked about as it is": {"key=" + ignoring + "&whitespace=ignore", false},
		"the whole diff, asked about as it is":              {"key=" + all, false},
		"the diff without whitespace, asked about whole":    {"key=" + ignoring, true},
		"the whole diff, asked about without whitespace":    {"key=" + all + "&whitespace=ignore", true},
	} {
		if got, _ := getChanged(t, s, c.query); got.Changed != c.changed {
			t.Errorf("%s: changed is %v, want %v", what, got.Changed, c.changed)
		}
	}
}

func TestAskingWhetherTheCodeChangedNeedsAKeyAndTheTabsToken(t *testing.T) {
	s, _ := reviewTabServer(t)
	if _, code := getChanged(t, s, ""); code != http.StatusBadRequest {
		t.Errorf("no key: %d, want 400", code)
	}
	if rec := do(t, s, req{path: "/api/review/changed?key=x"}); rec.Code != http.StatusUnauthorized {
		t.Errorf("no token: %d, want 401", rec.Code)
	}
	bare := New(Config{Sandbox: "box", Token: token, Port: testPort})
	if rec := do(t, bare, req{path: "/api/review/changed?key=x", headers: authed(nil)}); rec.Code != http.StatusNotFound {
		t.Errorf("no review tab: %d, want 404", rec.Code)
	}
}

func TestADiffKeyIsTheSameForTheSameChangesInAnyOrderAndOtherForOthers(t *testing.T) {
	a := review.File{Path: "a.go", Status: "modified", Fingerprint: "1"}
	b := review.File{Path: "b.go", Status: "added", Fingerprint: "2"}
	if review.DiffKey([]review.File{a, b}) != review.DiffKey([]review.File{b, a}) {
		t.Error("the key depends on the order the files came in")
	}
	base := review.DiffKey([]review.File{a, b})
	for what, files := range map[string][]review.File{
		"a changed fingerprint": {{Path: "a.go", Status: "modified", Fingerprint: "9"}, b},
		"another status":        {{Path: "a.go", Status: "deleted", Fingerprint: "1"}, b},
		"another path":          {{Path: "c.go", Status: "modified", Fingerprint: "1"}, b},
		"a file fewer":          {a},
		"nothing":               {},
	} {
		if review.DiffKey(files) == base {
			t.Errorf("%s gave the same key", what)
		}
	}
	if review.DiffKey(nil) == "" || review.DiffKey(nil) != review.DiffKey([]review.File{}) {
		t.Error("no files has no stable key")
	}
}
