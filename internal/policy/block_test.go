package policy

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// sbxWithRules answers `policy ls` with rules, records every command that changes anything, and fails the one whose verb is in failing.
type sbxWithRules struct {
	rules   string
	calls   [][]string
	failing map[string]error
}

func (s *sbxWithRules) run(_ context.Context, args ...string) ([]byte, error) {
	if args[1] == "ls" {
		return []byte(s.rules), nil
	}
	s.calls = append(s.calls, args)
	return nil, s.failing[args[1]]
}

func ruleJSON(id, scope, decision string, resources ...string) string {
	quoted := make([]string, len(resources))
	for i, r := range resources {
		quoted[i] = fmt.Sprintf("%q", r)
	}
	return fmt.Sprintf(`{"id": %q, "name": %q, "scope": %q, "decision": %q, "resources": [%s], "origin": "local", "layer": "local", "status": "active", "editable": true}`,
		id, id, scope, decision, strings.Join(quoted, ","))
}

func rulesOf(rules ...string) string { return `{"rules": [` + strings.Join(rules, ",") + `]}` }

func rm(id string) []string {
	return []string{"policy", "rm", "network", "--sandbox", "vibe", "--id", id, "--force"}
}
func deny(host string) []string {
	return []string{"policy", "deny", "network", "--sandbox", "vibe", host}
}

func block(t *testing.T, s *sbxWithRules, host string, removing ...string) ([]string, error) {
	t.Helper()
	return (&Client{Sandbox: "vibe", Run: s.run}).Block(host, removing)
}

// sbx refuses a deny beside an allow for the identical resource in the same scope, and for no other: so that allow is
// removed, and a deny is added only if some other rule still lets the host through.
func TestBlockRemovesTheSandboxsAllowForTheIdenticalResourceAndAddsNoDenyWhenNothingElseAllowsIt(t *testing.T) {
	s := &sbxWithRules{rules: rulesOf(ruleJSON("exact", "sandbox:vibe", "allow", "www.kernel.org:443"))}
	if _, err := block(t, s, "www.kernel.org:443", "exact"); err != nil {
		t.Fatal(err)
	}
	if want := [][]string{rm("exact")}; !reflect.DeepEqual(s.calls, want) {
		t.Errorf("ran %v, want %v", s.calls, want)
	}
}

func TestBlockAddsADenyWhenAWiderOrGlobalRuleStillAllowsTheHost(t *testing.T) {
	s := &sbxWithRules{rules: rulesOf(
		ruleJSON("exact", "sandbox:vibe", "allow", "www.kernel.org:443"),
		ruleJSON("wild", "sandbox:vibe", "allow", "*.kernel.org"),
	)}
	if _, err := block(t, s, "www.kernel.org:443", "exact"); err != nil {
		t.Fatal(err)
	}
	if want := [][]string{rm("exact"), deny("www.kernel.org:443")}; !reflect.DeepEqual(s.calls, want) {
		t.Errorf("ran %v, want %v: the wildcard must be left alone, and a deny beats it", s.calls, want)
	}
}

func TestBlockNeverRemovesAGlobalRuleAndDeniesBesideAGlobalAllowOfTheIdenticalResource(t *testing.T) {
	s := &sbxWithRules{rules: rulesOf(ruleJSON("global", "global", "allow", "www.kernel.org:443"))}
	if _, err := block(t, s, "www.kernel.org:443"); err != nil {
		t.Fatal(err)
	}
	if want := [][]string{deny("www.kernel.org:443")}; !reflect.DeepEqual(s.calls, want) {
		t.Errorf("ran %v, want %v: a sandbox's deny beside a global allow is accepted by sbx, and wins", s.calls, want)
	}
}

func TestBlockOfAHostNothingAllowsIsAPlainDeny(t *testing.T) {
	s := &sbxWithRules{rules: rulesOf(ruleJSON("elsewhere", "sandbox:vibe", "allow", "other.org"))}
	if _, err := block(t, s, "www.kernel.org:443"); err != nil {
		t.Fatal(err)
	}
	if want := [][]string{deny("www.kernel.org:443")}; !reflect.DeepEqual(s.calls, want) {
		t.Errorf("ran %v, want %v", s.calls, want)
	}
}

func TestAPortMakesAnotherResourceSoAPortlessAllowIsNotRemovedForAPortedHost(t *testing.T) {
	s := &sbxWithRules{rules: rulesOf(ruleJSON("portless", "sandbox:vibe", "allow", "www.kernel.org"))}
	if _, err := block(t, s, "www.kernel.org:443"); err != nil {
		t.Fatal(err)
	}
	if want := [][]string{deny("www.kernel.org:443")}; !reflect.DeepEqual(s.calls, want) {
		t.Errorf("ran %v, want %v: sbx accepts the deny beside it, and it wins", s.calls, want)
	}
}

func TestBlockIgnoresCaseWhenFindingTheIdenticalResource(t *testing.T) {
	s := &sbxWithRules{rules: rulesOf(ruleJSON("exact", "sandbox:vibe", "allow", "WWW.Kernel.org:443"))}
	if _, err := block(t, s, "www.kernel.org:443", "exact"); err != nil {
		t.Fatal(err)
	}
	if want := [][]string{rm("exact")}; !reflect.DeepEqual(s.calls, want) {
		t.Errorf("ran %v, want %v", s.calls, want)
	}
}

// What the user confirmed is what is done: if the rules are not what the dialog showed, nothing is.
func TestBlockRefusesAndChangesNothingWhenTheRulesToRemoveAreNotThoseTheUserWasShown(t *testing.T) {
	for name, tc := range map[string]struct {
		rules    string
		removing []string
	}{
		"a rule appeared":    {rulesOf(ruleJSON("exact", "sandbox:vibe", "allow", "www.kernel.org:443")), nil},
		"a rule went":        {rulesOf(), []string{"exact"}},
		"another rule there": {rulesOf(ruleJSON("other", "sandbox:vibe", "allow", "www.kernel.org:443")), []string{"exact"}},
	} {
		t.Run(name, func(t *testing.T) {
			s := &sbxWithRules{rules: tc.rules}
			_, err := block(t, s, "www.kernel.org:443", tc.removing...)
			if err == nil || !strings.Contains(err.Error(), "changed") {
				t.Fatalf("err = %v, want one saying the rules have changed", err)
			}
			if len(s.calls) != 0 {
				t.Errorf("ran %v", s.calls)
			}
		})
	}
}

func TestBlockRefusesWhenTheConflictingRuleCannotBeRemovedFromHere(t *testing.T) {
	kit := strings.Replace(ruleJSON("kit", "sandbox:vibe", "allow", "www.kernel.org:443"), `"editable": true`, `"editable": false`, 1)
	s := &sbxWithRules{rules: rulesOf(kit)}
	if _, err := block(t, s, "www.kernel.org:443", "kit"); err == nil {
		t.Fatal("expected a refusal: sbx would refuse the deny and the rule cannot be removed")
	}
	if len(s.calls) != 0 {
		t.Errorf("ran %v", s.calls)
	}
}

func TestBlockDoesNotAddADenyWhenTheRemovalFailed(t *testing.T) {
	s := &sbxWithRules{
		rules:   rulesOf(ruleJSON("exact", "sandbox:vibe", "allow", "www.kernel.org:443"), ruleJSON("wild", "sandbox:vibe", "allow", "*.kernel.org")),
		failing: map[string]error{"rm": errors.New("sbx said no")},
	}
	_, err := block(t, s, "www.kernel.org:443", "exact")
	if err == nil || !strings.Contains(err.Error(), "sbx said no") {
		t.Fatalf("err = %v", err)
	}
	if want := [][]string{rm("exact")}; !reflect.DeepEqual(s.calls, want) {
		t.Errorf("ran %v, want only the removal that failed", s.calls)
	}
}

// Half done is said, with what was and was not.
func TestBlockSaysThatTheRuleWasRemovedWhenTheDenyThatShouldHaveFollowedFailed(t *testing.T) {
	s := &sbxWithRules{
		rules:   rulesOf(ruleJSON("exact", "sandbox:vibe", "allow", "www.kernel.org:443"), ruleJSON("wild", "sandbox:vibe", "allow", "*.kernel.org")),
		failing: map[string]error{"deny": errors.New("sbx would not")},
	}
	_, err := block(t, s, "www.kernel.org:443", "exact")
	if err == nil {
		t.Fatal("expected an error")
	}
	for _, want := range []string{"exact", "removed", "sbx would not", "still"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not say %q", err, want)
		}
	}
}

func TestBlockOfAHostAnActiveDenyAlreadyCoversAddsNoSecondDeny(t *testing.T) {
	s := &sbxWithRules{rules: rulesOf(ruleJSON("wild", "sandbox:vibe", "allow", "*.kernel.org"), ruleJSON("no", "sandbox:vibe", "deny", "www.kernel.org:443"))}
	if _, err := block(t, s, "www.kernel.org:443"); err != nil {
		t.Fatal(err)
	}
	if len(s.calls) != 0 {
		t.Errorf("ran %v: the host is already denied, and a deny beside an identical one may be refused", s.calls)
	}
}

func TestBlockRefusesAHostSbxWouldNotAcceptBeforeReadingAnything(t *testing.T) {
	s := &sbxWithRules{}
	if _, err := block(t, s, "**"); err == nil {
		t.Fatal("expected a refusal")
	}
	if len(s.calls) != 0 {
		t.Errorf("ran %v", s.calls)
	}
}

func TestAnAllowRuleIsMarkedAsConflictingOnlyWhenSbxWouldRefuseADenyBesideIt(t *testing.T) {
	log := Log{Entries: []LogEntry{{Host: "www.kernel.org:443", Decision: Blocked}}}
	rules := Rules{Rules: []Rule{
		{ID: "exact", Resources: []string{"WWW.kernel.org:443"}, Decision: Allowed, Scope: "sandbox:vibe"},
		{ID: "several", Resources: []string{"other.org", "www.kernel.org:443"}, Decision: Allowed, Scope: "sandbox:vibe"},
		{ID: "global", Resources: []string{"www.kernel.org:443"}, Decision: Allowed, Scope: "global"},
		{ID: "anothers", Resources: []string{"www.kernel.org:443"}, Decision: Allowed, Scope: "sandbox:other"},
		{ID: "portless", Resources: []string{"www.kernel.org"}, Decision: Allowed, Scope: "sandbox:vibe"},
		{ID: "wild", Resources: []string{"*.kernel.org"}, Decision: Allowed, Scope: "sandbox:vibe"},
		{ID: "noscope", Resources: []string{"www.kernel.org:443"}, Decision: Allowed},
	}}
	got := map[string]bool{}
	for _, r := range Resolve("vibe", log, rules).Entries[0].AllowedBy {
		got[r.ID] = r.Conflicts
	}
	want := map[string]bool{"exact": true, "several": true, "global": false, "anothers": false, "portless": false, "wild": false, "noscope": false}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("conflicts = %v, want %v", got, want)
	}
}

// The page sends back the ids of the rules the log said Block would remove, and Block refuses if its own list differs: so the two have to be
// one decision. Whatever Resolve marks as conflicting, Block removes, and nothing else.
func TestBlockRemovesExactlyTheRulesTheLogSaidConflict(t *testing.T) {
	rulesJSON := rulesOf(
		ruleJSON("exact", "sandbox:vibe", "allow", "www.kernel.org:443"),
		ruleJSON("several", "sandbox:vibe", "allow", "other.org", "www.kernel.org:443"),
		ruleJSON("global", "global", "allow", "www.kernel.org:443"),
		ruleJSON("anothers", "sandbox:other", "allow", "www.kernel.org:443"),
		ruleJSON("portless", "sandbox:vibe", "allow", "www.kernel.org"),
		ruleJSON("wild", "sandbox:vibe", "allow", "*.kernel.org"),
		ruleJSON("noscope", "", "allow", "www.kernel.org:443"),
		ruleJSON("off", "sandbox:vibe", "allow", "www.kernel.org:443"),
	)
	rulesJSON = strings.Replace(rulesJSON, `"id": "off", "name": "off", "scope": "sandbox:vibe", "decision": "allow", "resources": ["www.kernel.org:443"], "origin": "local", "layer": "local", "status": "active"`,
		`"id": "off", "name": "off", "scope": "sandbox:vibe", "decision": "allow", "resources": ["www.kernel.org:443"], "origin": "local", "layer": "local", "status": "inactive"`, 1)
	parsed, err := ParseRules([]byte(rulesJSON))
	if err != nil {
		t.Fatal(err)
	}
	log := Log{Entries: []LogEntry{{Host: "www.kernel.org:443", Decision: Blocked}}}
	var shown []string
	for _, r := range Resolve("vibe", log, parsed).Entries[0].AllowedBy {
		if r.Conflicts {
			shown = append(shown, r.ID)
		}
	}
	s := &sbxWithRules{rules: rulesJSON}
	if _, err := block(t, s, "www.kernel.org:443", shown...); err != nil {
		t.Fatalf("Block refused the very rules the log said conflict (%v): %v", shown, err)
	}
	var removed []string
	for _, call := range s.calls {
		if call[1] == "rm" {
			removed = append(removed, call[6])
		}
	}
	if !sameIDs(removed, shown) || len(shown) != 2 {
		t.Errorf("the log said %v conflict, Block removed %v (want exact and several in both)", shown, removed)
	}
}

func TestAnAllowedHostSaysWhichRulesAllowItToo(t *testing.T) {
	log := Log{Entries: []LogEntry{{Host: "www.kernel.org:443", Decision: Allowed}, {Host: "denied.org:443", Decision: Allowed}}}
	rules := Rules{Rules: []Rule{
		{ID: "exact", Resources: []string{"www.kernel.org:443"}, Decision: Allowed, Scope: "sandbox:vibe"},
		{ID: "a", Resources: []string{"denied.org:443"}, Decision: Allowed},
		{ID: "d", Resources: []string{"denied.org:443"}, Decision: Blocked},
	}}
	got := Resolve("vibe", log, rules).Entries
	if len(got[0].AllowedBy) != 1 || got[0].AllowedBy[0].ID != "exact" || got[0].Decision != Allowed {
		t.Errorf("entry = %+v", got[0])
	}
	if got[1].AllowedBy != nil {
		t.Errorf("a host a deny now covers lists the rules that allow it: %+v", got[1].AllowedBy)
	}
}

func TestBlockAddsTheDenyWhenTheOnlyDenyThereIsIsForAnotherPort(t *testing.T) {
	s := &sbxWithRules{rules: rulesOf(
		ruleJSON("exact", "sandbox:vibe", "allow", "www.kernel.org:443"),
		ruleJSON("wild", "sandbox:vibe", "allow", "*.kernel.org"),
		ruleJSON("d80", "sandbox:vibe", "deny", "www.kernel.org:80"),
	)}
	if _, err := block(t, s, "www.kernel.org:443", "exact"); err != nil {
		t.Fatal(err)
	}
	if want := [][]string{rm("exact"), deny("www.kernel.org:443")}; !reflect.DeepEqual(s.calls, want) {
		t.Errorf("ran %v, want %v: the deny of port 80 does not block 443, which the wildcard still allows", s.calls, want)
	}
}
