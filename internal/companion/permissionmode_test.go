package companion

import (
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"testing"
)

// askingAgent is an agent that says which permission mode it was asked for (the rest of Agent isn't used here).
type askingAgent struct {
	Agent
	asked string
}

func (a *askingAgent) AskedPermissionMode() string { return a.asked }

// convoWith is a conversation whose running agent was asked for mode.
func convoWith(asked string) *conversation {
	c := &conversation{}
	c.agent = &askingAgent{asked: asked}
	return c
}

func TestTheModeInForceIsTakenFromTheAgentsInitAndStatusEvents(t *testing.T) {
	c := convoWith("auto")
	c.line([]byte(`{"type":"system","subtype":"init","permissionMode":"default","session_id":"s"}`))
	if got := c.permission(); got == nil || got.InForce != "manual" || got.Asked != "auto" {
		t.Fatalf("after init in default: %+v", got)
	}
	c.line([]byte(`{"type":"system","subtype":"status","status":null,"permissionMode":"auto"}`))
	if got := c.permission(); got != nil {
		t.Errorf("in force as asked, yet said: %+v", got)
	}
}

func TestNothingIsSaidUntilTheAgentHasSaidItsMode(t *testing.T) {
	if got := convoWith("bypassPermissions").permission(); got != nil {
		t.Errorf("said before the agent did: %+v", got)
	}
	c := &conversation{}
	c.line([]byte(`{"type":"system","subtype":"init","permissionMode":"default"}`))
	if got := c.permission(); got != nil {
		t.Errorf("an agent that doesn't say what it was asked for: %+v", got)
	}
}

func TestARefusalOfAModeChangeIsKeptUntilOneSucceeds(t *testing.T) {
	c := convoWith("bypassPermissions")
	c.line([]byte(`{"type":"system","subtype":"init","permissionMode":"auto"}`))
	c.line([]byte(`{"type":"control_response","response":{"subtype":"error","request_id":"vibe-mode-3","error":"Cannot set permission mode to bypassPermissions because it is disabled by settings or configuration","error_code":"bypass_disabled"}}`))
	got := c.permission()
	if got == nil || got.Refused != "Cannot set permission mode to bypassPermissions because it is disabled by settings or configuration" {
		t.Fatalf("got %+v", got)
	}
	c.line([]byte(`{"type":"control_response","response":{"subtype":"error","request_id":"vibe-2","error":"something about a model"}}`))
	if got := c.permission(); got == nil || got.Refused == "" || got.Refused == "something about a model" {
		t.Errorf("another request's error was taken for the mode's: %+v", got)
	}
	c.agent = &askingAgent{asked: "acceptEdits"}
	c.line([]byte(`{"type":"control_response","response":{"subtype":"success","request_id":"vibe-mode-4","response":{"mode":"acceptEdits"}}}`))
	c.line([]byte(`{"type":"system","subtype":"status","status":null,"permissionMode":"acceptEdits"}`))
	if got := c.permission(); got != nil {
		t.Errorf("after a change that succeeded: %+v", got)
	}
}

func TestAModeLineIsNotShownInTheConversation(t *testing.T) {
	c := convoWith("auto")
	c.line([]byte(`{"type":"control_response","response":{"subtype":"error","request_id":"vibe-mode-1","error":"no"}}`))
	if len(c.events) != 0 {
		t.Errorf("shown: %+v", c.events)
	}
}

// modeChoice stands in for the profile's choice.
type modeChoice struct {
	mode, from string
	set        []string
	err        error
}

func modeServer(m *modeChoice) *Server {
	s := newServer(&fakeSbx{})
	s.cfg.PermissionMode = &PermissionModeChoice{
		Get: func() (string, string, error) { return m.mode, m.from, nil },
		Set: func(mode string) error {
			if m.err != nil {
				return m.err
			}
			m.set = append(m.set, mode)
			m.mode = mode
			return nil
		},
	}
	return s
}

type modeBody struct {
	Mode    string   `json:"mode"`
	From    string   `json:"from"`
	Default string   `json:"default"`
	Modes   []string `json:"modes"`
	Asked   string   `json:"asked"`
	InForce string   `json:"inForce"`
	Refused string   `json:"refused"`
	Error   string   `json:"error"`
}

func modeOf(t *testing.T, s *Server, r req) (modeBody, int) {
	t.Helper()
	rec := do(t, s, r)
	var b modeBody
	json.Unmarshal(rec.Body.Bytes(), &b)
	return b, rec.Code
}

func TestTheAdminTabReadsTheChosenModeWhereFromAndWhatIsInForce(t *testing.T) {
	m := &modeChoice{mode: "acceptEdits", from: "~/.vibe/settings.yaml"}
	s := modeServer(m)
	s.convo.agent = &askingAgent{asked: "acceptEdits"}
	s.convo.line([]byte(`{"type":"system","subtype":"init","permissionMode":"default"}`))
	b, code := modeOf(t, s, req{path: "/api/permission-mode", headers: authed(nil)})
	if code != 200 {
		t.Fatalf("status %d", code)
	}
	want := modeBody{Mode: "acceptEdits", From: "~/.vibe/settings.yaml", Default: "auto",
		Modes: []string{"auto", "bypassPermissions", "acceptEdits", "manual", "dontAsk", "plan"}, Asked: "acceptEdits", InForce: "manual"}
	if !reflect.DeepEqual(b, want) {
		t.Errorf("got %+v\nwant %+v", b, want)
	}
}

func TestChoosingAModeKeepsItAndTheDefaultTakesItAway(t *testing.T) {
	m := &modeChoice{}
	s := modeServer(m)
	if b, code := modeOf(t, s, post("/api/permission-mode", `{"mode":"bypassPermissions"}`)); code != 200 || b.Mode != "bypassPermissions" {
		t.Errorf("status %d, %+v", code, b)
	}
	if _, code := modeOf(t, s, post("/api/permission-mode", `{"mode":""}`)); code != 200 {
		t.Errorf("default: status %d", code)
	}
	if !reflect.DeepEqual(m.set, []string{"bypassPermissions", ""}) {
		t.Errorf("kept %q", m.set)
	}
}

func TestAModeClaudeCodeDoesntTakeIsRefused(t *testing.T) {
	m := &modeChoice{}
	s := modeServer(m)
	for _, body := range []string{`{"mode":"yolo"}`, `{"mode":"default"}`, `{}`, `nonsense`} {
		if _, code := modeOf(t, s, post("/api/permission-mode", body)); code != http.StatusBadRequest {
			t.Errorf("%s: status %d", body, code)
		}
	}
	if len(m.set) != 0 {
		t.Errorf("kept %q", m.set)
	}
}

func TestAChoiceThatCantBeKeptSaysWhy(t *testing.T) {
	s := modeServer(&modeChoice{err: errors.New("profile 'p' is vibe's own")})
	if b, code := modeOf(t, s, post("/api/permission-mode", `{"mode":"plan"}`)); code != http.StatusInternalServerError || b.Error != "profile 'p' is vibe's own" {
		t.Errorf("status %d, %+v", code, b)
	}
}

func TestAPageWithNoChoiceHasNoPermissionMode(t *testing.T) {
	if _, code := modeOf(t, newServer(&fakeSbx{}), req{path: "/api/permission-mode", headers: authed(nil)}); code != http.StatusNotFound {
		t.Errorf("status %d", code)
	}
}

func TestTheConversationsPollSaysWhenTheModeInForceIsntTheOneAskedFor(t *testing.T) {
	s := newServer(&fakeSbx{})
	s.convo.agent = &askingAgent{asked: "bypassPermissions"}
	s.convo.line([]byte(`{"type":"system","subtype":"init","permissionMode":"auto"}`))
	rec := do(t, s, req{path: "/api/conversation", headers: authed(nil)})
	var body struct {
		PermissionMode *permissionState `json:"permissionMode"`
	}
	json.Unmarshal(rec.Body.Bytes(), &body)
	if body.PermissionMode == nil || body.PermissionMode.Asked != "bypassPermissions" || body.PermissionMode.InForce != "auto" {
		t.Errorf("got %+v", body.PermissionMode)
	}
}
