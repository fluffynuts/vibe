package companion

import (
	"encoding/json"
	"net/http"
	"slices"
	"strconv"
	"strings"
)

// The agent's permission mode: chosen on the Admin tab, applied to the running agent, and said when the mode Claude Code reports
// isn't the one asked for (see docs/plan-agent-permission-mode.md). An organisation's managed settings can disable a mode
// (bypassPermissions): Claude Code then starts in its settings' defaultMode without a word, or refuses a change with an error.

// PermissionModes are the modes the Admin tab offers, as Claude Code's --permission-mode takes them.
var PermissionModes = []string{"auto", "bypassPermissions", "acceptEdits", "manual", "dontAsk", "plan"}

// DefaultPermissionMode is the mode the agent runs in when nothing chooses another.
const DefaultPermissionMode = "auto"

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
		c.modeInForce = canonicalMode(mode)
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
		if answer.Response.Subtype == "error" {
			c.modeRefused = answer.Response.Error
		} else {
			c.modeRefused = ""
		}
	}
}

// modeState is the running agent's mode, nil when there's no agent, or it hasn't said what it was asked for, or what it is in.
// mu is held.
func (c *conversation) modeState() *permissionState {
	asker, ok := c.agent.(ModeAsker)
	if !ok || c.modeInForce == "" {
		return nil
	}
	asked := canonicalMode(asker.AskedPermissionMode())
	if asked == "" {
		return nil
	}
	return &permissionState{Asked: asked, InForce: c.modeInForce, Refused: c.modeRefused}
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
