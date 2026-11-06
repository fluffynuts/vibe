package companion

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"vibe/internal/review"
)

// A fenced block in the conversation is coloured by the host, by the language on its fence: the page sends the tag and the text,
// and is sent runs per line. It needs no repository (the Review tab's files do), only the token.
func blockServer() *Server { return New(Config{Sandbox: "box", Token: token, Port: testPort}) }

func colourBlock(t *testing.T, s *Server, tag, text string) (int, review.Highlighted) {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"lang": tag, "text": text})
	rec := do(t, s, req{method: "POST", path: "/api/highlight", headers: authed(nil), body: string(body)})
	var h review.Highlighted
	json.Unmarshal(rec.Body.Bytes(), &h)
	return rec.Code, h
}

func TestABlockIsColouredByItsTag(t *testing.T) {
	code, h := colourBlock(t, blockServer(), "go", "package main\n\nfunc f() {}\n")
	if code != http.StatusOK || h.Language != "Go" || len(h.Lines) != 3 {
		t.Errorf("%d %+v", code, h)
	}
}

func TestColouringABlockNeedsTheToken(t *testing.T) {
	rec := do(t, blockServer(), req{method: "POST", path: "/api/highlight", body: `{"lang":"go","text":"x"}`})
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("%d %s", rec.Code, rec.Body)
	}
}

func TestABlockOfNoLanguageComesBackPlainAndSaysNothing(t *testing.T) {
	for _, tag := range []string{"", "text", "klingon"} {
		code, h := colourBlock(t, blockServer(), tag, "just words\n")
		if code != http.StatusOK || h.Lines != nil || h.Language != "" || h.Skipped != "" {
			t.Errorf("%q: %d %+v", tag, code, h)
		}
	}
}

func TestABlockTooLongComesBackSayingWhyItIsntColoured(t *testing.T) {
	code, h := colourBlock(t, blockServer(), "go", strings.Repeat("x := 1\n", 6000))
	if code != http.StatusOK || h.Lines != nil || !strings.Contains(h.Skipped, "too long") {
		t.Errorf("%d skipped %q", code, h.Skipped)
	}
}

func TestWhatIsntAColouringRequestIsRefused(t *testing.T) {
	s := blockServer()
	for what, body := range map[string]string{"not JSON": "{", "no fields": "[]", "a text that isn't text": `{"lang":"go","text":5}`} {
		rec := do(t, s, req{method: "POST", path: "/api/highlight", headers: authed(nil), body: body})
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: %d %s", what, rec.Code, rec.Body)
		}
	}
	// Past any block that could be coloured, the request is refused unread: no more than a megabyte is taken.
	huge := `{"lang":"go","text":"` + strings.Repeat("a", 2<<20) + `"}`
	rec := do(t, s, req{method: "POST", path: "/api/highlight", headers: authed(nil), body: huge})
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("2 MB: %d", rec.Code)
	}
}

// A block's colouring can be made never to end (Chroma's JSONata lexer, on a comment that isn't closed), and the text is the sandbox's
// to write: it is shown plain once its time is up, saying so, and counts toward colouring being turned off, as a file does.
func TestABlockThatTakesTooLongToColourIsShownWithout(t *testing.T) {
	old := colourWithin
	colourWithin = time.Second
	t.Cleanup(func() { colourWithin = old })
	began := time.Now()
	code, h := colourBlock(t, blockServer(), "jsonata", "/*")
	if code != http.StatusOK || h.Lines != nil || !strings.Contains(h.Skipped, "too long") {
		t.Errorf("%d %+v", code, h)
	}
	if took := time.Since(began); took > 10*time.Second {
		t.Errorf("took %s", took)
	}
}

func TestBlocksAndFilesShareWhatTurnsColouringOff(t *testing.T) {
	old := colourWithin
	t.Cleanup(func() { colourWithin = old })
	s := blockServer()
	if _, h := colourBlock(t, s, "go", "package b\n"); h.Language != "Go" {
		t.Fatalf("before: %+v", h)
	}
	colourWithin = 200 * time.Millisecond
	for i := 0; i < colourGiveUpAfter; i++ {
		if _, h := colourBlock(t, s, "jsonata", "/*"); !strings.Contains(h.Skipped, "too long") {
			t.Fatalf("try %d: %+v", i+1, h)
		}
	}
	colourWithin = old
	code, h := colourBlock(t, s, "go", "package b\n")
	if code != http.StatusOK || h.Lines != nil || !strings.Contains(h.Skipped, "turned off") {
		t.Errorf("after %d took too long: %d %+v", colourGiveUpAfter, code, h)
	}
}

func TestOnlyAFewBlocksAreColouredAtOnce(t *testing.T) {
	old := colourWithin
	colourWithin = 500 * time.Millisecond
	t.Cleanup(func() { colourWithin = old })
	s := blockServer()
	s.colours = newColourer(2)
	began := time.Now()
	done := make(chan struct{})
	for range 4 {
		go func() { colourBlock(t, s, "jsonata", "/*"); done <- struct{}{} }()
	}
	for range 4 {
		<-done
	}
	if took := time.Since(began); took < 2*colourWithin {
		t.Errorf("4 blocks that each took %s were coloured in %s: more than 2 at once", colourWithin, took)
	}
}
