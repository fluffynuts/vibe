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
