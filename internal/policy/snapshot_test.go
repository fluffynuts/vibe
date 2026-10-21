package policy

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// rulesWithOwn is 'sbx policy ls vibe --json --wide --type network' as sbx
// printed it on the user's host on 2026-10-21, trimmed: a default global
// rule, a global one the user added, the kit's, and three of the
// sandbox's own (two approved from the page's network log, one added on
// its rules card).
const rulesWithOwn = `{
  "rules": [
    {"id": "default-ai-services", "name": "default-ai-services", "policy_id": "local-policy", "scope": "global", "applies_to": "all",
     "resource_type": "network", "decision": "allow", "resources": ["api.anthropic.com:443"], "origin": "local", "layer": "local",
     "status": "active", "editable": true, "provenance": {"created_via": "default"}, "actions": ["net:connect:tcp"]},
    {"id": "1e8df9a6-b7fd-4cac-948d-0fab95719547", "name": "1e8df9a6-b7fd-4cac-948d-0fab95719547", "policy_id": "local-policy",
     "scope": "global", "applies_to": "all", "resource_type": "network", "decision": "allow", "resources": ["192.168.50.45:22"],
     "origin": "local", "layer": "local", "status": "active", "editable": true, "provenance": {"created_via": "added"}, "actions": ["net:connect:tcp"]},
    {"id": "40843a8a-3f77-410c-8fa9-35f72c7b6a9f", "name": "kit:vibe", "policy_id": "84c65fd3-5670-43e3-a338-056657ec5e07",
     "scope": "sandbox:vibe", "applies_to": "sandbox:vibe", "resource_type": "network", "decision": "allow",
     "resources": ["api.anthropic.com:443", "github.com", "*.nuget.org"], "origin": "scoped", "layer": "local", "status": "active",
     "editable": false, "sandbox_id": "vibe", "provenance": {"created_via": "provisioned", "source": "sandboxes"}, "actions": ["net:connect:tcp"]},
    {"id": "53451254-20e8-4d12-b1f5-1bc566ad6dbc", "name": "53451254-20e8-4d12-b1f5-1bc566ad6dbc", "policy_id": "9b6521d8-1af6-4f76-ba1e-60d022be7437",
     "scope": "sandbox:vibe", "applies_to": "sandbox:vibe", "resource_type": "network", "decision": "allow",
     "resources": ["playwright-akamai.azureedge.net:443"], "origin": "scoped", "layer": "local", "status": "active", "editable": true,
     "sandbox_id": "vibe", "provenance": {"created_via": "added"}, "actions": ["net:connect:tcp"]},
    {"id": "c91fa7c3-9048-4642-9802-556ebc926252", "name": "c91fa7c3-9048-4642-9802-556ebc926252", "policy_id": "9b6521d8-1af6-4f76-ba1e-60d022be7437",
     "scope": "sandbox:vibe", "applies_to": "sandbox:vibe", "resource_type": "network", "decision": "allow",
     "resources": ["playwright.download.prss.microsoft.com:443"], "origin": "scoped", "layer": "local", "status": "active", "editable": true,
     "sandbox_id": "vibe", "provenance": {"created_via": "added"}, "actions": ["net:connect:tcp"]},
    {"id": "ea070143-8b28-4fd6-a709-23f758bf0fea", "name": "ea070143-8b28-4fd6-a709-23f758bf0fea", "policy_id": "9b6521d8-1af6-4f76-ba1e-60d022be7437",
     "scope": "sandbox:vibe", "applies_to": "sandbox:vibe", "resource_type": "network", "decision": "allow",
     "resources": ["example.com"], "origin": "scoped", "layer": "local", "status": "active", "editable": true,
     "sandbox_id": "vibe", "provenance": {"created_via": "added"}, "actions": ["net:connect:tcp"]}
  ]
}`

// sbxWith is a client whose sbx prints out for 'policy ls', fails with
// lsErr when set, and records every other command it's given, failing
// those whose host is in refuse.
func sbxWith(out string, lsErr error, refuse ...string) (*Client, *[][]string) {
	var ran [][]string
	c := &Client{Sandbox: "vibe", Run: func(_ context.Context, args ...string) ([]byte, error) {
		if len(args) > 1 && args[1] == "ls" {
			if lsErr != nil {
				return nil, lsErr
			}
			return []byte(out), nil
		}
		ran = append(ran, args)
		for _, host := range refuse {
			if args[len(args)-1] == host {
				return nil, errors.New("sbx said no")
			}
		}
		return nil, nil
	}}
	return c, &ran
}

func TestOnlyTheSandboxsOwnRulesAreSnapshot(t *testing.T) {
	c, _ := sbxWith(rulesWithOwn, nil)
	file := filepath.Join(t.TempDir(), "rules", "vibe.json")
	snap, err := c.SnapshotTo(file, time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	want := []SavedRule{
		{Decision: Allowed, Resources: []string{"playwright-akamai.azureedge.net:443"}},
		{Decision: Allowed, Resources: []string{"playwright.download.prss.microsoft.com:443"}},
		{Decision: Allowed, Resources: []string{"example.com"}},
	}
	if !reflect.DeepEqual(snap.Rules, want) {
		t.Errorf("snapshot = %+v", snap.Rules)
	}
	kept, found, err := LoadSnapshot(file)
	if err != nil || !found || !reflect.DeepEqual(kept.Rules, want) || !kept.Taken.Equal(snap.Taken) {
		t.Errorf("kept = %+v, %v, %v", kept, found, err)
	}
}

// Each snapshot replaces the last whole: a rule removed since stays gone.
func TestASnapshotReplacesTheLastOne(t *testing.T) {
	file := filepath.Join(t.TempDir(), "vibe.json")
	c, _ := sbxWith(rulesWithOwn, nil)
	if _, err := c.SnapshotTo(file, time.Now()); err != nil {
		t.Fatal(err)
	}
	none, _ := sbxWith(`{"rules": []}`, nil)
	if _, err := none.SnapshotTo(file, time.Now()); err != nil {
		t.Fatal(err)
	}
	kept, found, err := LoadSnapshot(file)
	if err != nil || !found || len(kept.Rules) != 0 {
		t.Errorf("after every rule was removed: %+v, %v, %v", kept, found, err)
	}
}

// Rules that can't be read are no reason to forget the ones kept.
func TestASnapshotThatCantBeTakenKeepsTheLastOne(t *testing.T) {
	file := filepath.Join(t.TempDir(), "vibe.json")
	c, _ := sbxWith(rulesWithOwn, nil)
	if _, err := c.SnapshotTo(file, time.Now()); err != nil {
		t.Fatal(err)
	}
	for name, broken := range map[string]*Client{
		"sbx failing":   func() *Client { c, _ := sbxWith("", ErrNotSignedIn); return c }(),
		"output unread": func() *Client { c, _ := sbxWith(`{"policies": 3}`, nil); return c }(),
	} {
		if _, err := broken.SnapshotTo(file, time.Now()); err == nil {
			t.Errorf("%s: no error", name)
		}
		if kept, _, _ := LoadSnapshot(file); len(kept.Rules) != 3 {
			t.Errorf("%s: the last snapshot was lost: %+v", name, kept)
		}
	}
}

func TestNoSnapshotIsntAnError(t *testing.T) {
	if _, found, err := LoadSnapshot(filepath.Join(t.TempDir(), "none.json")); found || err != nil {
		t.Errorf("found %v, err %v", found, err)
	}
}

func TestASnapshotThatCantBeReadSaysSo(t *testing.T) {
	file := filepath.Join(t.TempDir(), "vibe.json")
	os.WriteFile(file, []byte("{not json"), 0o600)
	if _, _, err := LoadSnapshot(file); err == nil || !strings.Contains(err.Error(), file) {
		t.Errorf("err = %v", err)
	}
}

func TestRestoringPutsBackEachRuleAndSaysWhatDidnt(t *testing.T) {
	c, ran := sbxWith("", nil, "too.wide.example")
	snap := Snapshot{Rules: []SavedRule{
		{Decision: Allowed, Resources: []string{"example.com", "too.wide.example"}},
		{Decision: Blocked, Resources: []string{"tracker.example"}},
		{Decision: Allowed, Resources: []string{"**"}},
	}}
	restored, failed := c.Restore(snap)
	if !reflect.DeepEqual(restored, []string{"allowed example.com", "blocked tracker.example"}) {
		t.Errorf("restored = %q", restored)
	}
	if len(failed) != 2 {
		t.Fatalf("failed = %v", failed)
	}
	for i, want := range []string{"sbx policy allow network --sandbox vibe too.wide.example", "sbx policy allow network --sandbox vibe **"} {
		if !strings.Contains(failed[i].Error(), want) {
			t.Errorf("failure %d doesn't say how to do it by hand (%q): %v", i, want, failed[i])
		}
	}
	if !reflect.DeepEqual(*ran, [][]string{
		{"policy", "allow", "network", "--sandbox", "vibe", "example.com"},
		{"policy", "allow", "network", "--sandbox", "vibe", "too.wide.example"},
		{"policy", "deny", "network", "--sandbox", "vibe", "tracker.example"},
	}) {
		t.Errorf("ran = %q", *ran)
	}
}
