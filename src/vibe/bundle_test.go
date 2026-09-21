package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"vibe/internal/kitspec"
	"vibe/internal/layout"
	"vibe/internal/library"
	"vibe/internal/settings"
)

// TestInstallScriptsDoNotCallAgentFiles guards the whole bundle against the
// one ordering rule install scripts have to live by: sbx deploys setup.files
// only after setup.install has run, so a step that invokes a helper from an
// agent-files tree runs before that helper exists and fails with "not found"
// every time — silently, since setup steps are deliberately non-fatal. The
// diffity CLI and its skills were installed that way and never landed in any
// sandbox. agent-files scripts belong to on-start, not to install steps.
func TestInstallScriptsDoNotCallAgentFiles(t *testing.T) {
	root := repoRoot(t)
	for _, tree := range []string{"defaults", "library", "profiles"} {
		err := filepath.WalkDir(filepath.Join(root, tree), func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() || kitspec.IsSidecar(d.Name()) {
				return err
			}
			if filepath.Base(filepath.Dir(path)) != "install-scripts" {
				return nil
			}
			content, readErr := os.ReadFile(path)
			if readErr != nil {
				return readErr
			}
			for _, line := range strings.Split(string(content), "\n") {
				code := strings.TrimSpace(line)
				if code == "" || strings.HasPrefix(code, "#") {
					continue // a comment may well explain this very rule
				}
				if strings.Contains(code, kitspec.AgentHome+"/.local/bin/") {
					rel, _ := filepath.Rel(root, path)
					t.Errorf("%s calls an agent-files helper: %s\n"+
						"install scripts run before agent-files are deployed — inline the work instead", rel, code)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walking %s: %v", tree, err)
		}
	}
}

// TestBundleDefaultFeaturesExist keeps the shipped settings.yaml honest: a
// defaultFeatures entry naming a feature the bundle doesn't have would stop
// guided profile creation dead, which is the right behavior for a user's own
// typo and an embarrassing one to ship.
func TestBundleDefaultFeaturesExist(t *testing.T) {
	lay := layout.New("", repoRoot(t))
	base, err := settings.Load(lay.File("settings.yaml"))
	if err != nil {
		t.Fatalf("loading the bundle's settings.yaml: %v", err)
	}
	if len(base.DefaultFeatures) == 0 {
		t.Skip("the bundle ships no defaultFeatures")
	}
	if err := library.Validate(base.DefaultFeatures, lay.Features()); err != nil {
		t.Errorf("settings.yaml defaultFeatures: %v", err)
	}
}
