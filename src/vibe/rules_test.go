package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"vibe/internal/policy"
	"vibe/internal/state"
)

// fakeRulesSbx stands in for sbx's policy commands for sandbox "vibe":
// 'policy ls' prints rules (or fails with lsErr), and every other command
// is recorded.
func fakeRulesSbx(t *testing.T, rules string, lsErr error) *[][]string {
	t.Helper()
	var ran [][]string
	was := rulesFor
	rulesFor = func(name string) *policy.Client {
		return &policy.Client{Sandbox: name, Run: func(_ context.Context, args ...string) ([]byte, error) {
			if args[1] == "ls" {
				return []byte(rules), lsErr
			}
			ran = append(ran, args)
			return nil, nil
		}}
	}
	t.Cleanup(func() { rulesFor = was })
	return &ran
}

const ownRule = `{"rules": [{"id": "ea070143", "name": "ea070143", "scope": "sandbox:vibe", "decision": "allow", "resources": ["example.com"], "editable": true}]}`

// A sandbox's own rules, kept before it's removed, come back when it's made
// again.
func TestKeptRulesComeBackWhenTheSandboxIsMadeAgain(t *testing.T) {
	home := t.TempDir()
	fakeRulesSbx(t, ownRule, nil)
	if err := keepRules(home, "vibe"); err != nil {
		t.Fatal(err)
	}
	ran := fakeRulesSbx(t, `{"rules": []}`, nil)
	said, problems := restoreRules(home, "vibe")
	if len(problems) != 0 {
		t.Errorf("problems: %v", problems)
	}
	if len(*ran) != 1 || strings.Join((*ran)[0], " ") != "policy allow network --sandbox vibe example.com" {
		t.Errorf("ran %q", *ran)
	}
	if !strings.Contains(strings.Join(said, "\n"), "allowed example.com") {
		t.Errorf("said %q", said)
	}
}

func TestWithNoRulesKeptNothingIsRestoredOrSaid(t *testing.T) {
	ran := fakeRulesSbx(t, "", nil)
	said, problems := restoreRules(t.TempDir(), "vibe")
	if len(said) != 0 || len(problems) != 0 || len(*ran) != 0 {
		t.Errorf("said %q, problems %v, ran %q", said, problems, *ran)
	}
}

func TestKeptRulesThatCantBeReadAreAProblem(t *testing.T) {
	home := t.TempDir()
	os.MkdirAll(filepath.Dir(state.RulesPath(home, "vibe")), 0o700)
	os.WriteFile(state.RulesPath(home, "vibe"), []byte("{oops"), 0o600)
	ran := fakeRulesSbx(t, "", nil)
	_, problems := restoreRules(home, "vibe")
	if len(problems) != 1 || !strings.Contains(problems[0].Error(), "none of the sandbox's own network rules were put back") || len(*ran) != 0 {
		t.Errorf("problems %v, ran %q", problems, *ran)
	}
}

func TestRulesThatCantBeReadLeaveTheKeptOnesAlone(t *testing.T) {
	home := t.TempDir()
	fakeRulesSbx(t, ownRule, nil)
	if err := keepRules(home, "vibe"); err != nil {
		t.Fatal(err)
	}
	fakeRulesSbx(t, "", errors.New("sbx is not signed in"))
	if err := keepRules(home, "vibe"); err == nil {
		t.Error("no error")
	}
	snap, _, _ := policy.LoadSnapshot(state.RulesPath(home, "vibe"))
	if len(snap.Rules) != 1 {
		t.Errorf("the kept rules were lost: %+v", snap)
	}
}

// A headless vibe ending keeps the rules, as a session ending does, asleep
// or not: sbx answers for them either way.
func TestAHeadlessVibeEndingKeepsTheSandboxsOwnRules(t *testing.T) {
	fakeWake(t, true, nil)
	fakeSandboxState(t, false, true, false)
	fakeRulesSbx(t, ownRule, nil)
	home := t.TempDir()
	notesOf(func() { backUpAsHeadlessEnds(home, "vibe", true) })
	if snap, found, err := policy.LoadSnapshot(state.RulesPath(home, "vibe")); !found || err != nil || len(snap.Rules) != 1 {
		t.Errorf("kept %+v, %v, %v", snap, found, err)
	}
}
