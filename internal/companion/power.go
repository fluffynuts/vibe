package companion

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"regexp"
	"slices"
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

// sandboxNamePattern is what a sandbox's name may be: one that ends up in
// the names of files (its record, its log), so it can't name another place.
var sandboxNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// ValidSandboxName says name could be a sandbox's: letters, digits, dots,
// dashes and underscores, starting with a letter or digit, and no "..".
func ValidSandboxName(name string) bool {
	return sandboxNamePattern.MatchString(name) && !strings.Contains(name, "..")
}

// powerSandbox is the sandbox a request is for: the one named, or this
// page's. It answers 400 itself, and says not ok, for a name that isn't one.
func (s *Server) powerSandbox(w http.ResponseWriter, name string) (string, bool) {
	if name == "" {
		name = s.cfg.Sandbox
	}
	if !ValidSandboxName(name) {
		writeError(w, http.StatusBadRequest, "that is not the name of a sandbox")
		return "", false
	}
	return name, true
}

func (s *Server) powerState(w http.ResponseWriter, r *http.Request) {
	name, ok := s.powerSandbox(w, r.URL.Query().Get("sandbox"))
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, s.cfg.Power.Check(name))
}

func (s *Server) powerDo(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Sandbox string `json:"sandbox"`
		Action  string `json:"action"`
		// Confirmed says the user was asked, and Warns what they were
		// shown: what is found now must be covered by it.
		Confirmed bool     `json:"confirmed"`
		Warns     []string `json:"warns"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || !powerActions[body.Action] {
		writeError(w, http.StatusBadRequest, `expected {"action": "stop", "restart" or "start"}`)
		return
	}
	name, ok := s.powerSandbox(w, body.Sandbox)
	if !ok {
		return
	}
	check := s.cfg.Power.Check(name)
	if check.Error != "" {
		writeError(w, http.StatusNotFound, check.Error)
		return
	}
	action, known := check.Actions[body.Action]
	switch {
	case !known:
		writeError(w, http.StatusConflict, "can't "+body.Action+" a sandbox from here")
		return
	case !action.OK:
		writeError(w, http.StatusConflict, action.Why)
		return
	case len(action.Warns) > 0 && !(body.Confirmed && covers(body.Warns, action.Warns)):
		writeError(w, http.StatusConflict, "this needs confirming first, as it would now interrupt: "+strings.Join(action.Warns, "; "))
		return
	}
	if err := s.cfg.Power.Do(name, body.Action); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]bool{"ok": true})
}

// covers says every one of needed is among shown.
func covers(shown, needed []string) bool {
	for _, n := range needed {
		if !slices.Contains(shown, n) {
			return false
		}
	}
	return true
}

// powerLog says what the last action said, and which run of this server says
// so: a restart is a log answered under another boot, which the page sees
// without waiting for its next poll of the state.
func (s *Server) powerLog(w http.ResponseWriter, r *http.Request) {
	name, ok := s.powerSandbox(w, r.URL.Query().Get("sandbox"))
	if !ok {
		return
	}
	from, _ := strconv.ParseInt(r.URL.Query().Get("from"), 10, 64)
	text, next, running := s.cfg.Power.Log(name, from)
	writeJSON(w, http.StatusOK, map[string]interface{}{"text": text, "next": next, "running": running, "boot": s.boot})
}
