package companion

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"
)

// fakeThemes is the vibe side of the theme picker: what the agent's theme is, and a record of what it was told.
type fakeThemes struct {
	mu      sync.Mutex
	state   ThemeState
	set     []string
	err     error
	color   string
	colorOf func() string
}

func (f *fakeThemes) Themes(ctx context.Context) ThemeState {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.state
}

func (f *fakeThemes) SetTheme(ctx context.Context, choice string) (ThemeSet, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.set = append(f.set, choice)
	if f.err != nil {
		return ThemeSet{}, f.err
	}
	f.state.Current = choice
	return ThemeSet{Live: f.state.Live, Color: f.color}, nil
}

func themeServer(f *fakeThemes, cfg ...func(*Config)) *Server {
	c := Config{Sandbox: "box", Token: token, Port: testPort, Themes: f}
	for _, change := range cfg {
		change(&c)
	}
	return New(c)
}

func TestWithoutAThemePickerThereIsNoThemeToChange(t *testing.T) {
	s := New(Config{Sandbox: "box", Token: token, Port: testPort})
	for _, r := range []req{{path: "/api/theme", headers: authed(nil)}, post("/api/theme", `{"choice":"amber"}`)} {
		if code := do(t, s, r).Code; code != http.StatusNotFound {
			t.Errorf("%s %s: %d", r.method, r.path, code)
		}
	}
}

func TestTheThemesOnOfferAndTheCurrentOneAreSaid(t *testing.T) {
	f := &fakeThemes{state: ThemeState{Current: "amber", Options: []string{"default", "amber", "teal"}, Live: true}}
	rec := do(t, themeServer(f), req{path: "/api/theme", headers: authed(nil)})
	var got ThemeState
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if got.Current != "amber" || len(got.Options) != 3 || !got.Live {
		t.Errorf("state: %+v", got)
	}
}

func TestChoosingAThemeSetsItAndSaysTheColour(t *testing.T) {
	f := &fakeThemes{state: ThemeState{Current: "default", Options: []string{"default", "teal"}, Live: true}, color: "#00aaaa"}
	rec := do(t, themeServer(f), post("/api/theme", `{"choice":"teal"}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	var got ThemeSet
	json.Unmarshal(rec.Body.Bytes(), &got)
	if len(f.set) != 1 || f.set[0] != "teal" || got.Color != "#00aaaa" || !got.Live {
		t.Errorf("set %v, answered %+v", f.set, got)
	}
}

// The page shows the colour the agent's theme has, kept a minute: a change the user has just made is not
// to be shown as the old one for that long.
func TestAThemeJustChosenIsTheColourTheNextStateSays(t *testing.T) {
	colour := "#aa0000"
	f := &fakeThemes{state: ThemeState{Options: []string{"default", "teal"}}, color: "#00aaaa"}
	s := themeServer(f, func(c *Config) { c.ThemeColor = func() string { return colour } })
	themeOf := func() string {
		var st struct {
			ThemeColor string `json:"themeColor"`
		}
		json.Unmarshal(do(t, s, req{path: "/api/state", headers: authed(nil)}).Body.Bytes(), &st)
		return st.ThemeColor
	}
	if got := themeOf(); got != "#aa0000" {
		t.Fatalf("before: %q", got)
	}
	colour = "#00aaaa"
	if rec := do(t, s, post("/api/theme", `{"choice":"teal"}`)); rec.Code != http.StatusOK {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if got := themeOf(); got != "#00aaaa" {
		t.Errorf("after: %q", got)
	}
}

func TestAThemeThatCantBeSetIsSaidWhy(t *testing.T) {
	f := &fakeThemes{err: errors.New("settings.json isn't readable as JSON")}
	rec := do(t, themeServer(f), post("/api/theme", `{"choice":"teal"}`))
	if rec.Code != http.StatusInternalServerError || !strings.Contains(rec.Body.String(), "isn't readable") {
		t.Errorf("%d %s", rec.Code, rec.Body)
	}
}

func TestAThemeNotOnOfferIsRefusedAsTheUsersMistake(t *testing.T) {
	f := &fakeThemes{err: ErrNoSuchTheme}
	if rec := do(t, themeServer(f), post("/api/theme", `{"choice":"nope"}`)); rec.Code != http.StatusBadRequest {
		t.Errorf("%d %s", rec.Code, rec.Body)
	}
	if rec := do(t, themeServer(f), post("/api/theme", `not json`)); rec.Code != http.StatusBadRequest {
		t.Errorf("bad body: %d %s", rec.Code, rec.Body)
	}
	if rec := do(t, themeServer(f), post("/api/theme", `{}`)); rec.Code != http.StatusBadRequest {
		t.Errorf("no choice: %d %s", rec.Code, rec.Body)
	}
}
