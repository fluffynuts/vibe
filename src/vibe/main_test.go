package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"vibe/internal/kitspec"
	"vibe/internal/layout"
)

// bundledProfile is the profile the repo ships that the golden-path tests
// build: the .NET/MySQL/RabbitMQ/Elasticsearch/Redis template.
const bundledProfile = "template_dotnet-mysql-rabbitmq-elasticsearch-redis"

func repoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	// cmd/vibe -> cmd -> repo root
	return filepath.Dir(filepath.Dir(wd))
}

// TestLoadKitBundledProfile is a golden-path regression test: it builds the
// kit spec for the bundled template profile end-to-end (base + defaults +
// profile) and
// checks it carries every install step, file and startup behavior the
// original hand-written vibe.sh embedded.
func TestLoadKitBundledProfile(t *testing.T) {
	root := repoRoot(t)
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
	if len(merged.Publish) != 1 || merged.Publish[0].Name != "diffity" || merged.Publish[0].UrlEnv != "VIBE_DIFFITY_URL" {
		t.Errorf("unexpected Publish: %+v", merged.Publish)
	}

	setup, _ := doc["setup"].(kitspec.Doc)
	if setup == nil {
		t.Fatal("expected setup section in generated doc")
	}

	installSteps, _ := setup["install"].([]interface{})
	const defaultSteps = 3
	const wantSteps = defaultSteps + 9 // default scripts + the profile's own
	if len(installSteps) != wantSteps {
		t.Fatalf("expected %d install steps, got %d", wantSteps, len(installSteps))
	}
	first := installSteps[0].(kitspec.Doc)
	if !strings.Contains(first["description"].(string), "setup log") {
		t.Errorf("expected defaults' start-log step first, got %+v", first)
	}
	firstProfileStep := installSteps[defaultSteps].(kitspec.Doc)
	if !strings.Contains(strings.ToLower(firstProfileStep["description"].(string)), "apt") {
		t.Errorf("expected profile's apt-refresh step right after %d default steps, got %+v", defaultSteps, firstProfileStep)
	}

	files, _ := setup["files"].([]interface{})
	wantPaths := map[string]bool{
		"/home/agent/.local/bin/install-diffity":     false,
		"/home/agent/.local/bin/add-diffity-skills":  false,
		"/home/agent/.claude/settings.json":          false,
		"/home/agent/.local/bin/start-rabbit":        false,
		"/home/agent/.local/bin/link-nuget":          false,
		"/home/agent/.local/bin/start-elasticsearch": false,
		"/home/agent/.local/bin/on-start":            false,
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

	startup, _ := doc["startup"].([]interface{})
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
	if !strings.Contains(content, "Diffity") || !strings.Contains(content, "Toolchain") {
		t.Error("expected agentInstructions to contain both the base and profile sections")
	}

	if _, err := kitspec.Marshal(doc); err != nil {
		t.Fatalf("Marshal: %v", err)
	}
}

// TestEnsureProfileExisting leaves a profile that is already there alone.
func TestEnsureProfileExisting(t *testing.T) {
	lay := layout.New(t.TempDir(), repoRoot(t))
	if err := ensureProfile(lay, bundledProfile, false); err != nil {
		t.Fatalf("ensureProfile on an existing profile: %v", err)
	}
	if _, err := os.Stat(lay.HomePath("profiles")); err == nil {
		t.Error("ensureProfile wrote to the overlay for a profile that already exists")
	}
}

// TestEnsureProfileCreatesBlank covers the two paths that need no prompting:
// an empty bundle (nothing to copy) and --force.
func TestEnsureProfileCreatesBlank(t *testing.T) {
	for _, tc := range []struct {
		name        string
		seed, force bool
	}{
		{"no profiles to copy", false, false},
		{"force", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			lay := layout.New(t.TempDir(), t.TempDir())
			if tc.seed {
				dir := lay.BundlePath("profiles", "yumbi")
				if err := os.MkdirAll(dir, 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte("name: yumbi\n"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			if err := ensureProfile(lay, "foo-browser", tc.force); err != nil {
				t.Fatalf("ensureProfile: %v", err)
			}
			// the new profile lands in the overlay, not the bundle
			if got, want := lay.ProfileDir("foo-browser"), lay.HomePath("profiles", "foo-browser"); got != want {
				t.Errorf("created in %s, want %s", got, want)
			}
			// and it loads: a blank profile is enough to build a kit
			if _, _, err := loadKit(lay, "foo-browser"); err != nil {
				t.Fatalf("loadKit on the created profile: %v", err)
			}
		})
	}
}

// TestEnsureProfileRejectsBadName refuses to create a profile whose name
// could escape the profiles directory.
func TestEnsureProfileRejectsBadName(t *testing.T) {
	if err := ensureProfile(layout.New(t.TempDir(), t.TempDir()), "../evil", true); err == nil {
		t.Fatal("expected an error for a path-escaping profile name")
	}
}

// TestLoadKitOverlayWins builds the bundled profile through an overlay
// that overrides the base settings, one default install script and the
// profile itself, and checks each override is the one that reaches the kit.
func TestLoadKitOverlayWins(t *testing.T) {
	lay := layout.New(t.TempDir(), repoRoot(t))

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
	// it holds run, in their own numeric order, and the bundle's diffity
	// steps are gone rather than layered underneath
	want := []string{"my own setup log", "99-extra"}
	if !reflect.DeepEqual(descriptions, want) {
		t.Errorf("install steps = %v, want exactly the overlay's %v", descriptions, want)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
