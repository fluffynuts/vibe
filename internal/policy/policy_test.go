package policy

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

// TestParseLogAsSbxV048PrintsIt is from 'sbx policy log vibe --json --type
// network', sbx v0.48: the decision is the list an entry is in, and the
// count is count.
func TestParseLogAsSbxV048PrintsIt(t *testing.T) {
	doc := `{ "allowed": [ { "sandbox_id": "vibe", "host": "mcp-gateway.docker.internal:80", "proxy_type": "forward", "rule": "<daemon-managed alias>", "count": 54, "first_seen": "2026-10-17T10:05:50.965513681+02:00", "last_seen": "2026-10-17T11:10:47.830113738+02:00" } ],
 "blocked": [ { "sandbox_id": "vibe", "host": "http-intake.logs.us5.datadoghq.com:443", "proxy_type": "forward", "rule": "no applicable policies for op(action=net:connect:tcp, resource=net:domain:http-intake.logs.us5.datadoghq.com:443)", "reason": "No matching allow rule (default deny)", "count": 87, "first_seen": "2026-10-17T10:05:13.127537295+02:00", "last_seen": "2026-10-17T11:11:01.756483328+02:00" } ] }`
	got, err := ParseLog([]byte(doc))
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Entries) != 2 {
		t.Fatalf("entries = %+v", got.Entries)
	}
	blocked, allowed := got.Entries[0], got.Entries[1]
	if blocked.Host != "http-intake.logs.us5.datadoghq.com:443" || blocked.Decision != Blocked || blocked.Hits != 87 ||
		blocked.Reason != "No matching allow rule (default deny)" || blocked.LastSeen != "2026-10-17T09:11:01Z" {
		t.Errorf("newest first, the blocked one: %+v", blocked)
	}
	if allowed.Host != "mcp-gateway.docker.internal:80" || allowed.Decision != Allowed || allowed.Hits != 54 ||
		allowed.Rule != "<daemon-managed alias>" || allowed.ProxyType != "forward" {
		t.Errorf("allowed = %+v", allowed)
	}
}

func TestParseLogOfNothingIsAnEmptyLog(t *testing.T) {
	for _, doc := range []string{"", "  \n", "null", `{"blocked": [], "allowed": []}`, `{"blocked": null}`, `{"blocked_hosts": [], "allowed_hosts": []}`} {
		got, err := ParseLog([]byte(doc))
		if err != nil {
			t.Errorf("%q: %v", doc, err)
		}
		if len(got.Entries) != 0 {
			t.Errorf("%q: entries = %+v", doc, got.Entries)
		}
	}
}

// Only the shape sbx prints is read; anything else is said to be unusable,
// rather than shown as an empty or partial log.
func TestParseLogRejectsWhatSbxDoesNotPrint(t *testing.T) {
	for _, doc := range []string{
		"no sandbox named foo",
		`[{"host": "a.example.com", "decision": "allowed"}]`,
		`{"entries": [{"host": "a.example.com", "status": "allowed"}]}`,
		`{}`,
		`{"allowed_hosts": [{"vm_name": "vibe", "count_since": 3}]}`,
	} {
		if got, err := ParseLog([]byte(doc)); err == nil {
			t.Errorf("ParseLog(%s) = %+v, want an error", doc, got)
		}
	}
}

// realNetworkRules is 'sbx policy ls vibe --json --wide --type network'
// from sbx v0.48, cut down to a default rule, one the user added and the
// sandbox's kit's.
const realNetworkRules = `{
  "rules": [
    {
      "id": "default-ai-services",
      "name": "default-ai-services",
      "policy_id": "local-policy",
      "scope": "global",
      "applies_to": "all",
      "resource_type": "network",
      "decision": "allow",
      "resources": ["api.anthropic.com:443", "**.openai.com:443"],
      "origin": "local",
      "layer": "local",
      "status": "active",
      "editable": true,
      "provenance": {"created_via": "default"},
      "actions": ["net:connect:tcp"]
    },
    {
      "id": "1e8df9a6-b7fd-4cac-948d-0fab95719547",
      "name": "1e8df9a6-b7fd-4cac-948d-0fab95719547",
      "policy_id": "local-policy",
      "scope": "global",
      "applies_to": "all",
      "resource_type": "network",
      "decision": "allow",
      "resources": ["192.168.50.45:22"],
      "origin": "local",
      "layer": "local",
      "status": "active",
      "editable": true,
      "provenance": {"created_via": "added"},
      "actions": ["net:connect:tcp"]
    },
    {
      "id": "dc1b99b8-5e3c-44d7-9758-965f9080ad91",
      "name": "kit:vibe",
      "policy_id": "dcb3dbd5-198b-4b5c-b47d-12fcbce8f5f9",
      "scope": "sandbox:vibe",
      "applies_to": "sandbox:vibe",
      "resource_type": "network",
      "decision": "allow",
      "resources": ["api.anthropic.com:443", "github.com", "*.ubuntu.com"],
      "origin": "scoped",
      "layer": "local",
      "status": "active",
      "editable": false,
      "sandbox_id": "vibe",
      "provenance": {"created_via": "provisioned", "source": "sandboxes"},
      "actions": ["net:connect:tcp"]
    }
  ]
}`

func TestParseRulesReadsSbxsShape(t *testing.T) {
	got, err := ParseRules([]byte(realNetworkRules))
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Rules) != 3 {
		t.Fatalf("rules = %+v", got.Rules)
	}
	byID := map[string]Rule{}
	for _, r := range got.Rules {
		byID[r.ID] = r
	}
	def, added, kit := byID["default-ai-services"], byID["1e8df9a6-b7fd-4cac-948d-0fab95719547"], byID["dc1b99b8-5e3c-44d7-9758-965f9080ad91"]
	if def.ID != "default-ai-services" || def.Name != "default-ai-services" || def.Decision != Allowed ||
		!reflect.DeepEqual(def.Resources, []string{"api.anthropic.com:443", "**.openai.com:443"}) ||
		def.Source != "local" || def.Scope != "global" || def.Status != "active" || !def.Removable {
		t.Errorf("default = %+v", def)
	}
	if added.ID != "1e8df9a6-b7fd-4cac-948d-0fab95719547" || !added.Removable {
		t.Errorf("added = %+v", added)
	}
	if kit.Name != "kit:vibe" || kit.Source != "scoped" || kit.Scope != "sandbox:vibe" || kit.Removable {
		t.Errorf("a kit's rule, which sbx says isn't editable, = %+v", kit)
	}
	if !strings.Contains(string(kit.Raw), `"provisioned"`) {
		t.Errorf("raw = %s", kit.Raw)
	}
}

// The sandbox's own rules come first, so they aren't under every global
// one; otherwise sbx's order stands.
func TestParseRulesPutsTheSandboxsOwnRulesFirst(t *testing.T) {
	got, err := ParseRules([]byte(realNetworkRules))
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, r := range got.Rules {
		ids = append(ids, r.ID)
	}
	want := []string{"dc1b99b8-5e3c-44d7-9758-965f9080ad91", "default-ai-services", "1e8df9a6-b7fd-4cac-948d-0fab95719547"}
	if !reflect.DeepEqual(ids, want) {
		t.Errorf("order = %v, want %v", ids, want)
	}
}

// From 'sbx policy ls vibe --json --wide --type filesystem', sbx v0.48.
func TestParseRulesReadsFilesystemRules(t *testing.T) {
	doc := `{
  "rules": [
    {"id": "default-fs-read-allow-all", "name": "default-fs-read-allow-all", "policy_id": "local-policy", "scope": "global", "applies_to": "all",
     "resource_type": "filesystem:read", "decision": "allow", "resources": ["**"], "origin": "local", "layer": "local", "status": "active",
     "editable": false, "provenance": {"created_via": "default"}, "actions": ["fs:mount:read"]},
    {"id": "default-fs-write-allow-all", "name": "default-fs-write-allow-all", "policy_id": "local-policy", "scope": "global", "applies_to": "all",
     "resource_type": "filesystem:write", "decision": "allow", "resources": ["**"], "origin": "local", "layer": "local", "status": "active",
     "editable": false, "provenance": {"created_via": "default"}, "actions": ["fs:mount:read", "fs:mount:write"]}
  ]
}`
	got, err := ParseRules([]byte(doc))
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Rules) != 2 || got.Rules[0].Decision != Allowed || got.Rules[0].Removable || got.Rules[1].Removable {
		t.Errorf("rules = %+v", got.Rules)
	}
}

// From 'sbx policy deny network --sandbox vibe deny-test.example.com', then
// 'sbx policy ls vibe --json --wide --type network', sbx v0.48.
func TestParseRulesReadsADenyRule(t *testing.T) {
	doc := `{
  "rules": [
    {
      "id": "872e8e87-b0d3-443b-a42d-556b54c669f3",
      "name": "872e8e87-b0d3-443b-a42d-556b54c669f3",
      "policy_id": "7dc9bdb9-0b7c-4c79-814a-f882d6fdee44",
      "scope": "sandbox:vibe",
      "applies_to": "sandbox:vibe",
      "resource_type": "network",
      "decision": "deny",
      "resources": ["deny-test.example.com"],
      "origin": "scoped",
      "layer": "local",
      "status": "active",
      "editable": true,
      "sandbox_id": "vibe",
      "provenance": {"created_via": "added"},
      "actions": ["net:connect:tcp", "net:connect:udp"]
    }
  ]
}`
	got, err := ParseRules([]byte(doc))
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Rules) != 1 {
		t.Fatalf("rules = %+v", got.Rules)
	}
	r := got.Rules[0]
	if r.ID != "872e8e87-b0d3-443b-a42d-556b54c669f3" || r.Decision != Blocked || r.Scope != "sandbox:vibe" || !r.Removable {
		t.Errorf("rule = %+v", r)
	}
}

func TestParseRulesOfNothingIsNoRules(t *testing.T) {
	for _, doc := range []string{"", "null", `{"rules": []}`, `{"rules": null}`} {
		got, err := ParseRules([]byte(doc))
		if err != nil || len(got.Rules) != 0 {
			t.Errorf("%q: %+v, %v", doc, got.Rules, err)
		}
	}
}

func TestParseRulesRejectsWhatSbxDoesNotPrint(t *testing.T) {
	for _, doc := range []string{
		"not json",
		`[{"id": "x", "decision": "allow", "resources": ["a.example.com"]}]`,
		`{"policies": [{"name": "local", "rules": [{"rule_id": "r-1", "resources": ["github.com"], "decision": "allow"}]}]}`,
		`{}`,
		`{"rules": [{"id": "x", "decision": "allow"}]}`,
		`{"rules": [{"id": "x", "decision": "maybe", "resources": ["a.example.com"]}]}`,
	} {
		if got, err := ParseRules([]byte(doc)); err == nil {
			t.Errorf("ParseRules(%s) = %+v, want an error", doc, got)
		}
	}
}

func TestValidHost(t *testing.T) {
	good := []string{
		"github.com", "*.github.com", "**.example.com", "api?.example.com", "api[12].example.com",
		"example.com:443", "10.0.0.0/8", "[2001:db8::1]:443", "2001:db8::1/128", "host_name.internal",
		"*.example.com:443", "2001:db8::/32", "localhost", "::1",
	}
	for _, h := range good {
		if err := ValidHost(h); err != nil {
			t.Errorf("%q: %v", h, err)
		}
	}
	bad := []string{
		"", "-rf", "--sandbox", "a.com,b.com", "a.com b.com", "a.com;rm -rf /", "$(id).com", "a.com\n",
		"*", "**", "*.*", "?", strings.Repeat("a", 301),
		// Patterns that would let in far more than one site: a whole
		// top-level domain, every host on a port, all addresses.
		"*.com", "**.com", "**.com:443", "*:443", "[a-z]*", "*.[a-z]*", "0.0.0.0/0", "8.0.0.0/7", "::/0", "2001::/15",
	}
	for _, h := range bad {
		if err := ValidHost(h); err == nil {
			t.Errorf("%q should be refused", h)
		}
	}
}

func TestValidSandboxAndRuleID(t *testing.T) {
	for _, n := range []string{"vibe", "my-sandbox", "a.b_c", "x1"} {
		if err := ValidSandbox(n); err != nil {
			t.Errorf("%q: %v", n, err)
		}
	}
	for _, n := range []string{"", "-x", "a b", "a/b", "a;b"} {
		if err := ValidSandbox(n); err == nil {
			t.Errorf("%q should be refused", n)
		}
	}
	if err := ValidRuleID("r-1:abc"); err != nil {
		t.Error(err)
	}
	for _, id := range []string{"", "--all", "a b", strings.Repeat("a", 129)} {
		if err := ValidRuleID(id); err == nil {
			t.Errorf("%q should be refused", id)
		}
	}
}

type recorder struct {
	calls [][]string
	out   string
	err   error
}

func (r *recorder) run(_ context.Context, args ...string) ([]byte, error) {
	r.calls = append(r.calls, args)
	return []byte(r.out), r.err
}

func TestMutationsAreScopedToTheSandbox(t *testing.T) {
	r := &recorder{}
	c := &Client{Sandbox: "vibe", Run: r.run}
	if err := c.Allow("api.example.com"); err != nil {
		t.Fatal(err)
	}
	if err := c.Deny("ads.example.com"); err != nil {
		t.Fatal(err)
	}
	if err := c.Remove("r-1"); err != nil {
		t.Fatal(err)
	}
	want := [][]string{
		{"policy", "allow", "network", "--sandbox", "vibe", "api.example.com"},
		{"policy", "deny", "network", "--sandbox", "vibe", "ads.example.com"},
		{"policy", "rm", "network", "--sandbox", "vibe", "--id", "r-1", "--force"},
	}
	if !reflect.DeepEqual(r.calls, want) {
		t.Errorf("calls = %v, want %v", r.calls, want)
	}
}

func TestInvalidInputNeverReachesSbx(t *testing.T) {
	r := &recorder{}
	c := &Client{Sandbox: "vibe", Run: r.run}
	if c.Allow("**") == nil || c.Deny("-x") == nil || c.Remove("--all") == nil {
		t.Error("expected every one to be refused")
	}
	bad := &Client{Sandbox: "-bad", Run: r.run}
	if bad.Allow("a.example.com") == nil {
		t.Error("expected a bad sandbox name to be refused")
	}
	if _, err := bad.Log(); err == nil {
		t.Error("expected a bad sandbox name to be refused")
	}
	if len(r.calls) != 0 {
		t.Errorf("sbx was run: %v", r.calls)
	}
}

func TestReadsAskSbxForJSONOfTheRightKind(t *testing.T) {
	r := &recorder{out: "null"}
	c := &Client{Sandbox: "vibe", Run: r.run}
	if _, err := c.Log(); err != nil {
		t.Fatal(err)
	}
	if _, err := c.NetworkRules(); err != nil {
		t.Fatal(err)
	}
	if _, err := c.FilesystemRules(); err != nil {
		t.Fatal(err)
	}
	want := [][]string{
		{"policy", "log", "vibe", "--json", "--type", "network"},
		{"policy", "ls", "vibe", "--json", "--wide", "--type", "network"},
		{"policy", "ls", "vibe", "--json", "--wide", "--type", "filesystem"},
	}
	if !reflect.DeepEqual(r.calls, want) {
		t.Errorf("calls = %v, want %v", r.calls, want)
	}
}

func TestSbxErrorsComeBack(t *testing.T) {
	r := &recorder{err: ErrNotSignedIn}
	c := &Client{Sandbox: "vibe", Run: r.run}
	if _, err := c.Log(); !errors.Is(err, ErrNotSignedIn) {
		t.Errorf("err = %v", err)
	}
}

// TestParseLogGroupedByDecision is the shape sbx printed before v0.48: the
// decision is the list an entry is in, and the count is count_since.
func TestParseLogGroupedByDecision(t *testing.T) {
	doc := `{"blocked_hosts": [
  {"host": "kernel.org:80", "vm_name": "vibe", "proxy_type": "forward", "rule": "no applicable policies", "last_seen": "2026-10-09T13:01:10.517026142+02:00", "count_since": 1, "reason": "No matching allow rule (default deny)"}
], "allowed_hosts": [
  {"host": "api.anthropic.com:443", "vm_name": "vibe", "proxy_type": "forward", "rule": "", "last_seen": "2026-10-09T13:03:43.884376885+02:00", "count_since": 438}
]}`
	got, err := ParseLog([]byte(doc))
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Entries) != 2 {
		t.Fatalf("entries = %+v", got.Entries)
	}
	byHost := map[string]LogEntry{}
	for _, e := range got.Entries {
		byHost[e.Host] = e
	}
	if e := byHost["kernel.org:80"]; e.Decision != Blocked || e.Hits != 1 || e.Reason == "" {
		t.Errorf("kernel = %+v", e)
	}
	if e := byHost["api.anthropic.com:443"]; e.Decision != Allowed || e.Hits != 438 {
		t.Errorf("anthropic = %+v", e)
	}
	if err := ValidHost("kernel.org:80"); err != nil {
		t.Errorf("a logged host can't be allowed: %v", err)
	}
}

func TestResolveMarksBlockedHostsARuleNowAllows(t *testing.T) {
	log := Log{Entries: []LogEntry{
		{Host: "api.example.com", Decision: Blocked},
		{Host: "denied.example.com", Decision: Blocked},
		{Host: "other.org", Decision: Blocked},
		{Host: "ok.net", Decision: Allowed},
	}}
	rules := Rules{Rules: []Rule{
		{Resources: []string{"*.example.com"}, Decision: Allowed},
		{Resources: []string{"denied.example.com:443"}, Decision: Blocked},
		{Resources: []string{"ok.net"}, Decision: Allowed},
	}}
	got := Resolve(log, rules)
	want := []string{Unblocked, Blocked, Blocked, Allowed}
	for i, e := range got.Entries {
		if e.Decision != want[i] {
			t.Errorf("%s: got %q, want %q", e.Host, e.Decision, want[i])
		}
	}
	if log.Entries[0].Decision != Blocked {
		t.Error("Resolve changed its input")
	}
}

// With the rules unreadable, the log is still what happened, but it says
// that blocked hosts a rule now allows can't be marked.
func TestResolvedLogSaysWhenTheRulesCouldNotBeRead(t *testing.T) {
	c := &Client{Sandbox: "vibe", Run: func(_ context.Context, args ...string) ([]byte, error) {
		if args[1] == "ls" {
			return nil, errors.New("sbx policy ls: daemon not running")
		}
		return []byte(`{"blocked": [{"host": "a.example.com:443", "count": 1, "last_seen": "2026-10-17T09:00:00Z"}]}`), nil
	}}
	got, err := c.ResolvedLog()
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Entries) != 1 || got.Entries[0].Decision != Blocked {
		t.Errorf("entries = %+v", got.Entries)
	}
	if !strings.Contains(got.Unresolved, "daemon not running") {
		t.Errorf("unresolved = %q, want why the rules couldn't be read", got.Unresolved)
	}
}
