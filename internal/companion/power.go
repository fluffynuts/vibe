package companion

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
)

// Power is what the page's power menu and Profiles tab do to a vibe and its
// sandbox: stop, restart, start. Each is a CLI command run apart from the vibe
// serving the page, which it may be ending; the page only saves going to a
// terminal for it. Nil means the page has no such items.
type Power interface {
	// Check says what can be done to sandbox name, and what each would
	// interrupt. It may be slow (it asks the vibes holding the folder).
	Check(name string) PowerCheck
	// Do starts action on sandbox name, apart from this vibe, and returns
	// once it has started: what it says goes to a log (see Log).
	Do(name, action string) error
	// Log is what the last action on sandbox name said, from byte from; next
	// is where to go on from, and running whether the action is still going.
	Log(name string, from int64) (text string, next int64, running bool)
}

// What the power menu can ask for.
const (
	PowerStop    = "stop"    // end the vibes of the sandbox's folder, then stop the sandbox
	PowerRestart = "restart" // end them, and start a new headless vibe in their place
	PowerStart   = "start"   // start a headless vibe for the sandbox
)

var powerActions = map[string]bool{PowerStop: true, PowerRestart: true, PowerStart: true}

// PowerCheck is what can be done to a sandbox, as the page shows it.
type PowerCheck struct {
	Sandbox string `json:"sandbox"`
	// Folder is the folder the sandbox works in.
	Folder  string                 `json:"folder"`
	Actions map[string]PowerAction `json:"actions"`
	// Error says why nothing is known of the sandbox.
	Error string `json:"error,omitempty"`
}

// PowerAction is whether one action can be done.
type PowerAction struct {
	OK bool `json:"ok"`
	// Why says why not, when it can't.
	Why string `json:"why,omitempty"`
	// Warns is what it would interrupt, when it can be done: the page asks
	// before, and the server won't start it unconfirmed.
	Warns []string `json:"warns,omitempty"`
}

var powerFeature = feature{func(s *Server) bool { return s.cfg.Power != nil },
	"this page has no power menu", 1 << 10}

func newBoot() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// powerSandbox is the sandbox a request is for: the one named, or this page's.
func (s *Server) powerSandbox(name string) string {
	if name == "" {
		return s.cfg.Sandbox
	}
	return name
}

func (s *Server) powerState(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.cfg.Power.Check(s.powerSandbox(r.URL.Query().Get("sandbox"))))
}

func (s *Server) powerDo(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Sandbox   string `json:"sandbox"`
		Action    string `json:"action"`
		Confirmed bool   `json:"confirmed"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || !powerActions[body.Action] {
		writeError(w, http.StatusBadRequest, `expected {"action": "stop", "restart" or "start"}`)
		return
	}
	name := s.powerSandbox(body.Sandbox)
	check := s.cfg.Power.Check(name)
	if check.Error != "" {
		writeError(w, http.StatusNotFound, check.Error)
		return
	}
	action, known := check.Actions[body.Action]
	switch {
	case !known:
		writeError(w, http.StatusConflict, "a sandbox can't be "+body.Action+"ed from here")
		return
	case !action.OK:
		writeError(w, http.StatusConflict, action.Why)
		return
	case len(action.Warns) > 0 && !body.Confirmed:
		writeError(w, http.StatusConflict, "this needs confirming first: "+strings.Join(action.Warns, "; "))
		return
	}
	if err := s.cfg.Power.Do(name, body.Action); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]bool{"ok": true})
}

func (s *Server) powerLog(w http.ResponseWriter, r *http.Request) {
	from, _ := strconv.ParseInt(r.URL.Query().Get("from"), 10, 64)
	text, next, running := s.cfg.Power.Log(s.powerSandbox(r.URL.Query().Get("sandbox")), from)
	writeJSON(w, http.StatusOK, map[string]interface{}{"text": text, "next": next, "running": running})
}
