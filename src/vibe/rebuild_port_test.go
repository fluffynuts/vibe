package main

import (
	"context"
	"testing"

	"vibe/internal/cliargs"
	"vibe/internal/state"
)

// The page's port is remembered so its address — a bookmark, another
// machine's ssh forwards — stays the same; a rebuild mustn't forget it.
func TestARebuiltSandboxKeepsItsPagesPort(t *testing.T) {
	lay := updateLayout(t)
	inst := sandboxRecord(t, lay, "box", "p").inst
	inst.CompanionPort = 10007
	if err := state.Save(lay.Home, inst); err != nil {
		t.Fatal(err)
	}
	loggingSbx(t, "box running\\n")

	if err := rebuildSandbox(context.Background(), lay, cliargs.Args{Force: true}, "box", inst.Target, true, false); err != nil {
		t.Fatal(err)
	}
	after, _, err := state.Load(lay.Home, "box")
	if err != nil {
		t.Fatal(err)
	}
	if after.CompanionPort != 10007 {
		t.Errorf("page port after the rebuild: %d, want 10007", after.CompanionPort)
	}
}
