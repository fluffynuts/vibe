package main

import (
	"os"
	"path/filepath"
	"testing"

	"vibe/internal/kitspec"
	"vibe/internal/sbxtest"
)

// agentHomeSandbox puts an sbx on PATH whose exec runs the command here,
// with the agent's home standing for the returned directory.
func agentHomeSandbox(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	sbxtest.OnPath(t, "[ \"$1\" = exec ] || exit 1\nshift\n[ \"$1\" = -i ] && shift\nshift 2\n"+
		"for a; do shift; set -- \"$@\" \"$(printf %s \"$a\" | sed \"s#^"+kitspec.AgentHome+"#"+root+"#\")\"; done\n"+
		"exec \"$@\"\n")
	return root
}

// A file vibe writes in the sandbox is replaced whole, its folder made if
// need be, and nothing is left beside it.
func TestASandboxFileIsWrittenWhole(t *testing.T) {
	root := agentHomeSandbox(t)
	path := kitspec.AgentHome + "/.config/vibe/git-push.md"
	for _, content := range []string{"first, and longer\n", "second\n"} {
		if err := writeSandboxFile("box", path, content); err != nil {
			t.Fatal(err)
		}
		if data, _ := os.ReadFile(filepath.Join(root, ".config", "vibe", "git-push.md")); string(data) != content {
			t.Errorf("file = %q, want %q", data, content)
		}
	}
	if entries, _ := os.ReadDir(filepath.Join(root, ".config", "vibe")); len(entries) != 1 {
		t.Errorf("left beside it: %v", entries)
	}
}
