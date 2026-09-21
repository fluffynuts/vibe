package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"vibe/internal/cliargs"
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
	const wantSteps = defaultSteps + 11 // default scripts + the profile's own
	if len(installSteps) != wantSteps {
		t.Fatalf("expected %d install steps, got %d", wantSteps, len(installSteps))
	}
	first := installSteps[0].(kitspec.Doc)
	if !strings.Contains(strings.ToLower(first["description"].(string)), "apt") {
		t.Errorf("expected defaults' apt-refresh step first, got %+v", first)
	}
	firstProfileStep := installSteps[defaultSteps].(kitspec.Doc)
	if !strings.Contains(strings.ToLower(firstProfileStep["description"].(string)), "apt") {
		t.Errorf("expected profile's apt-refresh step right after %d default steps, got %+v", defaultSteps, firstProfileStep)
	}
	// diffity is a library feature now, so the template profile carries its
	// steps itself rather than getting them from the defaults
	var sawDiffityCLI, sawDiffitySkills bool
	for _, step := range installSteps {
		switch cmd := step.(kitspec.Doc)["command"].(string); {
		case strings.Contains(cmd, "npm install -g diffity"):
			sawDiffityCLI = true
		case strings.Contains(cmd, "skills add nilbuild/diffity"):
			sawDiffitySkills = true
		}
	}
	if !sawDiffityCLI || !sawDiffitySkills {
		t.Errorf("diffity steps missing from the template profile: cli=%v skills=%v", sawDiffityCLI, sawDiffitySkills)
	}

	files, _ := setup["files"].([]interface{})
	wantPaths := map[string]bool{
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

// TestDoReCreateDeletesTheOverlayProfileThenRebuilds checks the part of
// --re-create that doesn't need a real sbx binary: it deletes the overlay
// copy of the matched profile, then (via doReInit, with no profile left)
// creates a fresh blank one in its place before going on to attempt the
// sandbox rebuild, which fails harmlessly here since sbx isn't installed.
func TestDoReCreateDeletesTheOverlayProfileThenRebuilds(t *testing.T) {
	lay := layout.New(t.TempDir(), t.TempDir())
	dir := lay.HomePath("profiles", "foo-browser")
	writeFile(t, filepath.Join(dir, "config.yaml"), "name: foo-browser\n")
	writeFile(t, filepath.Join(dir, "install-scripts", "01-mine"), "echo mine\n")

	args := cliargs.Args{Force: true, Profile: "foo-browser"}
	if err := doReCreate(lay, args, "foo-browser", t.TempDir()); err == nil {
		t.Fatal("expected an error from the sandbox rebuild step (sbx is not installed here)")
	}
	if _, err := os.Stat(filepath.Join(dir, "install-scripts", "01-mine")); err == nil {
		t.Error("the old profile's install script is still there — the overlay copy was not deleted")
	}
	data, err := os.ReadFile(filepath.Join(dir, "config.yaml"))
	if err != nil {
		t.Fatalf("expected a fresh blank profile in its place: %v", err)
	}
	if !strings.Contains(string(data), "name: foo-browser") {
		t.Errorf("unexpected config.yaml after re-create:\n%s", data)
	}
}

// TestDoInstallCopiesEverythingAndLeavesCustomizationsAlone checks the
// two --install requirements: it merges config.yaml/settings.yaml,
// defaults/, profiles/ and library/ from the bundle into ~/.vibe (without
// touching anything already customized there), and it copies the running
// binary into ~/.local/bin.
func TestDoInstallCopiesEverythingAndLeavesCustomizationsAlone(t *testing.T) {
	home := t.TempDir()
	bundle := t.TempDir()
	t.Setenv("HOME", home) // installBinary resolves ~/.local/bin from this

	writeFile(t, filepath.Join(bundle, "config.yaml"), "name: vibe\n")
	writeFile(t, filepath.Join(bundle, "settings.yaml"), "memory: 12g\n")
	writeFile(t, filepath.Join(bundle, "defaults", "install-scripts", "01-a"), "echo a\n")
	writeFile(t, filepath.Join(bundle, "profiles", "yumbi", "config.yaml"), "name: yumbi\n")
	writeFile(t, filepath.Join(bundle, "library", "mysql", "install-scripts", "01-a"), "echo mysql\n")

	vibeHome := filepath.Join(home, ".vibe")
	// a pre-existing customization that must survive the install
	writeFile(t, filepath.Join(vibeHome, "settings.yaml"), "memory: 24g\n")

	lay := layout.New(vibeHome, bundle)
	if err := doInstall(lay, true); err != nil {
		t.Fatal(err)
	}

	if data, err := os.ReadFile(filepath.Join(vibeHome, "settings.yaml")); err != nil || !strings.Contains(string(data), "24g") {
		t.Errorf("existing settings.yaml was overwritten: %q, %v", data, err)
	}
	if _, err := os.ReadFile(filepath.Join(vibeHome, "config.yaml")); err != nil {
		t.Errorf("expected config.yaml copied in: %v", err)
	}
	for _, p := range []string{
		filepath.Join(vibeHome, "defaults", "install-scripts", "01-a"),
		filepath.Join(vibeHome, "profiles", "yumbi", "config.yaml"),
		filepath.Join(vibeHome, "library", "mysql", "install-scripts", "01-a"),
	} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("expected %s copied in: %v", p, err)
		}
	}

	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	dest := filepath.Join(home, ".local", "bin", filepath.Base(exe))
	if info, err := os.Stat(dest); err != nil {
		t.Errorf("expected the binary copied to %s: %v", dest, err)
	} else if info.Mode().Perm()&0o100 == 0 {
		t.Errorf("expected the copied binary to be executable, got %v", info.Mode())
	}
}

// TestDoInstallIsSafeToRunTwice re-running --install (e.g. after fetching a
// newer bundle release) must not clobber a file the user edited in ~/.vibe
// after the first install.
func TestDoInstallIsSafeToRunTwice(t *testing.T) {
	home := t.TempDir()
	bundle := t.TempDir()
	t.Setenv("HOME", home)
	writeFile(t, filepath.Join(bundle, "config.yaml"), "name: vibe\n")

	lay := layout.New(filepath.Join(home, ".vibe"), bundle)
	if err := doInstall(lay, true); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(home, ".vibe", "config.yaml"), "name: customized\n")
	if err := doInstall(lay, true); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(home, ".vibe", "config.yaml"))
	if err != nil || !strings.Contains(string(data), "customized") {
		t.Errorf("second install overwrote the customized config.yaml: %q, %v", data, err)
	}
}

func TestWarnIfNotOnPathNeverFails(t *testing.T) {
	if err := warnIfNotOnPath(t.TempDir()); err != nil {
		t.Errorf("warnIfNotOnPath must never fail: %v", err)
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

// TestGuidedProfileChecksDefaultFeatures covers the two ways settings.yaml's
// defaultFeatures list is read before the picker is ever drawn: a name the
// library doesn't have stops everything with the closest real name
// suggested, and a name it does have gets through to the prompt (which there
// is no terminal for here, so it aborts — the point is that it got that far).
func TestGuidedProfileChecksDefaultFeatures(t *testing.T) {
	for _, tc := range []struct{ name, wanted, wantErr string }{
		{"typo", "diffty", "did you mean 'diffity'?"},
		{"unknown", "postgres", "the library has: diffity"},
		{"known", "diffity", "aborted"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			lay := layout.New(t.TempDir(), t.TempDir())
			writeFile(t, lay.BundlePath("library", "diffity", "install-scripts", "01-install"), "#!/bin/sh\necho hi\n")
			writeFile(t, lay.HomePath("settings.yaml"), "defaultFeatures:\n  - "+tc.wanted+"\n")

			err := ensureGuidedProfile(lay, "demo")
			if err == nil {
				t.Fatal("expected an error")
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("error = %v, want it to mention %q", err, tc.wantErr)
			}
		})
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
