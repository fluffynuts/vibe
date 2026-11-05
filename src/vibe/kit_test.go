package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"vibe/internal/kitspec"
	"vibe/internal/layout"
	"vibe/internal/library"
)

// bundledProfile is the profile goldenBundle composes: the base features,
// and the .NET, MySQL, RabbitMQ, Elasticsearch and Redis ones, of the repo's
// library.
const bundledProfile = "golden"

// goldenBundle is a bundle made of the repo's own base config, defaults and
// library, shipping one profile composed from the features a .NET project
// would pick — the way a guided profile is made. It returns the bundle's root
// and how many install scripts the profile came out with.
func goldenBundle(t *testing.T) (root string, profileSteps int) {
	t.Helper()
	repo := repoRoot(t)
	root = t.TempDir()
	for _, name := range []string{"config.yaml", "settings.yaml", "defaults", "library"} {
		if err := os.Symlink(filepath.Join(repo, name), filepath.Join(root, name)); err != nil {
			t.Fatal(err)
		}
	}
	lay := layout.New(t.TempDir(), repo)
	dir := filepath.Join(root, "profiles", bundledProfile)
	// The base features first, as a guided profile is composed.
	features := append(lay.BaseFeatures(), "dotnet", "mysql-8.4", "rabbitmq", "elasticsearch", "redis")
	if err := library.Compose(features, lay.FeatureDir, dir); err != nil {
		t.Fatalf("composing the golden profile: %v", err)
	}
	scripts, err := os.ReadDir(filepath.Join(dir, "install-scripts"))
	if err != nil {
		t.Fatal(err)
	}
	return root, len(scripts)
}

// TestLoadKitBundledProfile is a golden-path regression test: it builds the
// kit spec for a profile composed from the library end-to-end (base +
// defaults + profile) and checks it carries every install step, file and
// startup behavior the original hand-written vibe.sh embedded.
func TestLoadKitBundledProfile(t *testing.T) {
	root, profileSteps := goldenBundle(t)
	doc, merged, err := loadKit(layout.New(t.TempDir(), root), bundledProfile)
	if err != nil {
		t.Fatalf("loadKit: %v", err)
	}

	if merged.Memory != "12g" {
		t.Errorf("Memory = %q, want 12g", merged.Memory)
	}
	if merged.Agent != "claude" {
		t.Errorf("Agent = %q, want claude", merged.Agent)
	}
	if merged.NugetDir == "" {
		t.Error("expected NugetDir from profile settings.yaml")
	}

	setup, _ := doc["setup"].(kitspec.Doc)
	if setup == nil {
		t.Fatal("expected setup section in generated doc")
	}

	installSteps, _ := setup["install"].([]interface{})
	const defaultSteps = 4
	wantSteps := defaultSteps + profileSteps // default scripts + the profile's own
	if len(installSteps) != wantSteps {
		t.Fatalf("expected %d install steps, got %d", wantSteps, len(installSteps))
	}
	first := installSteps[0].(kitspec.Doc)
	if !strings.Contains(strings.ToLower(first["description"].(string)), "apt") {
		t.Errorf("expected defaults' apt-refresh step first, got %+v", first)
	}

	files, _ := setup["files"].([]interface{})
	wantPaths := map[string]bool{
		"/home/agent/.claude/settings.json":          false,
		"/home/agent/.local/bin/start-rabbit":        false,
		"/home/agent/.local/bin/link-nuget":          false,
		"/home/agent/.local/bin/start-elasticsearch": false,
		"/home/agent/.local/bin/on-start":            false,
		// the review feature's command, a base feature's file
		"/home/agent/.local/bin/vibe-review": false,
	}
	for _, f := range files {
		entry := f.(kitspec.Doc)
		p := entry["path"].(string)
		if _, ok := wantPaths[p]; ok {
			wantPaths[p] = true
		}
		switch p {
		case "/home/agent/.claude/settings.json":
			if entry["onlyIfMissing"] != true {
				t.Errorf("settings.json onlyIfMissing should be true via sidecar, got %v", entry["onlyIfMissing"])
			}
			if strings.Contains(entry["content"].(string), "vibe:") {
				t.Error("directive text must not leak into deployed content")
			}
		case "/home/agent/.local/bin/on-start":
			if entry["mode"] != "0755" {
				t.Errorf("on-start should be executable, got %v", entry["mode"])
			}
		}
	}
	for p, found := range wantPaths {
		if !found {
			t.Errorf("expected file entry %s not found", p)
		}
	}

	startup, _ := setup["startup"].([]interface{})
	if len(startup) != 1 {
		t.Fatalf("expected exactly one startup step (on-start), got %d", len(startup))
	}
	step := startup[0].(kitspec.Doc)
	if step["description"] != "running startup script" {
		t.Errorf("unexpected startup description: %v", step["description"])
	}
	cmd, _ := step["command"].([]interface{})
	if len(cmd) != 1 || cmd[0] != "/home/agent/.local/bin/on-start" {
		t.Errorf("unexpected startup command: %v", step["command"])
	}
	if step["user"] != "0" || step["background"] != true {
		t.Errorf("unexpected startup user/background: %+v", step)
	}

	ai, _ := doc["agentInstructions"].(kitspec.Doc)
	content, _ := ai["content"].(string)
	if !strings.Contains(content, "## Reviews") {
		t.Error("expected agentInstructions to carry the review feature's section")
	}

	if _, err := kitspec.Marshal(doc); err != nil {
		t.Fatalf("Marshal: %v", err)
	}
}

// TestLoadKitOverlayWins builds the bundled profile through an overlay
// that overrides the base settings, one default install script and the
// profile itself, and checks each override is the one that reaches the kit.
func TestLoadKitOverlayWins(t *testing.T) {
	root, _ := goldenBundle(t)
	lay := layout.New(t.TempDir(), root)

	writeFile(t, lay.HomePath("settings.yaml"), "memory: 24g\nagent: claude\n")
	writeFile(t, lay.HomePath("defaults", "install-scripts", "01-ensure-local-bin"), "#!/bin/sh\n# vibe: description: my own setup log\necho mine\n")
	writeFile(t, lay.HomePath("defaults", "install-scripts", "99-extra"), "#!/bin/sh\necho extra\n")
	writeFile(t, lay.HomePath("profiles", bundledProfile, "config.yaml"), "name: mine\ndisplayName: my own profile\n")

	doc, merged, err := loadKit(lay, bundledProfile)
	if err != nil {
		t.Fatalf("loadKit: %v", err)
	}
	if merged.Memory != "24g" {
		t.Errorf("Memory = %q, want the overlay's 24g", merged.Memory)
	}
	if merged.NugetDir != "" {
		t.Errorf("NugetDir = %q: the overlay's profile replaced the bundle's whole, so its settings.yaml is gone", merged.NugetDir)
	}
	if doc["displayName"] != "my own profile" {
		t.Errorf("displayName = %v, want the overlay profile's", doc["displayName"])
	}

	setup, _ := doc["setup"].(kitspec.Doc)
	steps, _ := setup["install"].([]interface{})
	var descriptions []string
	for _, step := range steps {
		descriptions = append(descriptions, step.(kitspec.Doc)["description"].(string))
	}
	first := steps[0].(kitspec.Doc)
	if first["description"] != "my own setup log" || !strings.Contains(first["command"].(string), "echo mine") {
		t.Errorf("the overlay's 01-ensure-local-bin did not replace the bundle's: %+v", first)
	}
	// the overlay's defaults replace the bundle's whole: only the two scripts
	// it holds run, in their own numeric order, and the bundle's own
	// steps are gone rather than layered underneath
	want := []string{"my own setup log", "99-extra"}
	if !reflect.DeepEqual(descriptions, want) {
		t.Errorf("install steps = %v, want exactly the overlay's %v", descriptions, want)
	}
}

func TestTheKitSbxIsHandedMergesVibesDenyRulesIntoTheAgentsSettings(t *testing.T) {
	root, _ := goldenBundle(t)
	doc, _, err := loadKit(layout.New(t.TempDir(), root), bundledProfile)
	if err != nil {
		t.Fatal(err)
	}
	text, err := kitspec.MarshalV3(doc, "claude")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(text), "Read(./.env)") || !strings.Contains(string(text), "Merge vibe's settings") {
		t.Error("the kit has no step merging vibe's deny rules into the agent's settings.json")
	}
}

func TestEveryProfilesKitInstallsTzdata(t *testing.T) {
	root, _ := goldenBundle(t)
	doc, _, err := loadKit(layout.New(t.TempDir(), root), bundledProfile)
	if err != nil {
		t.Fatal(err)
	}
	text, err := kitspec.MarshalV3(doc, "claude")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(text), "Install tzdata") {
		t.Error("the kit has no step installing tzdata")
	}
}

func TestAFailedCreateKeepsOnlyTheLatestKitForASandboxName(t *testing.T) {
	tmp := t.TempDir()
	for _, v := range []string{"TMPDIR", "TMP", "TEMP"} {
		t.Setenv(v, tmp)
	}
	doc := kitspec.Doc{"schemaVersion": "2", "kind": "mixin", "name": "x"}
	keep := func(name, marker string) string {
		dir, err := writeKit(doc, name, "claude")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "marker"), []byte(marker), 0o600); err != nil {
			t.Fatal(err)
		}
		kept, err := keepFailedKit(dir, name)
		if err != nil {
			t.Fatal(err)
		}
		return kept
	}
	first := keep("box", "first")
	other := keep("other", "other's")
	second := keep("box", "second")
	if first != second {
		t.Errorf("a second failure kept the kit somewhere else: %s, then %s", first, second)
	}
	if got, _ := os.ReadFile(filepath.Join(second, "marker")); string(got) != "second" {
		t.Errorf("the kit kept is %q's, want the latest", got)
	}
	if _, err := os.Stat(filepath.Join(second, "spec.yaml")); err != nil {
		t.Errorf("the kept kit has no spec.yaml: %v", err)
	}
	if _, err := os.Stat(other); err != nil {
		t.Errorf("another sandbox's kept kit was touched: %v", err)
	}
	forgetFailedKit("box")
	if _, err := os.Stat(second); !os.IsNotExist(err) {
		t.Errorf("the kept kit is still there after the sandbox was made: %v", err)
	}
	if _, err := os.Stat(other); err != nil {
		t.Errorf("forgetting one sandbox's kit removed another's: %v", err)
	}
}
