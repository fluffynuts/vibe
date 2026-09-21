package memory

import "testing"

// nonexistentSandbox is a name no sandbox on this machine can have, so
// asking about it always fails the same way whether or not sbx is installed.
const nonexistentSandbox = "vibe-test-no-such-sandbox-3f9c1"

func TestSupportedIsClaudeOnly(t *testing.T) {
	if !Supported("claude") {
		t.Error("claude should support memory preservation")
	}
	if Supported("codex") || Supported("") {
		t.Error("only claude has an equivalent memory path")
	}
}

// TestProbeSaysUnknownRatherThanNoneWhenItCannotAsk is the whole point of
// Presence having three states: a sandbox that can't be reached (stopped, or
// no sbx at all) must not be reported as having no memories, or --re-init
// destroys it without ever offering to keep them.
func TestProbeSaysUnknownRatherThanNoneWhenItCannotAsk(t *testing.T) {
	if got := Probe(nonexistentSandbox); got != Unknown {
		t.Errorf("Probe on an unreachable sandbox = %v, want Unknown", got)
	}
}

// TestBackupOfAnUnreachableSandboxReportsNothingSaved makes sure Unknown is
// never mistaken for "backed up" further down the line.
func TestBackupOfAnUnreachableSandboxReportsNothingSaved(t *testing.T) {
	saved, err := Backup(nonexistentSandbox, t.TempDir())
	if err != nil {
		t.Fatalf("Backup: %v", err)
	}
	if saved {
		t.Error("Backup claimed to have saved memories it could not read")
	}
}

func TestStoreForIsPerSandbox(t *testing.T) {
	if got, want := StoreFor("/root", "alpha"), "/root/alpha"; got != want {
		t.Errorf("StoreFor = %q, want %q", got, want)
	}
}
