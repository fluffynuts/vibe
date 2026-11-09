package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// An install step runs as root with the agent's home, so an npm using its default cache fills ~/.npm with root's files and the
// agent's own npm install fails with EACCES. Giving the folder back to the agent after wasn't enough: the next step to run npm
// as root filled it again. Every step that runs npm therefore sets a cache of its own, which no order of steps can undo.
func TestEveryInstallStepThatRunsNpmGivesItACacheOfItsOwn(t *testing.T) {
	scripts, err := filepath.Glob(filepath.Join(repoRoot(t), "library", "*", "install-scripts", "*"))
	if err != nil || len(scripts) == 0 {
		t.Fatalf("no install scripts found: %v", err)
	}
	runsNpm := regexp.MustCompile(`(^|[\s;&|(])npm\s+(install|i|ci|prefix|root|exec|run|config)\b`)
	for _, script := range scripts {
		data, err := os.ReadFile(script)
		if err != nil {
			t.Fatal(err)
		}
		runs := false
		for _, line := range strings.Split(string(data), "\n") {
			if trimmed := strings.TrimSpace(line); !strings.HasPrefix(trimmed, "#") && runsNpm.MatchString(line) {
				runs = true
			}
		}
		if runs && !strings.Contains(string(data), "export npm_config_cache") {
			rel, _ := filepath.Rel(repoRoot(t), script)
			t.Errorf("%s runs npm without exporting npm_config_cache: as root, it would fill the agent's ~/.npm with root's files", rel)
		}
	}
}
