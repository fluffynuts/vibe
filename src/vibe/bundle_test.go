package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"vibe/internal/directive"
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
//
// Only names some agent-files tree actually ships count: ~/.local/bin also
// holds what the sandbox image put there (the claude binary, say), which is
// present before any install step runs. A step that only writes a helper's
// path into configuration — registering a hook that runs long after setup,
// say — names it in a "# vibe: registers: <helper>, ..." directive.
func TestInstallScriptsDoNotCallAgentFiles(t *testing.T) {
	root := repoRoot(t)
	helpers := agentFilesHelpers(t, root)
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
			registered := map[string]bool{}
			for _, name := range strings.Split(directive.Parse(content)["registers"], ",") {
				registered[strings.TrimSpace(name)] = true
			}
			for _, line := range strings.Split(string(content), "\n") {
				code := strings.TrimSpace(line)
				if code == "" || strings.HasPrefix(code, "#") {
					continue // a comment may well explain this very rule
				}
				for _, helper := range helpers {
					if registered[helper] {
						continue
					}
					if namesHelper(code, kitspec.AgentHome+"/.local/bin/"+helper) {
						rel, _ := filepath.Rel(root, path)
						t.Errorf("%s calls an agent-files helper: %s\n"+
							"install scripts run before agent-files are deployed — inline the work instead", rel, code)
					}
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walking %s: %v", tree, err)
		}
	}
}

// namesHelper reports whether code mentions path as a whole name — not as
// the start of a longer one (diffity-url within diffity-url-hook).
func namesHelper(code, path string) bool {
	for rest := code; ; {
		i := strings.Index(rest, path)
		if i < 0 {
			return false
		}
		rest = rest[i+len(path):]
		if rest == "" || !strings.ContainsAny(rest[:1], "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-_.") {
			return true
		}
	}
}

// agentFilesHelpers lists the names every agent-files tree in the bundle
// deploys into ~/.local/bin.
func agentFilesHelpers(t *testing.T, root string) []string {
	t.Helper()
	seen := map[string]bool{}
	var names []string
	for _, tree := range []string{"defaults", "library", "profiles"} {
		err := filepath.WalkDir(filepath.Join(root, tree), func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() || kitspec.IsSidecar(d.Name()) {
				return err
			}
			if !strings.HasSuffix(filepath.ToSlash(filepath.Dir(path)), "/agent-files/.local/bin") {
				return nil
			}
			if !seen[d.Name()] {
				seen[d.Name()] = true
				names = append(names, d.Name())
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walking %s: %v", tree, err)
		}
	}
	if len(names) == 0 {
		t.Fatal("found no agent-files helpers in the bundle — has the layout changed?")
	}
	return names
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

// TestDiffityFeatureUpdatesOnStart pins the reason diffity carries startup
// scripts at all: install steps run once, at creation, so a sandbox that is
// only ever restarted would keep the CLI and skills it was born with — and
// the two are released together, so they have to move together. Composing
// the real feature has to put both update steps into the generated on-start,
// and has to leave diffity-url, the on-demand helper, out of it.
func TestDiffityFeatureUpdatesOnStart(t *testing.T) {
	lay := layout.New(t.TempDir(), repoRoot(t))
	profile := filepath.Join(t.TempDir(), "demo")
	if err := library.Compose([]string{"diffity"}, lay.FeatureDir, profile); err != nil {
		t.Fatalf("composing the diffity feature: %v", err)
	}
	onStart, err := os.ReadFile(filepath.Join(profile, "agent-files", ".local", "bin", "on-start"))
	if err != nil {
		t.Fatalf("the diffity feature generated no on-start: %v", err)
	}
	for _, want := range []string{"update-diffity", "update-diffity-skills"} {
		if !strings.Contains(string(onStart), want) {
			t.Errorf("on-start does not run %s at startup:\n%s", want, onStart)
		}
	}
	if strings.Contains(string(onStart), "diffity-url") {
		t.Errorf("on-start runs the on-demand URL helper:\n%s", onStart)
	}
}

// TestClipboardFeatureStartsOnStart pins what the clipboard feature needs
// from composition: the page's server is only ever started by on-start, so
// the generated on-start has to run start-clipboard — and has to leave the
// on-demand URL helper and the hook, which Claude Code runs itself, out of
// it. The hooks have to be registered by an install step, named in its
// registers directive since it only writes the hook's path into settings.
func TestClipboardFeatureStartsOnStart(t *testing.T) {
	lay := layout.New(t.TempDir(), repoRoot(t))
	profile := filepath.Join(t.TempDir(), "demo")
	if err := library.Compose([]string{"clipboard-bridge"}, lay.FeatureDir, profile); err != nil {
		t.Fatalf("composing the clipboard feature: %v", err)
	}
	onStart, err := os.ReadFile(filepath.Join(profile, "agent-files", ".local", "bin", "on-start"))
	if err != nil {
		t.Fatalf("the clipboard feature generated no on-start: %v", err)
	}
	if !strings.Contains(string(onStart), "start-clipboard") {
		t.Errorf("on-start does not run start-clipboard:\n%s", onStart)
	}
	for _, unwanted := range []string{"clipboard-url", "clipboard-hook"} {
		if strings.Contains(string(onStart), unwanted) {
			t.Errorf("on-start runs %s, which isn't a startup step:\n%s", unwanted, onStart)
		}
	}
	settings, err := os.ReadFile(filepath.Join(profile, "settings.yaml"))
	if err != nil || !strings.Contains(string(settings), "VIBE_CLIPBOARD_URL") {
		t.Errorf("the profile's settings.yaml doesn't publish the clipboard page:\n%s", settings)
	}
}
