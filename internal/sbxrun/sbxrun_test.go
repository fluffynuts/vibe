package sbxrun

import (
	"testing"
	"time"
)

// TestStartGivesUpOnASandboxItCannotBoot checks Start's failure path: with no
// such sandbox (or no sbx at all) it reports false promptly rather than
// waiting out startTimeout on something that is never going to answer.
func TestStartGivesUpOnASandboxItCannotBoot(t *testing.T) {
	done := make(chan bool, 1)
	go func() { done <- Start("vibe-test-no-such-sandbox-3f9c1") }()
	select {
	case started := <-done:
		if started {
			t.Error("Start reported a nonexistent sandbox as running")
		}
	case <-time.After(30 * time.Second):
		t.Fatal("Start hung on a sandbox that cannot boot")
	}
}
