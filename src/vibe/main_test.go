package main

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"vibe/internal/cliargs"
	"vibe/internal/fscopy"
	"vibe/internal/hostmem"
	"vibe/internal/kitspec"
	"vibe/internal/layout"
	"vibe/internal/state"
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
	const defaultSteps = 4
	const wantSteps = defaultSteps + 12 // default scripts + the profile's own
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
	t.Setenv("HOME", home)        // installBinary resolves ~/.local/bin from this
	t.Setenv("USERPROFILE", home) // ...or, on Windows, this

	writeFile(t, filepath.Join(bundle, "config.yaml"), "name: vibe\n")
	writeFile(t, filepath.Join(bundle, "settings.yaml"), "memory: 12g\n")
	writeFile(t, filepath.Join(bundle, "defaults", "install-scripts", "01-a"), "echo a\n")
	writeFile(t, filepath.Join(bundle, "profiles", "yumbi", "config.yaml"), "name: yumbi\n")
	writeFile(t, filepath.Join(bundle, "library", "mysql", "install-scripts", "01-a"), "echo mysql\n")

	vibeHome := filepath.Join(home, ".vibe")
	// a pre-existing customization that must survive the install
	writeFile(t, filepath.Join(vibeHome, "settings.yaml"), "memory: 24g\n")

	// It differs from the package's, and with no record of which release it
	// came from (so no merge) and -f (so no asking), it's left alone — and
	// the install says so, failing, rather than calling itself done.
	lay := layout.New(vibeHome, bundle)
	err := doInstall(lay, true, "")
	if err == nil || !strings.Contains(err.Error(), "left unmerged in 1 file") {
		t.Errorf("doInstall = %v, want it to report the one unsettled file", err)
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
	} else if runtime.GOOS != "windows" && info.Mode().Perm()&0o100 == 0 {
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
	t.Setenv("USERPROFILE", home)
	writeFile(t, filepath.Join(bundle, "config.yaml"), "name: vibe\n")

	lay := layout.New(filepath.Join(home, ".vibe"), bundle)
	if err := doInstall(lay, true, ""); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(home, ".vibe", "config.yaml"), "name: customized\n")
	if err := doInstall(lay, true, ""); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(home, ".vibe", "config.yaml"))
	if err != nil || !strings.Contains(string(data), "customized") {
		t.Errorf("second install overwrote the customized config.yaml: %q, %v", data, err)
	}
}

// TestDoInstallUpgradesFromOneReleaseToTheNext installs one release, edits a
// file, then installs the next: files the user never touched follow the
// package, a file changed on both sides merges, and one the user deleted
// stays deleted.
func TestDoInstallUpgradesFromOneReleaseToTheNext(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("merging needs git")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	vibeHome := filepath.Join(home, ".vibe")
	lines := "a\nb\nc\nd\ne\nf\ng\n"

	v1 := t.TempDir()
	writeFile(t, filepath.Join(v1, "settings.yaml"), lines)
	writeFile(t, filepath.Join(v1, "defaults", "install-scripts", "01-a"), "echo v1\n")
	writeFile(t, filepath.Join(v1, "defaults", "install-scripts", "02-b"), "echo b\n")
	if err := doInstall(layout.New(vibeHome, v1), true, ""); err != nil {
		t.Fatal(err)
	}

	// The user edits the end of settings.yaml and deletes a default script.
	writeFile(t, filepath.Join(vibeHome, "settings.yaml"), strings.Replace(lines, "g\n", "G (mine)\n", 1))
	os.Remove(filepath.Join(vibeHome, "defaults", "install-scripts", "02-b"))

	// The next release changes the start of settings.yaml and 01-a, and adds a file.
	v2 := t.TempDir()
	writeFile(t, filepath.Join(v2, "settings.yaml"), strings.Replace(lines, "a\n", "A (package)\n", 1))
	writeFile(t, filepath.Join(v2, "defaults", "install-scripts", "01-a"), "echo v2\n")
	writeFile(t, filepath.Join(v2, "defaults", "install-scripts", "02-b"), "echo b\n")
	writeFile(t, filepath.Join(v2, "defaults", "install-scripts", "03-new"), "echo new\n")
	if err := doInstall(layout.New(vibeHome, v2), true, "merge,keep"); err != nil {
		t.Fatal(err)
	}

	read := func(rel ...string) string {
		data, _ := os.ReadFile(filepath.Join(append([]string{vibeHome}, rel...)...))
		return string(data)
	}
	if got := read("settings.yaml"); !strings.Contains(got, "A (package)") || !strings.Contains(got, "G (mine)") {
		t.Errorf("settings.yaml wasn't merged: %q", got)
	}
	if got := read("defaults", "install-scripts", "01-a"); got != "echo v2\n" {
		t.Errorf("the untouched 01-a wasn't updated: %q", got)
	}
	if got := read("defaults", "install-scripts", "03-new"); got != "echo new\n" {
		t.Errorf("the new file wasn't copied: %q", got)
	}
	if _, err := os.Stat(filepath.Join(vibeHome, "defaults", "install-scripts", "02-b")); err == nil {
		t.Error("the deleted 02-b came back")
	}
}

func TestInstallUpdateStrategyIsCheckedFirst(t *testing.T) {
	err := doInstall(layout.New(t.TempDir(), t.TempDir()), true, "update,merge")
	if err == nil || !strings.Contains(err.Error(), "unknown --update-strategy") {
		t.Errorf("doInstall with a bad strategy = %v", err)
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

// TestReComposeNeedsAProfileThatRecordsItsFeatures covers the two ways
// --re-compose refuses rather than guessing: no profile at all, and a
// profile that was not composed from the library (hand-written, copied or
// blank), which has nothing to re-compose from.
func TestReComposeNeedsAProfileThatRecordsItsFeatures(t *testing.T) {
	lay := layout.New(t.TempDir(), t.TempDir())
	args := cliargs.Args{Force: true, Profile: "foo-browser"}

	err := doReCompose(lay, args, "foo-browser", t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "no profile 'foo-browser' to re-compose") {
		t.Errorf("error for a missing profile = %v", err)
	}

	writeFile(t, lay.HomePath("profiles", "foo-browser", "config.yaml"), "name: foo-browser\n")
	err = doReCompose(lay, args, "foo-browser", t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "does not record which features") {
		t.Errorf("error for a profile with no record = %v", err)
	}
}

// TestReComposeRejectsAFeatureThatIsGone keeps a renamed or removed feature
// from silently dropping out of a rebuilt profile.
func TestReComposeRejectsAFeatureThatIsGone(t *testing.T) {
	lay := layout.New(t.TempDir(), t.TempDir())
	writeFile(t, lay.BundlePath("library", "diffity", "install-scripts", "01-install"), "#!/bin/sh\necho hi\n")
	writeFile(t, lay.HomePath("profiles", "foo-browser", "config.yaml"),
		"# vibe: features: diffity, diffty\nname: foo-browser\n")

	err := doReCompose(lay, cliargs.Args{Force: true, Profile: "foo-browser"}, "foo-browser", t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "did you mean 'diffity'?") {
		t.Errorf("error for a vanished feature = %v", err)
	}
}

func TestPublishedURLsPreferTheLiveMapping(t *testing.T) {
	records := []state.PublishRecord{
		{Name: "diffity", ContainerPort: 5391, HostPort: 5396},
		{Name: "api", ContainerPort: 8080, HostPort: 8080},
		{Name: "api", ContainerPort: 8081, HostPort: 8082}, // not the entry's first port
	}
	live := map[int]int{5391: 5399}
	got := publishedURLs(records, live)
	if len(got) != 2 {
		t.Fatalf("publishedURLs = %+v, want one per publish entry", got)
	}
	if got[0].name != "diffity" || got[0].url() != "http://localhost:5399" || got[0].unverified {
		t.Errorf("diffity = %+v, want the live port 5399, verified", got[0])
	}
	if got[0].fileContent() != "http://localhost:5399\n" {
		t.Errorf("verified file content = %q", got[0].fileContent())
	}
	if got[1].name != "api" || got[1].url() != "http://localhost:8080" || !got[1].unverified {
		t.Errorf("api = %+v, want the recorded port 8080, marked unverified", got[1])
	}
	if c := got[1].fileContent(); !strings.HasPrefix(c, "http://localhost:8080\nunverified: ") {
		t.Errorf("unverified file content = %q", c)
	}
}

func themedKit(themes ...string) kitspec.Doc {
	var files []interface{}
	for _, name := range themes {
		files = append(files, kitspec.Doc{"path": kitspec.AgentHome + "/.claude/themes/" + name + ".json", "content": "{}"})
	}
	files = append(files, kitspec.Doc{"path": kitspec.AgentHome + "/.claude/settings.json", "content": "{}", "onlyIfMissing": true})
	return kitspec.Doc{"setup": kitspec.Doc{"files": files, "install": []interface{}{}}}
}

func themeIn(t *testing.T, doc kitspec.Doc) string {
	t.Helper()
	for _, f := range doc["setup"].(kitspec.Doc)["files"].([]interface{}) {
		entry := f.(kitspec.Doc)
		if entry["path"] == kitspec.AgentHome+"/.claude/settings.json" {
			var s map[string]interface{}
			if err := json.Unmarshal([]byte(entry["content"].(string)), &s); err != nil {
				t.Fatal(err)
			}
			theme, _ := s["theme"].(string)
			return theme
		}
	}
	t.Fatal("no settings.json in the kit")
	return ""
}

// With no terminal (as in every test), nothing is asked: the sandbox being
// replaced wins, then the profile's record, then the default — and whatever
// is settled on is what the profile records next. What counts as a theme is
// what ~/.vibe/defaults holds, not what the kit happens to carry.
func TestSetClaudeThemePrefersTheSandboxThenTheProfile(t *testing.T) {
	for _, tc := range []struct {
		name, recorded, fromSandbox, want, setting string
	}{
		{"nothing known", "", "", "default", ""},
		{"profile only", "amber", "", "amber", "custom:amber"},
		{"sandbox beats profile", "amber", "red", "red", "custom:red"},
		{"sandbox on a built-in theme", "", "light", "light", "light"},
		{"theme no longer shipped", "purple", "", "default", ""},
		{"theme only in the kit, not the defaults", "cyan", "", "default", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			lay := layout.New(t.TempDir(), t.TempDir())
			for _, name := range []string{"amber", "green", "red"} {
				writeFile(t, lay.HomePath("defaults", "agent-files", ".claude", "themes", name+".json"), "{}")
			}
			profileDir := lay.HomePath("profiles", "demo")
			writeFile(t, filepath.Join(profileDir, "config.yaml"), "name: demo\n")
			if tc.recorded != "" {
				writeFile(t, filepath.Join(profileDir, "claude-theme"), tc.recorded+"\n")
			}

			doc, err := setClaudeTheme(lay, "demo", themedKit("amber", "cyan", "green", "red"), tc.fromSandbox, true, false)
			if err != nil {
				t.Fatal(err)
			}
			if got := themeIn(t, doc); got != tc.setting {
				t.Errorf("settings.json theme = %q, want %q", got, tc.setting)
			}
			data, err := os.ReadFile(filepath.Join(profileDir, "claude-theme"))
			if err != nil || strings.TrimSpace(string(data)) != tc.want {
				t.Errorf("profile records %q (%v), want %q", data, err, tc.want)
			}
		})
	}
}

// An upgrade replaces the installed vibe even while another session is
// running it — which a plain copy over it can't do ("text file busy").
func TestReplaceFileWhileTheTargetIsRunning(t *testing.T) {
	// The running target is a copy of this test binary, which stays up
	// when told to (see TestMain) whatever it's called.
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	dest := filepath.Join(dir, "vibe")
	if runtime.GOOS == "windows" {
		dest += ".exe" // Windows only runs what's named like a program
	}
	if err := fscopy.File(self, dest); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	running := exec.Command(dest)
	running.Env = append(os.Environ(), "VIBE_TEST_STAY_RUNNING=1")
	if err := running.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { running.Process.Kill(); running.Wait() })
	time.Sleep(200 * time.Millisecond)
	if err := running.Process.Signal(syscall.Signal(0)); err != nil && runtime.GOOS != "windows" {
		t.Fatalf("the stand-in exited before the test could replace it: %v", err)
	}
	// Writing straight into it is what the plain copy used to do, and fails.
	if f, err := os.OpenFile(dest, os.O_WRONLY|os.O_TRUNC, 0); err == nil {
		f.Close()
		t.Log("this platform lets a running binary be overwritten in place; the swap is still exercised")
	}

	src := filepath.Join(dir, "new-vibe")
	writeFile(t, src, "#!/bin/sh\necho new\n")
	if err := replaceFile(src, dest); err != nil {
		t.Fatalf("replaceFile while the target runs: %v", err)
	}
	if data, _ := os.ReadFile(dest); string(data) != "#!/bin/sh\necho new\n" {
		t.Errorf("dest wasn't replaced: %q", data)
	}
	if info, _ := os.Stat(dest); runtime.GOOS != "windows" && info.Mode().Perm()&0o100 == 0 {
		t.Errorf("the replaced binary isn't executable: %v", info.Mode())
	}
	leftovers := []string{dest + ".new"}
	if runtime.GOOS != "windows" { // Windows can't delete the running old one yet
		leftovers = append(leftovers, dest+".old")
	}
	for _, leftover := range leftovers {
		if _, err := os.Stat(leftover); err == nil {
			t.Errorf("left behind %s", leftover)
		}
	}
}

// onHost stands in a machine with gib GB installed — reporting a little
// under that, as an OS does — for the rest of the test.
func onHost(t *testing.T, gib uint64) {
	t.Helper()
	old := hostmem.Total
	hostmem.Total = func() (uint64, error) { return gib*hostmem.GiB - 400<<20, nil }
	t.Cleanup(func() { hostmem.Total = old })
}

const shippedSettings = `memory: 12g
agent: claude
# ticked by default when a guided profile is created
defaultFeatures:
  - diffity
`

// TestDoInstallFirstTimeSetsMemoryForThisMachine: with nobody to ask, a
// first install keeps the package's settings, but with no more memory than
// half of this machine's.
func TestDoInstallFirstTimeSetsMemoryForThisMachine(t *testing.T) {
	onHost(t, 16)
	home := t.TempDir()
	bundle := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	writeFile(t, filepath.Join(bundle, "settings.yaml"), shippedSettings)

	vibeHome := filepath.Join(home, ".vibe")
	if err := doInstall(layout.New(vibeHome, bundle), true, ""); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(vibeHome, "settings.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if want := strings.Replace(shippedSettings, "memory: 12g", "memory: 8g", 1); string(data) != want {
		t.Errorf("settings.yaml after a first install on 16 GB:\n%s\nwant:\n%s", data, want)
	}
}

// TestDoInstallLowersAMemorySettingThisMachineCantGive: a later install
// leaves the user's settings alone, but for a memory setting over half
// this machine's.
func TestDoInstallLowersAMemorySettingThisMachineCantGive(t *testing.T) {
	home := t.TempDir()
	bundle := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	writeFile(t, filepath.Join(bundle, "settings.yaml"), shippedSettings)
	vibeHome := filepath.Join(home, ".vibe")
	lay := layout.New(vibeHome, bundle)
	if err := doInstall(lay, true, ""); err != nil {
		t.Fatal(err)
	}
	mine := strings.Replace(shippedSettings, "agent: claude", "agent: codex", 1)
	writeFile(t, filepath.Join(vibeHome, "settings.yaml"), mine)

	if err := doInstall(lay, true, ""); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(filepath.Join(vibeHome, "settings.yaml")); string(data) != mine {
		t.Errorf("a later install on a big machine changed settings.yaml:\n%s", data)
	}

	onHost(t, 16)
	if err := doInstall(lay, true, ""); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(vibeHome, "settings.yaml"))
	if want := strings.Replace(mine, "memory: 12g", "memory: 8g", 1); string(data) != want {
		t.Errorf("settings.yaml after a later install on 16 GB:\n%s\nwant:\n%s", data, want)
	}
}

func TestPickMemoryWithoutAsking(t *testing.T) {
	onHost(t, 32)
	for want, got := range map[string]string{"12g": "12g", "6g": "6g", "24g": "16g", "lots": "16g"} {
		if m := pickMemory(want, false, ""); m != got {
			t.Errorf("pickMemory(%q) on 32 GB = %q, want %q", want, m, got)
		}
	}
	hostmem.Total = func() (uint64, error) { return 0, errors.New("no idea") }
	if m := pickMemory("24g", false, ""); m != "24g" {
		t.Errorf("pickMemory on an unknown machine = %q, want it kept", m)
	}
}
