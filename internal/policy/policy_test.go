package policy

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestParseLogFlatArray(t *testing.T) {
	doc := `[
  {"resource": "api.anthropic.com", "decision": "POLICY_DECISION_ALLOWED", "count": 339, "last_seen": "2026-10-09T12:04:51Z", "rule": "anthropic", "proxy_type": "forward"},
  {"resource": "http-intake.logs.us5.datadoghq.com", "decision": "POLICY_DECISION_BLOCKED", "count": 86, "last_seen": "2026-10-09T12:05:31Z", "reason": "no matching allow rule"}
]`
	got, err := ParseLog([]byte(doc))
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Entries) != 2 || got.Skipped != 0 {
		t.Fatalf("entries = %d, skipped = %d", len(got.Entries), got.Skipped)
	}
	first := got.Entries[0]
	if first.Host != "http-intake.logs.us5.datadoghq.com" || first.Decision != Blocked || first.Hits != 86 {
		t.Errorf("newest first, then: %+v", first)
	}
	if first.Reason != "no matching allow rule" {
		t.Errorf("reason = %q", first.Reason)
	}
	second := got.Entries[1]
	if second.Decision != Allowed || second.Rule != "anthropic" || second.ProxyType != "forward" || second.Hits != 339 {
		t.Errorf("second = %+v", second)
	}
	if !strings.Contains(string(second.Raw), "api.anthropic.com") {
		t.Errorf("raw = %s", second.Raw)
	}
}

func TestParseLogWrappedWithOtherNames(t *testing.T) {
	doc := `{"sandbox": "vibe", "entries": [
  {"host": "github.com", "status": "allowed", "hits": "17", "lastSeen": {"seconds": 1791547439, "nanos": 5}},
  {"host": "ads.example.com", "blocked": true, "requests": 2, "time": 1791547500000}
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
	if e := byHost["github.com"]; e.Decision != Allowed || e.Hits != 17 || !strings.HasPrefix(e.LastSeen, "2026-") {
		t.Errorf("github = %+v", e)
	}
	if e := byHost["ads.example.com"]; e.Decision != Blocked || e.Hits != 2 || !strings.HasPrefix(e.LastSeen, "2026-") {
		t.Errorf("ads = %+v", e)
	}
}

func TestParseLogCountsWhatItCannotReadAHostFrom(t *testing.T) {
	got, err := ParseLog([]byte(`[{"decision": "allowed", "count": 3}, {"host": "a.example.com", "decision": "allowed"}]`))
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Entries) != 1 || got.Skipped != 1 {
		t.Errorf("entries = %d, skipped = %d", len(got.Entries), got.Skipped)
	}
}

func TestParseLogOfNothingIsAnEmptyLog(t *testing.T) {
	for _, doc := range []string{"", "  \n", "[]", "null", "{}"} {
		got, err := ParseLog([]byte(doc))
		if err != nil {
			t.Errorf("%q: %v", doc, err)
		}
		if len(got.Entries) != 0 {
			t.Errorf("%q: entries = %+v", doc, got.Entries)
		}
	}
}

func TestParseLogOfNonJSONIsAnError(t *testing.T) {
	if _, err := ParseLog([]byte("no sandbox named foo")); err == nil {
		t.Error("expected an error")
	}
}

func TestParseRulesNestedInPolicies(t *testing.T) {
	doc := `{"policies": [
  {"name": "local", "source": "local", "rules": [
    {"rule_id": "r-1", "name": "allow-github", "resources": ["github.com", "*.github.com"], "decision": "allow", "protocol": "tcp", "sandbox": "vibe"}
  ]},
  {"name": "org", "source": "org", "rules": [
    {"rule_id": "o-1", "resources": ["*.corp.example"], "decision": "deny", "source": "org"}
  ]}
]}`
	got, err := ParseRules([]byte(doc))
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Rules) != 2 {
		t.Fatalf("rules = %+v", got.Rules)
	}
	local, org := got.Rules[0], got.Rules[1]
	if local.ID != "r-1" || !reflect.DeepEqual(local.Resources, []string{"github.com", "*.github.com"}) ||
		local.Decision != Allowed || local.Protocol != "tcp" {
		t.Errorf("local = %+v", local)
	}
	if org.Decision != Blocked || org.Source != "org" {
		t.Errorf("org = %+v", org)
	}
	if !local.Removable {
		t.Error("a local rule with an id should be removable")
	}
	if org.Removable {
		t.Error("an org rule must never be offered for removal")
	}
}

func TestParseRulesWithoutAnIDCannotBeRemoved(t *testing.T) {
	got, err := ParseRules([]byte(`[{"resource": "a.example.com", "decision": "allow", "source": "local"}]`))
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Rules) != 1 || got.Rules[0].Removable {
		t.Errorf("rules = %+v", got.Rules)
	}
}

func TestParseRulesCountsWhatHasNoResource(t *testing.T) {
	got, err := ParseRules([]byte(`[{"rule_id": "x", "decision": "allow"}]`))
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Rules) != 0 || got.Skipped != 1 {
		t.Errorf("rules = %+v, skipped = %d", got.Rules, got.Skipped)
	}
}

func TestValidHost(t *testing.T) {
	good := []string{
		"github.com", "*.github.com", "**.example.com", "api?.example.com", "api[12].example.com",
		"example.com:443", "10.0.0.0/8", "[2001:db8::1]:443", "2001:db8::1/128", "host_name.internal",
	}
	for _, h := range good {
		if err := ValidHost(h); err != nil {
			t.Errorf("%q: %v", h, err)
		}
	}
	bad := []string{
		"", "-rf", "--sandbox", "a.com,b.com", "a.com b.com", "a.com;rm -rf /", "$(id).com", "a.com\n",
		"*", "**", "*.*", "?", strings.Repeat("a", 301),
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
		{"policy", "rm", "network", "--sandbox", "vibe", "--id", "r-1"},
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
	r := &recorder{out: "[]"}
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

// TestParseLogGroupedByDecision is the shape sbx really prints: the decision
// is the list an entry is in, and the count is count_since.
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
