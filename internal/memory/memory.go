// Package memory backs up and restores an agent's persistent memories across
// a sandbox re-init, by mounting a host directory and copying to/from it
// with a plain `sbx exec` cp — no tar streaming through sbx exec's stdio.
package memory

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"vibe/internal/sbxrun"
)

// AgentPath is where Claude Code keeps its memories inside the sandbox. Memory
// preservation is Claude-specific: other agents have no equivalent path.
const AgentPath = "/home/agent/.claude/projects"

// agentPath is AgentPath, as a variable so tests can point it elsewhere.
var agentPath = AgentPath

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
	// Every exec here is a plain argv with no shell script in it: sbx exec
	// may join its arguments into one command line, and a `sh -c "<script>"`
	// that gets split that way runs only the script's first word — which
	// fails, and so reported every sandbox as having no memories.
	if !sbxrun.ExecSilent(sandboxName, "test", "-d", agentPath) {
		return None
	}
	out, err := sbxrun.ExecCapture(sandboxName, "find", agentPath, "-mindepth", "1", "-maxdepth", "1", "-print", "-quit")
	if err != nil {
		return Unknown
	}
	if strings.Contains(out, agentPath+"/") {
		return Some
	}
	return None
}

// copyInSandbox copies the contents of dir src into dir dst, inside the
// sandbox, creating dst if need be.
func copyInSandbox(sandboxName, src, dst string) bool {
	return sbxrun.ExecSilent(sandboxName, "mkdir", "-p", dst) &&
		sbxrun.ExecSilent(sandboxName, "cp", "-a", src+"/.", dst+"/")
}

// Backup copies memories out of a running sandbox into store — before it is
// destroyed, or when a session ends. Returns false (no error) when the
// sandbox has no memories to preserve, or cannot be asked.
func Backup(sandboxName, store string) (bool, error) {
	if err := os.MkdirAll(store, 0o755); err != nil {
		return false, fmt.Errorf("creating memory store: %w", err)
	}
	if Probe(sandboxName) != Some {
		return false, nil
	}
	inside, err := sandboxPath(sandboxName, store)
	if err != nil {
		return false, err
	}
	if !copyInSandbox(sandboxName, agentPath, inside) {
		return false, fmt.Errorf("could not copy memories to %s", store)
	}
	return true, nil
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
	inside, err := sandboxPath(sandboxName, store)
	if err != nil {
		return fmt.Errorf("%w — memories left in %s", err, store)
	}
	if !copyInSandbox(sandboxName, inside, agentPath) {
		return fmt.Errorf("restore failed — memories left in %s", store)
	}
	return nil
}

// hostOS is runtime.GOOS, as a variable so tests can play at being Windows.
var hostOS = runtime.GOOS

// sandboxPath returns where the host directory store — mounted into the
// sandbox when it was created — appears inside it. Elsewhere a mount keeps
// its host path, but a Windows path (C:\Users\...) can't exist as-is in a
// Linux sandbox, and sbx doesn't document what it becomes. So on Windows
// the likely spellings are tried in turn, each checked against a marker file
// dropped into store on the host, rather than guessing.
func sandboxPath(sandboxName, store string) (string, error) {
	if hostOS != "windows" {
		return store, nil
	}
	token := make([]byte, 8)
	if _, err := rand.Read(token); err != nil {
		return "", err
	}
	marker := ".vibe-mount-check-" + hex.EncodeToString(token)
	if err := os.WriteFile(filepath.Join(store, marker), nil, 0o644); err != nil {
		return "", fmt.Errorf("marking the memory store: %w", err)
	}
	defer os.Remove(filepath.Join(store, marker))
	for _, candidate := range windowsMountCandidates(store) {
		if sbxrun.ExecSilent(sandboxName, "test", "-e", candidate+"/"+marker) {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("could not find %s inside the sandbox", store)
}

// windowsMountCandidates lists the paths a Windows host directory might be
// mounted at inside a Linux sandbox: the host path with forward slashes,
// and the /c/..., /mnt/c/... and drive-less forms other Docker tools use.
func windowsMountCandidates(store string) []string {
	slashed := strings.ReplaceAll(store, `\`, "/")
	drive, rest := "", slashed
	if len(slashed) >= 2 && slashed[1] == ':' {
		drive, rest = strings.ToLower(slashed[:1]), slashed[2:]
	}
	if !strings.HasPrefix(rest, "/") {
		rest = "/" + rest
	}
	candidates := []string{slashed}
	if drive != "" {
		candidates = append(candidates,
			"/"+drive+rest,
			"/mnt/"+drive+rest,
			"/run/desktop/mnt/host/"+drive+rest,
			"/host_mnt/"+drive+rest,
			rest)
	}
	return candidates
}
