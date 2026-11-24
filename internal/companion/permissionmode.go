package companion

import (
	"encoding/json"
	"net/http"
	"slices"
	"strconv"
	"strings"
)

// The agent's permission mode: chosen on the Admin tab, applied to the running agent, and said when the mode Claude Code reports
// isn't the one asked for (see docs/adr/2026-11-24-agent-permission-mode-choice.md). An organisation's managed settings can disable a mode
// (bypassPermissions): Claude Code then starts in its settings' defaultMode without a word, or refuses a change with an error.

// PermissionModes are the modes the Admin tab offers, as Claude Code's --permission-mode takes them.
var PermissionModes = []string{"auto", "bypassPermissions", "acceptEdits", "manual", "dontAsk", "plan"}

// DefaultPermissionMode is the mode the agent runs in when nothing chooses another.
const DefaultPermissionMode = "bypassPermissions"

// modeRequestPrefix starts the request_id of a control request that changes the agent's mode, so that its answer is known.
const modeRequestPrefix = "vibe-mode-"

// ModeRequestID is the request_id for the nth control request, when it changes the agent's permission mode.
func ModeRequestID(n int) string { return modeRequestPrefix + strconv.Itoa(n) }

// PermissionModeChoice is the agent's permission mode as the profile chooses it.
type PermissionModeChoice struct {
	// Get is what is chosen ("" for the default) and the file it comes from ("" when nothing chose it).
	Get func() (mode, from string, err error)
	// Set keeps mode ("" takes the choice away), once the page has checked it is one of PermissionModes.
	Set func(mode string) error
}

// ModeAsker is an Agent that says which permission mode it was last asked for: its start's, or a change since.
type ModeAsker interface {
	AskedPermissionMode() string
}

// permissionState is the agent's permission mode for the page: what it was asked for, what it reports being in, and Claude
// Code's words when it refused a change.
type permissionState struct {
	Asked   string `json:"asked"`
	InForce string `json:"inForce"`
	Refused string `json:"refused,omitempty"`
}

// permissionModeNames are the modes as the page names them.
var permissionModeNames = map[string]string{"auto": "Auto", "bypassPermissions": "Bypass permissions", "acceptEdits": "Accept edits",
	"manual": "Manual", "dontAsk": "Don't ask", "plan": "Plan"}

// refusal is what the conversation says of a permission Claude Code refused: the tool, its reason, and the mode the agent was in
// ("" when it hasn't said).
func refusal(tool, reason, mode string) string {
	if tool == "" {
		tool = "a tool"
	}
	text := "Claude Code didn't let the agent use " + tool
	if reason = strings.TrimSpace(reason); reason != "" {
		text += ": " + reason
	} else {
		text += "."
	}
	if name, ok := permissionModeNames[mode]; ok {
		text += " (the agent is in " + name + ")"
	}
	return text
}

// canonicalMode is mode as --permission-mode names it: the agent reports Manual as "default".
func canonicalMode(mode string) string {
	if mode == "default" {
		return "manual"
	}
	return mode
}

// noteMode takes what a line of the agent's says of its mode: an init or status event's mode, or the answer to a mode change.
// mu is held.
func (c *conversation) noteMode(raw []byte, typ, subtype, mode string) {
	switch {
	case typ == "system" && (subtype == "init" || subtype == "status") && mode != "":
		// Kept together, from the same agent: the mode it is in, and the one it was asked for. Both outlive it (it ends ~30 s after
		// its last work), until another agent says its own.
		c.modeInForce = canonicalMode(mode)
		c.modeAsked = ""
		if asker, ok := c.agent.(ModeAsker); ok {
			c.modeAsked = canonicalMode(asker.AskedPermissionMode())
		}
	case typ == "control_response":
		var answer struct {
			Response struct {
				Subtype   string `json:"subtype"`
				RequestID string `json:"request_id"`
				Error     string `json:"error"`
			} `json:"response"`
		}
		json.Unmarshal(raw, &answer)
		if !strings.HasPrefix(answer.Response.RequestID, modeRequestPrefix) {
			return
		}
		// A refusal sends no status event: what the agent was asked for is taken here too, or asked and in force would agree
		// and the refusal go unsaid.
		if asker, ok := c.agent.(ModeAsker); ok {
			c.modeAsked = canonicalMode(asker.AskedPermissionMode())
		}
		if answer.Response.Subtype == "error" {
			c.modeRefused = answer.Response.Error
		} else {
			c.modeRefused = ""
		}
	}
}

// modeState is the mode the agent last said it was in, and what it was asked for then; nil until an agent that says what it was
// asked for has said its mode. mu is held.
func (c *conversation) modeState() *permissionState {
	if c.modeInForce == "" || c.modeAsked == "" {
		return nil
	}
	return &permissionState{Asked: c.modeAsked, InForce: c.modeInForce, Refused: c.modeRefused}
}

// modeToSuggest is the mode that would let through what Claude Code just refused: bypass, unless it was bypass that was refused
// (Auto, which a policy that disables bypass allows); none in bypass itself, where Claude Code still refuses a few things that no
// mode would let through, nor when the mode isn't known. mu is held.
func (c *conversation) modeToSuggest() string {
	st := c.modeState()
	switch {
	case st == nil, st.InForce == "bypassPermissions":
		return ""
	case st.Asked == "bypassPermissions":
		return "auto"
	}
	return "bypassPermissions"
}

// modeNow is the running agent's mode, asked for and in force, whenever it is known (nil when it isn't).
func (c *conversation) modeNow() *permissionState {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.modeState()
}

// permission is the running agent's mode when it isn't the one asked for (nil when it is, or isn't known): for the page to say so.
func (c *conversation) permission() *permissionState {
	c.mu.Lock()
	defer c.mu.Unlock()
	st := c.modeState()
	if st == nil || st.Asked == st.InForce {
		return nil
	}
	return st
}

var permissionModeFeature = feature{func(s *Server) bool { return s.cfg.PermissionMode != nil },
	"this page can't choose the agent's permission mode", 1 << 10}

func (s *Server) permissionModeState(w http.ResponseWriter, r *http.Request) {
	s.writePermissionMode(w)
}

func (s *Server) permissionModeSet(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Mode *string `json:"mode"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Mode == nil || (*body.Mode != "" && !slices.Contains(PermissionModes, *body.Mode)) {
		writeError(w, http.StatusBadRequest, "expected {\"mode\": one of "+strings.Join(PermissionModes, ", ")+", or \"\" for the default}")
		return
	}
	if err := s.cfg.PermissionMode.Set(*body.Mode); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.writePermissionMode(w)
}

func (s *Server) writePermissionMode(w http.ResponseWriter) {
	mode, from, err := s.cfg.PermissionMode.Get()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	body := map[string]interface{}{"mode": mode, "from": from, "default": DefaultPermissionMode, "modes": PermissionModes}
	s.convo.mu.Lock()
	if st := s.convo.modeState(); st != nil {
		body["asked"], body["inForce"], body["refused"] = st.Asked, st.InForce, st.Refused
	}
	s.convo.mu.Unlock()
	writeJSON(w, http.StatusOK, body)
}
