package policy

import "testing"

// The log is a history, one entry for each (host, decision): a host blocked and then allowed shows twice. The older says what
// was so, not what is, and is Superseded.
func TestAnOlderEntryOfAHostWhoseLaterEntryDecidedOtherwiseIsSuperseded(t *testing.T) {
	log := Log{Entries: []LogEntry{
		{Host: "a.org:443", Decision: Blocked, LastSeen: "2026-11-12T10:00:00Z"},
		{Host: "a.org:443", Decision: Allowed, LastSeen: "2026-11-12T11:00:00Z"},
		{Host: "b.org:443", Decision: Allowed, LastSeen: "2026-11-12T10:00:00Z"},
		{Host: "b.org:443", Decision: Blocked, LastSeen: "2026-11-12T11:00:00Z"},
		{Host: "c.org:443", Decision: Blocked, LastSeen: "2026-11-12T10:00:00Z"},
	}}
	want := []bool{true, false, true, false, false}
	for i, e := range Resolve(log, Rules{}).Entries {
		if e.Superseded != want[i] {
			t.Errorf("entry %d (%s %s): superseded = %v, want %v", i, e.Host, e.Decision, e.Superseded, want[i])
		}
	}
}

func TestEntriesSeenAtTheSameMomentAreNeitherSuperseded(t *testing.T) {
	log := Log{Entries: []LogEntry{
		{Host: "a.org:443", Decision: Blocked, LastSeen: "2026-11-12T10:00:00Z"},
		{Host: "a.org:443", Decision: Allowed, LastSeen: "2026-11-12T10:00:00Z"},
	}}
	for _, e := range Resolve(log, Rules{}).Entries {
		if e.Superseded {
			t.Errorf("%s %s was superseded by one seen at the same moment: nothing says which is later", e.Host, e.Decision)
		}
	}
}

func TestAnEntryWhoseTimeIsNotKnownIsNeverSuperseded(t *testing.T) {
	log := Log{Entries: []LogEntry{
		{Host: "a.org:443", Decision: Blocked},
		{Host: "a.org:443", Decision: Allowed, LastSeen: "2026-11-12T11:00:00Z"},
		{Host: "b.org:443", Decision: Blocked, LastSeen: "2026-11-12T10:00:00Z"},
		{Host: "b.org:443", Decision: Allowed, LastSeen: "not a time"},
	}}
	for _, e := range Resolve(log, Rules{}).Entries {
		if e.Superseded {
			t.Errorf("%s %s was superseded without a time to say so", e.Host, e.Decision)
		}
	}
}

// A blocked host a rule now allows (Unblocked) and the allowed entry of the same host say the same: neither is the old news.
func TestUnblockedAndAllowedEntriesOfOneHostDoNotSupersedeEachOther(t *testing.T) {
	log := Log{Entries: []LogEntry{
		{Host: "a.org:443", Decision: Allowed, LastSeen: "2026-11-12T10:00:00Z"},
		{Host: "a.org:443", Decision: Blocked, LastSeen: "2026-11-12T11:00:00Z"},
	}}
	rules := Rules{Rules: []Rule{{ID: "r", Resources: []string{"a.org:443"}, Decision: Allowed}}}
	for _, e := range Resolve(log, rules).Entries {
		if e.Superseded {
			t.Errorf("%s %s superseded, though the host is allowed either way", e.Host, e.Decision)
		}
	}
}

func TestADifferentPortIsAnotherHostForSupersedingToo(t *testing.T) {
	log := Log{Entries: []LogEntry{
		{Host: "a.org:443", Decision: Blocked, LastSeen: "2026-11-12T10:00:00Z"},
		{Host: "a.org:80", Decision: Allowed, LastSeen: "2026-11-12T11:00:00Z"},
	}}
	if Resolve(log, Rules{}).Entries[0].Superseded {
		t.Error("an entry for port 443 was superseded by one for port 80")
	}
}

// Blocked, then allowed, then blocked again: the host is blocked, so the first entry says what is so, and only the allowed one is history.
func TestAnEntryThatAgreesWithTheHostsLatestEntryIsNotSupersededBecauseOfAnInBetweenOne(t *testing.T) {
	log := Log{Entries: []LogEntry{
		{Host: "a.org:443", Decision: Blocked, LastSeen: "2026-11-12T08:00:00Z", Rule: "first"},
		{Host: "a.org:443", Decision: Allowed, LastSeen: "2026-11-12T09:00:00Z"},
		{Host: "a.org:443", Decision: Blocked, LastSeen: "2026-11-12T10:00:00Z", Rule: "last"},
	}}
	want := []bool{false, true, false}
	for i, e := range Resolve(log, Rules{}).Entries {
		if e.Superseded != want[i] {
			t.Errorf("entry %d (%s at %s): superseded = %v, want %v", i, e.Decision, e.LastSeen, e.Superseded, want[i])
		}
	}
}

// The log only records requests, so a host allowed when last seen and blocked by a deny rule since (a Block from the page) still has an
// Allowed entry as its latest: the row has to say what is so now, as an Unblocked row does for a host a rule now lets through.
func TestAnAllowedHostADenyRuleBlocksNowIsBlockedNowAndSaysWhichRules(t *testing.T) {
	log := Log{Entries: []LogEntry{
		{Host: "a.org:443", Decision: Blocked, LastSeen: "2026-11-12T08:00:00Z"},
		{Host: "a.org:443", Decision: Allowed, LastSeen: "2026-11-12T09:00:00Z"},
		{Host: "ok.org:443", Decision: Allowed, LastSeen: "2026-11-12T09:00:00Z"},
	}}
	rules := Rules{Rules: []Rule{
		{ID: "deny", Resources: []string{"a.org:443"}, Decision: Blocked, Scope: "sandbox:vibe", Removable: true},
		{ID: "allow", Resources: []string{"a.org:443"}, Decision: Allowed, Scope: "global"},
	}}
	got := Resolve(log, rules).Entries
	if got[1].Decision != BlockedNow || len(got[1].BlockedBy) != 1 || got[1].BlockedBy[0].ID != "deny" || got[1].AllowedBy != nil {
		t.Errorf("the allowed entry = %+v", got[1])
	}
	if got[2].Decision != Allowed || got[2].BlockedBy != nil {
		t.Errorf("a host nothing blocks changed: %+v", got[2])
	}
	// The older blocked entry agrees with it: neither is history.
	if got[0].Superseded || got[1].Superseded {
		t.Errorf("superseded = %v, %v: both say the host is blocked", got[0].Superseded, got[1].Superseded)
	}
}
