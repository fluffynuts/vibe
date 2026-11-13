package policy

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func allow(host string) []string {
	return []string{"policy", "allow", "network", "--sandbox", "vibe", host}
}

func allowRemoving(t *testing.T, s *sbxWithRules, host string, removing ...string) ([]string, error) {
	t.Helper()
	return (&Client{Sandbox: "vibe", Run: s.run}).AllowRemoving(host, removing)
}

// A deny beats any allow, so adding an allow beside a deny does nothing (and sbx refuses it for the identical resource): the
// denies that cover the host are removed first, and then the allow added.
func TestAllowRemovesTheDenyRulesThatCoverTheHostAndThenAllowsIt(t *testing.T) {
	s := &sbxWithRules{rules: rulesOf(
		ruleJSON("exact", "sandbox:vibe", "deny", "www.kernel.org:443"),
		ruleJSON("wild", "sandbox:vibe", "deny", "*.kernel.org"),
		ruleJSON("elsewhere", "sandbox:vibe", "deny", "other.org"),
	)}
	if _, err := allowRemoving(t, s, "www.kernel.org:443", "exact", "wild"); err != nil {
		t.Fatal(err)
	}
	if want := [][]string{rm("exact"), rm("wild"), allow("www.kernel.org:443")}; !reflect.DeepEqual(s.calls, want) {
		t.Errorf("ran %v, want %v", s.calls, want)
	}
}

func TestAllowOfAHostNothingDeniesIsAPlainAllow(t *testing.T) {
	s := &sbxWithRules{rules: rulesOf(ruleJSON("elsewhere", "sandbox:vibe", "deny", "other.org"))}
	if _, err := allowRemoving(t, s, "www.kernel.org:443"); err != nil {
		t.Fatal(err)
	}
	if want := [][]string{allow("www.kernel.org:443")}; !reflect.DeepEqual(s.calls, want) {
		t.Errorf("ran %v, want %v", s.calls, want)
	}
}

func TestAllowAddsNothingWhenAnAllowAlreadyCoversTheHostOnceTheDenyIsGone(t *testing.T) {
	s := &sbxWithRules{rules: rulesOf(ruleJSON("deny", "sandbox:vibe", "deny", "www.kernel.org:443"), ruleJSON("wild", "global", "allow", "*.kernel.org"))}
	if _, err := allowRemoving(t, s, "www.kernel.org:443", "deny"); err != nil {
		t.Fatal(err)
	}
	if want := [][]string{rm("deny")}; !reflect.DeepEqual(s.calls, want) {
		t.Errorf("ran %v, want %v", s.calls, want)
	}
}

// A deny of the global policy can't be removed for one sandbox, and while it stands nothing the sandbox's own rules say lets the host in.
func TestAllowRefusesAndChangesNothingWhileAGlobalOrUnremovableDenyCoversTheHost(t *testing.T) {
	kit := strings.Replace(ruleJSON("kit", "sandbox:vibe", "deny", "www.kernel.org:443"), `"editable": true`, `"editable": false`, 1)
	for name, tc := range map[string]struct {
		rules, mention string
		removing       []string
	}{
		"global":      {rulesOf(ruleJSON("sandboxs", "sandbox:vibe", "deny", "www.kernel.org:443"), ruleJSON("everyones", "global", "deny", "*.kernel.org")), "sbx policy rm network --id everyones", []string{"sandboxs", "everyones"}},
		"unremovable": {rulesOf(kit), "kit", []string{"kit"}},
	} {
		t.Run(name, func(t *testing.T) {
			s := &sbxWithRules{rules: tc.rules}
			_, err := allowRemoving(t, s, "www.kernel.org:443", tc.removing...)
			var refused Refused
			if !errors.As(err, &refused) || !strings.Contains(err.Error(), tc.mention) {
				t.Fatalf("err = %v, want a refusal saying %q", err, tc.mention)
			}
			if len(s.calls) != 0 {
				t.Errorf("ran %v: the sandbox's own deny was removed and the host is still blocked", s.calls)
			}
		})
	}
}

func TestAllowRefusesWhenTheDenyRulesAreNotThoseTheUserWasShown(t *testing.T) {
	s := &sbxWithRules{rules: rulesOf(ruleJSON("exact", "sandbox:vibe", "deny", "www.kernel.org:443"))}
	if _, err := allowRemoving(t, s, "www.kernel.org:443"); err == nil || !strings.Contains(err.Error(), "changed") {
		t.Fatalf("err = %v", err)
	}
	if len(s.calls) != 0 {
		t.Errorf("ran %v", s.calls)
	}
}

func TestAllowSaysTheDenyWasRemovedWhenTheAllowThatShouldHaveFollowedFailed(t *testing.T) {
	s := &sbxWithRules{
		rules:   rulesOf(ruleJSON("exact", "sandbox:vibe", "deny", "www.kernel.org:443")),
		failing: map[string]error{"allow": errors.New("sbx would not")},
	}
	_, err := allowRemoving(t, s, "www.kernel.org:443", "exact")
	if err == nil {
		t.Fatal("expected an error")
	}
	for _, want := range []string{"exact", "removed", "sbx would not", "blocked"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not say %q", err, want)
		}
	}
}

func TestAllowDoesNotAddTheAllowWhenARemovalFailed(t *testing.T) {
	s := &sbxWithRules{
		rules:   rulesOf(ruleJSON("exact", "sandbox:vibe", "deny", "www.kernel.org:443")),
		failing: map[string]error{"rm": errors.New("sbx said no")},
	}
	if _, err := allowRemoving(t, s, "www.kernel.org:443", "exact"); err == nil {
		t.Fatal("expected an error")
	}
	if want := [][]string{rm("exact")}; !reflect.DeepEqual(s.calls, want) {
		t.Errorf("ran %v", s.calls)
	}
}

func TestABlockedHostSaysWhichDenyRulesBlockIt(t *testing.T) {
	log := Log{Entries: []LogEntry{{Host: "www.kernel.org:443", Decision: Blocked}, {Host: "plain.org:443", Decision: Blocked}}}
	rules := Rules{Rules: []Rule{
		{ID: "exact", Resources: []string{"www.kernel.org:443"}, Decision: Blocked, Scope: "sandbox:vibe", Removable: true},
		{ID: "wild", Resources: []string{"*.kernel.org"}, Decision: Blocked, Scope: "global"},
		{ID: "off", Resources: []string{"www.kernel.org"}, Decision: Blocked, Status: "inactive"},
		{ID: "other", Resources: []string{"other.org"}, Decision: Blocked},
	}}
	got := Resolve(log, rules).Entries
	var ids []string
	for _, r := range got[0].BlockedBy {
		ids = append(ids, r.ID)
	}
	if !equalStrings(ids, []string{"exact", "wild"}) || !got[0].BlockedBy[1].Wider || got[0].BlockedBy[0].Wider {
		t.Errorf("blocked by %+v", got[0].BlockedBy)
	}
	if got[1].BlockedBy != nil {
		t.Errorf("a host only the default blocks lists rules: %+v", got[1].BlockedBy)
	}
}
