package companion

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"
)

// ErrNoSuchTheme is a ThemePicker's answer to a choice it doesn't offer.
var ErrNoSuchTheme = errors.New("that is not a theme on offer")

// ThemePicker is how the page changes the colour scheme of the sandbox's agent: the theme the agent is set to,
// whose accent colour the page picks itself out by. Only an agent with themes has one.
type ThemePicker interface {
	// Themes says what the agent's theme is now, and the themes on offer.
	Themes(ctx context.Context) ThemeState
	// SetTheme changes the theme, and says how: ErrNoSuchTheme for one that isn't on offer.
	SetTheme(ctx context.Context, choice string) (ThemeSet, error)
}

// ThemeState is the theme the agent has, and those it could have.
type ThemeState struct {
	Current string   `json:"current"`
	Options []string `json:"options"`
	// AgentThemes says the agent has themes of its own, which a choice changes. For one that hasn't, a choice
	// only colours this page: it is recorded in the profile, and nothing is written into the sandbox.
	AgentThemes bool `json:"agentThemes"`
	// Live says the sandbox is running, so a change reaches its agent now: else it is only recorded, for
	// the next time the sandbox is built. Always false for an agent with no themes of its own.
	Live bool `json:"live"`
}

// ThemeSet is what a change of theme came to.
type ThemeSet struct {
	AgentThemes bool `json:"agentThemes"`
	Live        bool `json:"live"`
	// Color is the accent colour of the theme, "" for none.
	Color string `json:"color"`
}

var themeFeature = feature{func(s *Server) bool { return s.cfg.Themes != nil },
	"this page can't change the theme", 1 << 10}

// themeTimeout bounds asking or changing the theme in a sandbox, which is the agent's to make slow.
const themeTimeout = 30 * time.Second

func (s *Server) themeState(w http.ResponseWriter, r *http.Request) {
	if s.cfg.Themes == nil {
		writeError(w, http.StatusNotFound, themeFeature.missing)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), themeTimeout)
	defer cancel()
	writeJSON(w, http.StatusOK, s.cfg.Themes.Themes(ctx))
}

func (s *Server) themeSet(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Choice string `json:"choice"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Choice == "" {
		writeError(w, http.StatusBadRequest, "expected {\"choice\": ...}")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), themeTimeout)
	defer cancel()
	set, err := s.cfg.Themes.SetTheme(ctx, body.Choice)
	if errors.Is(err, ErrNoSuchTheme) {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// The page's colour is read from the agent, and kept a minute: what was just chosen is not to wait.
	s.cache.forget("theme")
	writeJSON(w, http.StatusOK, set)
}
