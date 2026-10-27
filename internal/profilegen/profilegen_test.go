package profilegen

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"vibe/internal/layout"
	"vibe/internal/userprefs"
)

// testLayout builds a throwaway overlay + bundle, the bundle holding one
// "widget"-shaped profile.
func testLayout(t *testing.T) layout.Layout {
	t.Helper()
	l := layout.New(t.TempDir(), t.TempDir())
	dir := l.BundlePath("profiles", "widget")
	if err := os.MkdirAll(filepath.Join(dir, "install-scripts"), 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(dir, "config.yaml"), "# a comment\nname: widget\ndisplayName: Widget\n", 0o644)
	write(t, filepath.Join(dir, "settings.yaml"), "nugetDir: ~/.nuget\n", 0o644)
	write(t, filepath.Join(dir, "install-scripts", "01-hello"), "#!/bin/sh\necho hi\n", 0o755)
	return l
}

func write(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
}

func TestCreateBlankLandsInTheOverlay(t *testing.T) {
	l := testLayout(t)
	dir, err := CreateBlank(l, "phoenix")
	if err != nil {
		t.Fatal(err)
	}
	if want := l.HomePath("profiles", "phoenix"); dir != want {
		t.Errorf("created in %s, want %s", dir, want)
	}
	data, err := os.ReadFile(filepath.Join(dir, "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "name: phoenix") {
		t.Errorf("blank config.yaml does not name the profile:\n%s", data)
	}
	for _, sub := range []string{"install-scripts", "agent-files"} {
		if info, err := os.Stat(filepath.Join(dir, sub)); err != nil || !info.IsDir() {
			t.Errorf("expected %s/ in the blank profile: %v", sub, err)
		}
	}
	if !l.ProfileExists("phoenix") {
		t.Error("the created profile is not visible through the layout")
	}
}

func TestCopyFromBundleIntoTheOverlay(t *testing.T) {
	l := testLayout(t)
	dir, err := CopyFrom(l, "widget", "widget-two")
	if err != nil {
		t.Fatal(err)
	}
	if want := l.HomePath("profiles", "widget-two"); dir != want {
		t.Errorf("created in %s, want %s", dir, want)
	}
	data, err := os.ReadFile(filepath.Join(dir, "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "name: widget-two") {
		t.Errorf("copied config.yaml was not renamed:\n%s", data)
	}
	if !strings.Contains(string(data), "# a comment") || !strings.Contains(string(data), "displayName: Widget") {
		t.Errorf("copy lost comments or other fields:\n%s", data)
	}
	if _, err := os.Stat(filepath.Join(dir, "settings.yaml")); err != nil {
		t.Errorf("settings.yaml not copied: %v", err)
	}
	info, err := os.Stat(filepath.Join(dir, "install-scripts", "01-hello"))
	if err != nil {
		t.Fatalf("install script not copied: %v", err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o100 == 0 {
		t.Errorf("install script lost its executable bit: %v", info.Mode())
	}
	orig, err := os.ReadFile(l.BundlePath("profiles", "widget", "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(orig), "name: widget\n") {
		t.Errorf("the bundled profile was modified:\n%s", orig)
	}
}

func TestImportToHomeKeepsTheName(t *testing.T) {
	l := testLayout(t)
	dir, err := ImportToHome(l, "widget")
	if err != nil {
		t.Fatal(err)
	}
	if want := l.HomePath("profiles", "widget"); dir != want {
		t.Errorf("imported to %s, want %s", dir, want)
	}
	data, err := os.ReadFile(filepath.Join(dir, "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "name: widget") {
		t.Errorf("import changed the profile:\n%s", data)
	}
	// the overlay copy is now the one that counts
	if got := l.ProfileDir("widget"); got != dir {
		t.Errorf("ProfileDir = %s, want the imported %s", got, dir)
	}
}

func TestCreateGuidedComposesTheChosenFeatures(t *testing.T) {
	l := testLayout(t)
	write(t, mkdirAndPath(t, l.BundlePath("library", "mysql", "install-scripts"), "01-install-mysql"),
		"#!/bin/sh\necho install-mysql\n", 0o644)
	write(t, mkdirAndPath(t, l.BundlePath("library", "mysql", "agent-files", ".local", "bin"), "start-mysql"),
		"#!/bin/sh\necho start-mysql\n", 0o644)
	write(t, mkdirAndPath(t, l.BundlePath("library", "rabbitmq", "install-scripts"), "01-install-rabbitmq"),
		"#!/bin/sh\necho install-rabbitmq\n", 0o644)

	dir, err := CreateGuided(l, "phoenix", []string{"mysql", "rabbitmq"})
	if err != nil {
		t.Fatal(err)
	}
	if want := l.HomePath("profiles", "phoenix"); dir != want {
		t.Errorf("created in %s, want %s", dir, want)
	}
	if _, err := os.ReadFile(filepath.Join(dir, "config.yaml")); err != nil {
		t.Errorf("expected a config.yaml naming the profile: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "install-scripts", "01-install-mysql")); err != nil {
		t.Errorf("expected mysql's install script first: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "install-scripts", "02-install-rabbitmq")); err != nil {
		t.Errorf("expected rabbitmq's install script offset after mysql's: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "agent-files", ".local", "bin", "start-mysql")); err != nil {
		t.Errorf("expected start-mysql copied in: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "agent-files", ".local", "bin", "on-start")); err != nil {
		t.Errorf("expected a generated on-start: %v", err)
	}
}

// TestCreateGuidedUsesTheOverlaysOverrideForOneFeatureOnly checks the
// actual requirement: overriding a single feature in the overlay must not
// hide any other feature the bundle ships.
func TestCreateGuidedUsesTheOverlaysOverrideForOneFeatureOnly(t *testing.T) {
	l := testLayout(t)
	write(t, mkdirAndPath(t, l.BundlePath("library", "mysql", "install-scripts"), "01-install-mysql"),
		"#!/bin/sh\necho bundled-mysql\n", 0o644)
	write(t, mkdirAndPath(t, l.BundlePath("library", "rabbitmq", "install-scripts"), "01-install-rabbitmq"),
		"#!/bin/sh\necho install-rabbitmq\n", 0o644)
	write(t, mkdirAndPath(t, l.HomePath("library", "mysql", "install-scripts"), "01-install-mysql"),
		"#!/bin/sh\necho my-own-mysql\n", 0o644)

	dir, err := CreateGuided(l, "phoenix", []string{"mysql", "rabbitmq"})
	if err != nil {
		t.Fatal(err)
	}
	mysqlScript, err := os.ReadFile(filepath.Join(dir, "install-scripts", "01-install-mysql"))
	if err != nil || string(mysqlScript) != "#!/bin/sh\necho my-own-mysql\n" {
		t.Errorf("expected the overlay's mysql override, got %q, %v", mysqlScript, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "install-scripts", "02-install-rabbitmq")); err != nil {
		t.Errorf("expected the bundle's rabbitmq still used, unaffected by mysql's override: %v", err)
	}
}

func mkdirAndPath(t *testing.T, dir, name string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(dir, name)
}

func TestCopyFromUnknownSource(t *testing.T) {
	if _, err := CopyFrom(testLayout(t), "nope", "phoenix"); err == nil {
		t.Fatal("expected an error copying from a missing profile")
	}
}

func TestValidateRejectsPathEscapes(t *testing.T) {
	for _, bad := range []string{"../evil", "a/b", "/abs", "", ".hidden", "Upper"} {
		if err := Validate(bad); err == nil {
			t.Errorf("Validate(%q) = nil, want an error", bad)
		}
	}
	for _, good := range []string{"widget", "foo-browser", "a1", "x_y.z"} {
		if err := Validate(good); err != nil {
			t.Errorf("Validate(%q) = %v, want nil", good, err)
		}
	}
}

// TestComposedFromRoundTrips is what --re-compose stands on: a guided
// profile has to be able to say later which features it was made of, in the
// order they were composed, and composing must not lose the record.
func TestComposedFromRoundTrips(t *testing.T) {
	l := testLayout(t)
	write(t, mkdirAndPath(t, l.BundlePath("library", "mysql", "install-scripts"), "01-install-mysql"),
		"#!/bin/sh\necho install-mysql\n", 0o644)
	write(t, mkdirAndPath(t, l.BundlePath("library", "dashboard"), "config.yaml"),
		"ports:\n  - container: 5391\n", 0o644)

	dir, err := CreateGuided(l, "phoenix", []string{"dashboard", "mysql"})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := ComposedFrom(dir), []string{"dashboard", "mysql"}; !reflect.DeepEqual(got, want) {
		t.Errorf("ComposedFrom = %v, want %v", got, want)
	}
	config := read(t, filepath.Join(dir, "config.yaml"))
	if !strings.Contains(config, "# vibe: features: dashboard, mysql") {
		t.Errorf("the record should be readable in the file itself:\n%s", config)
	}
	// it is a comment, so it must not have become a YAML key
	if strings.Contains(config, "features:\n") {
		t.Errorf("the record leaked into the kit fragment:\n%s", config)
	}

	// re-composing replaces the record rather than stacking another one
	if _, err := CreateGuided(l, "phoenix", []string{"mysql"}); err != nil {
		t.Fatal(err)
	}
	if got, want := ComposedFrom(dir), []string{"mysql"}; !reflect.DeepEqual(got, want) {
		t.Errorf("ComposedFrom after re-composing = %v, want %v", got, want)
	}
	if n := strings.Count(read(t, filepath.Join(dir, "config.yaml")), "# vibe: features:"); n != 1 {
		t.Errorf("found %d feature records, want exactly 1", n)
	}
}

// TestComposedFromIsEmptyForAProfileThatWasNotComposed keeps --re-compose
// from guessing at a profile written by hand or created blank.
func TestComposedFromIsEmptyForAProfileThatWasNotComposed(t *testing.T) {
	l := testLayout(t)
	dir, err := CreateBlank(l, "phoenix")
	if err != nil {
		t.Fatal(err)
	}
	if got := ComposedFrom(dir); got != nil {
		t.Errorf("ComposedFrom on a blank profile = %v, want nil", got)
	}
	if got := ComposedFrom(l.BundlePath("profiles", "widget")); got != nil {
		t.Errorf("ComposedFrom on a hand-written profile = %v, want nil", got)
	}
	if got := ComposedFrom(filepath.Join(t.TempDir(), "nope")); got != nil {
		t.Errorf("ComposedFrom on a missing profile = %v, want nil", got)
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func baseLayout(t *testing.T) layout.Layout {
	t.Helper()
	l := testLayout(t)
	write(t, filepath.Join(mkdir(t, l.BundlePath("library", "base", "clip", "agent-files")), "clip.txt"), "clip\n", 0o644)
	write(t, filepath.Join(mkdir(t, l.BundlePath("library", "mysql")), "config.yaml"), "name: x\n", 0o644)
	return l
}

func mkdir(t *testing.T, dir string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestGuidedProfilesAlwaysGetTheBaseFeaturesWithoutRecordingThem(t *testing.T) {
	l := baseLayout(t)
	dir, err := CreateGuided(l, "g", []string{"clip", "mysql"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "agent-files", "clip.txt")); err != nil {
		t.Errorf("the base feature's files are not in the profile: %v", err)
	}
	if got, want := ComposedFrom(dir), []string{"mysql"}; !reflect.DeepEqual(got, want) {
		t.Errorf("recorded %v, want %v", got, want)
	}

	none, err := CreateGuided(l, "n", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(none, "agent-files", "clip.txt")); err != nil {
		t.Errorf("a profile with no picks lacks the base feature: %v", err)
	}
	if !IsGuided(none) {
		t.Error("a profile composed from the base features alone should still say it was composed")
	}
}

// Taking a feature out of the record is vibe's doing, not a hand edit: a
// profile nobody had edited still reads as unedited after.
func TestDropFromRecordKeepsAnUneditedProfileUnedited(t *testing.T) {
	l := testLayout(t)
	dir := l.HomePath("profiles", "old")
	mkdir(t, dir)
	write(t, filepath.Join(dir, "config.yaml"), "# vibe: features: gone, mysql\nname: old\n", 0o644)
	if err := RecordHash(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := DropFromRecord(dir, []string{"gone"}); err != nil {
		t.Fatal(err)
	}
	if have, _ := ContentHash(dir); RecordedHash(dir) != have {
		t.Error("the profile reads as edited by hand")
	}

	// One that had been edited stays so.
	write(t, filepath.Join(dir, "config.yaml"), "# vibe: features: gone, mysql\nname: mine\n", 0o644)
	if _, err := DropFromRecord(dir, []string{"gone"}); err != nil {
		t.Fatal(err)
	}
	if have, _ := ContentHash(dir); RecordedHash(dir) == have {
		t.Error("a hand-edited profile now reads as unedited")
	}
}

func TestDropFromRecordRewritesTheProfile(t *testing.T) {
	l := testLayout(t)
	dir := l.HomePath("profiles", "old")
	mkdir(t, dir)
	write(t, filepath.Join(dir, "config.yaml"), "# vibe: features: clip, mysql\nname: old\n", 0o644)

	got, err := DropFromRecord(dir, []string{"clip"})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"mysql"}; !reflect.DeepEqual(got, want) || !reflect.DeepEqual(ComposedFrom(dir), want) {
		t.Errorf("returned %v, recorded %v, want %v", got, ComposedFrom(dir), want)
	}

	// Only the base feature recorded: the profile is still a guided one.
	write(t, filepath.Join(dir, "config.yaml"), "# vibe: features: clip\nname: old\n", 0o644)
	if _, err := DropFromRecord(dir, []string{"clip"}); err != nil {
		t.Fatal(err)
	}
	if !IsGuided(dir) || len(ComposedFrom(dir)) != 0 {
		t.Errorf("want a guided profile recording nothing, got guided=%v recorded=%v", IsGuided(dir), ComposedFrom(dir))
	}
}

// A copy keeps the choices made for the profile it was copied from (on purpose:
// it is the same kind of profile, and vibe says so when it makes one).
func TestACopyCarriesTheChoicesMadeForItsSource(t *testing.T) {
	l := testLayout(t)
	source, err := CopyFrom(l, "widget", "widget-two")
	if err != nil {
		t.Fatal(err)
	}
	if err := userprefs.Update(source, func(p *userprefs.Prefs) { p.Model, p.GitAuthor = "opus", "leave" }); err != nil {
		t.Fatal(err)
	}
	dir, err := CopyFrom(l, "widget-two", "widget-three")
	if err != nil {
		t.Fatal(err)
	}
	got, err := userprefs.Load(dir)
	if err != nil || got.Model != "opus" || got.GitAuthor != "leave" {
		t.Errorf("the copy holds %+v, %v", got, err)
	}
}

// oldBlankConfig is the config.yaml vibe wrote for a blank profile before blank profiles had the
// base features: nothing composed, and marked as blank.
const oldBlankConfig = `# vibe profile "plain".
# This profile was created blank: nothing was composed into it, so a
# re-compose (vibe -C) leaves it as it is and only makes the sandbox afresh.
# vibe: blank: yes
name: plain
displayName: plain sandbox
description: Sandbox for the plain workspace
`

// oldBlank is a blank profile as an earlier vibe made it, in the overlay of l.
func oldBlank(t *testing.T, l layout.Layout) string {
	t.Helper()
	dir := l.NewProfileDir("plain")
	mkdir(t, filepath.Join(dir, "install-scripts"))
	mkdir(t, filepath.Join(dir, "agent-files"))
	write(t, filepath.Join(dir, "config.yaml"), oldBlankConfig, 0o644)
	write(t, filepath.Join(dir, "agent-instructions.md"), "", 0o644)
	return dir
}

func TestABlankProfileIsAGuidedOneWithNoOptionalFeaturesPicked(t *testing.T) {
	// Or it has no clipboard page, and a vibe app with a missing part.
	l := baseLayout(t)
	dir, err := CreateBlank(l, "plain")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "agent-files", "clip.txt")); err != nil {
		t.Errorf("the base feature's files are not in a blank profile: %v", err)
	}
	if !IsGuided(dir) {
		t.Error("a blank profile doesn't say it was composed, so a re-compose would treat it as hand-made")
	}
	if got := ComposedFrom(dir); got != nil {
		t.Errorf("optional features recorded for a blank profile: %v", got)
	}
}

func TestABlankProfileHasTheSameFilesAsAGuidedOneWithNothingPicked(t *testing.T) {
	l := baseLayout(t)
	blank, err := CreateBlank(l, "blank")
	if err != nil {
		t.Fatal(err)
	}
	guided, err := CreateGuided(l, "guided", nil)
	if err != nil {
		t.Fatal(err)
	}
	files := func(dir string) []string {
		var out []string
		filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
			if err == nil && !d.IsDir() {
				rel, _ := filepath.Rel(dir, path)
				out = append(out, rel)
			}
			return nil
		})
		return out
	}
	if a, b := files(blank), files(guided); !reflect.DeepEqual(a, b) {
		t.Errorf("a blank profile has %v, a guided one with nothing picked %v", a, b)
	}
}

func TestANewProfilesConfigDoesNotSayNothingWasComposedIntoIt(t *testing.T) {
	for name, make := range map[string]func(layout.Layout) (string, error){
		"blank":  func(l layout.Layout) (string, error) { return CreateBlank(l, "p") },
		"guided": func(l layout.Layout) (string, error) { return CreateGuided(l, "p", nil) },
	} {
		dir, err := make(baseLayout(t))
		if err != nil {
			t.Fatal(err)
		}
		data, _ := os.ReadFile(filepath.Join(dir, "config.yaml"))
		if strings.Contains(string(data), "created blank") || strings.Contains(string(data), "vibe: blank") {
			t.Errorf("%s: the config says it was left uncomposed, which it wasn't:\n%s", name, data)
		}
	}
}

func TestAnEarlierVibesBlankProfileIsStillRecognisedAsOne(t *testing.T) {
	dir := oldBlank(t, testLayout(t))
	if !IsBlank(dir) || IsGuided(dir) {
		t.Errorf("blank = %v, guided = %v", IsBlank(dir), IsGuided(dir))
	}
}

func TestABlankProfileNobodyHasGrownIsSaidToBeUntouched(t *testing.T) {
	dir := oldBlank(t, testLayout(t))
	if !IsUntouchedBlank(dir) {
		t.Error("a blank profile as it was made isn't untouched")
	}
	// The user's own instructions and choices are carried across a rebuild, so are not growth.
	write(t, filepath.Join(dir, "agent-instructions.md"), "be kind\n", 0o644)
	write(t, filepath.Join(dir, "user-preferences.yaml"), "model: opus\n", 0o644)
	if !IsUntouchedBlank(dir) {
		t.Error("the user's instructions and preferences count as growing it")
	}
}

func TestABlankProfileTheUserHasGrownIsNotUntouched(t *testing.T) {
	grown := map[string]func(dir string){
		"an install script": func(dir string) { write(t, filepath.Join(dir, "install-scripts", "01-mine"), "echo\n", 0o755) },
		"an agent file": func(dir string) {
			write(t, filepath.Join(mkdir(t, filepath.Join(dir, "agent-files", ".local")), "x"), "x\n", 0o644)
		},
		"a settings.yaml":      func(dir string) { write(t, filepath.Join(dir, "settings.yaml"), "memory: 8g\n", 0o644) },
		"a kit's instructions": func(dir string) { write(t, filepath.Join(dir, "kit-instructions.md"), "x\n", 0o644) },
		"something added to the config": func(dir string) {
			f, _ := os.OpenFile(filepath.Join(dir, "config.yaml"), os.O_APPEND|os.O_WRONLY, 0o644)
			f.WriteString("ports:\n  - container: 8080\n")
			f.Close()
		},
	}
	for what, grow := range grown {
		dir := oldBlank(t, testLayout(t))
		grow(dir)
		if IsUntouchedBlank(dir) {
			t.Errorf("a blank profile with %s is called untouched", what)
		}
	}
}

func TestOnlyABlankProfileCanBeUntouchedBlank(t *testing.T) {
	l := baseLayout(t)
	if dir, _ := CreateGuided(l, "g", nil); IsUntouchedBlank(dir) {
		t.Error("a guided profile is called an untouched blank one")
	}
	if IsUntouchedBlank(l.BundlePath("profiles", "widget")) {
		t.Error("a hand-written profile is called an untouched blank one")
	}
	if IsUntouchedBlank(filepath.Join(t.TempDir(), "nope")) {
		t.Error("a profile that isn't there is called an untouched blank one")
	}
}
