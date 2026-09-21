// Package memory backs up and restores an agent's persistent memories across
// a sandbox re-init, by mounting a host directory and copying to/from it
// with a plain `sbx exec` cp — no tar streaming through sbx exec's stdio.
package memory

import (
	"fmt"
	"os"
	"path/filepath"

	"vibe/internal/sbxrun"
)

// AgentPath is where Claude Code keeps its memories inside the sandbox. Memory
// preservation is Claude-specific: other agents have no equivalent path.
const AgentPath = "/home/agent/.claude/projects"

// Supported reports whether the given agent supports memory preservation.
func Supported(agent string) bool {
	return agent == "claude"
}

// StoreFor returns the host directory backing a sandbox's memories.
func StoreFor(memoryRoot, name string) string {
	return filepath.Join(memoryRoot, name)
}

// Presence is what asking a sandbox about its memories established.
type Presence int

const (
	// None: the sandbox answered, and has no memories worth preserving.
	None Presence = iota
	// Some: the sandbox answered, and has memories.
	Some
	// Unknown: the sandbox could not be asked — it is stopped, or the exec
	// failed. Distinct from None on purpose: reporting "no memories" for a
	// sandbox that was never asked loses them silently at the next remove.
	Unknown
)

// Probe asks a sandbox whether it holds any agent memories worth offering to
// preserve — a missing or empty AgentPath counts as None. A sandbox that
// isn't running cannot be asked; see sbxrun.Start for booting one first.
func Probe(sandboxName string) Presence {
	if !sbxrun.Reachable(sandboxName) {
		return Unknown
	}
	if sbxrun.ExecSilent(sandboxName, "sh", "-c",
		fmt.Sprintf("test -d '%s' && [ -n \"$(ls -A '%s' 2>/dev/null)\" ]", AgentPath, AgentPath)) {
		return Some
	}
	return None
}

// Backup copies memories out of a running sandbox into store, before it is
// destroyed. Returns false (no error) when the sandbox has no memories to
// preserve.
func Backup(sandboxName, store string) (bool, error) {
	if err := os.MkdirAll(store, 0o755); err != nil {
		return false, fmt.Errorf("creating memory store: %w", err)
	}
	if Probe(sandboxName) != Some {
		return false, nil
	}
	ok := sbxrun.ExecSilent(sandboxName, "sh", "-c",
		fmt.Sprintf("mkdir -p '%s' && cp -a '%s'/. '%s'/", store, AgentPath, store))
	return ok, nil
}

// Restore waits for the (re-created) sandbox to come up and copies memories
// back in from store, before the agent starts.
func Restore(sandboxName, store string) error {
	entries, err := os.ReadDir(store)
	if err != nil || len(entries) == 0 {
		return nil
	}
	if !sbxrun.WaitReachable(sandboxName, 120) {
		return fmt.Errorf("sandbox not reachable — memories left in %s", store)
	}
	ok := sbxrun.ExecSilent(sandboxName, "sh", "-c",
		fmt.Sprintf("mkdir -p '%s' && cp -a '%s'/. '%s'/", AgentPath, store, AgentPath))
	if !ok {
		return fmt.Errorf("restore failed — memories left in %s", store)
	}
	return nil
}
