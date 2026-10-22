package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"vibe/internal/cliargs"
	"vibe/internal/layout"
	"vibe/internal/userprefs"
)

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

// TestReComposeRejectsAFeatureThatIsGoneWhenNotAsking keeps a renamed or
// removed feature from silently dropping out of a rebuilt profile when there
// is no one to ask (--force).
func TestReComposeRejectsAFeatureThatIsGoneWhenNotAsking(t *testing.T) {
	lay := layout.New(t.TempDir(), t.TempDir())
	writeFile(t, lay.BundlePath("library", "dashboard", "install-scripts", "01-install"), "#!/bin/sh\necho hi\n")
	writeFile(t, lay.HomePath("profiles", "foo-browser", "config.yaml"),
		"# vibe: features: dashboard, dashbord\nname: foo-browser\n")

	err := doReCompose(lay, cliargs.Args{Force: true, Profile: "foo-browser"}, "foo-browser", t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "did you mean 'dashboard'?") {
		t.Errorf("error for a vanished feature = %v", err)
	}
}

func TestSplitMissingFeaturesKeepsWhatIsStillThere(t *testing.T) {
	present, missing := splitMissingFeatures(
		[]string{"clipboard", "dashboard", "mysql"}, []string{"dashboard", "mysql", "redis"})
	if got := strings.Join(present, ","); got != "dashboard,mysql" {
		t.Errorf("present = %s", got)
	}
	if got := strings.Join(missing, ","); got != "clipboard" {
		t.Errorf("missing = %s", got)
	}
}

// instructionsFixture is a library with one feature that carries
// instructions, and a guided profile composed from it the way profiles were
// before the kits' instructions had a file of their own: their text in the
// profile's agent-instructions.md, and no kit-instructions.md.
func instructionsFixture(t *testing.T) (lay layout.Layout, profileDir string) {
	t.Helper()
	lay = layout.New(t.TempDir(), t.TempDir())
	writeFile(t, lay.BundlePath("library", "dashboard", "install-scripts", "01-install"), "#!/bin/sh\necho hi\n")
	writeFile(t, lay.BundlePath("library", "dashboard", "agent-instructions.md"), "## Dashboard\n\nreview with it.\n")
	profileDir = lay.HomePath("profiles", "foo-browser")
	writeFile(t, filepath.Join(profileDir, "config.yaml"), "# vibe: features: dashboard\nname: foo-browser\n")
	writeFile(t, filepath.Join(profileDir, "agent-instructions.md"), "## Dashboard\n\nreview with it.\n")
	return lay, profileDir
}

func readFile(t *testing.T, path string) (string, bool) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	return string(data), true
}

func TestReComposingAnOldProfileGivesTheUserTheirOwnInstructionsFile(t *testing.T) {
	lay, dir := instructionsFixture(t)
	args := cliargs.Args{Force: true, Profile: "foo-browser"}
	if err := recomposeProfile(lay, args, "foo-browser", t.TempDir(), false, nil); err != nil {
		t.Fatal(err)
	}
	if got, _ := readFile(t, filepath.Join(dir, "kit-instructions.md")); !strings.Contains(got, "## Dashboard") {
		t.Errorf("kit-instructions.md = %q", got)
	}
	if got, ok := readFile(t, filepath.Join(dir, "agent-instructions.md")); !ok || got != "" {
		t.Errorf("agent-instructions.md = %q, %v; want the user's, blank", got, ok)
	}
	if got, ok := readFile(t, lay.HomePath("agent-instructions.md")); !ok || got != "" {
		t.Errorf("~/.vibe/agent-instructions.md = %q, %v; want a blank one", got, ok)
	}
}

func TestReComposingKeepsTheUsersInstructions(t *testing.T) {
	lay, dir := instructionsFixture(t)
	args := cliargs.Args{Force: true, Profile: "foo-browser"}
	if err := recomposeProfile(lay, args, "foo-browser", t.TempDir(), false, nil); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "agent-instructions.md"), "use British spelling\n")
	for i := 0; i < 2; i++ {
		if err := recomposeProfile(lay, args, "foo-browser", t.TempDir(), false, nil); err != nil {
			t.Fatal(err)
		}
		if got, _ := readFile(t, filepath.Join(dir, "agent-instructions.md")); got != "use British spelling\n" {
			t.Fatalf("after re-compose %d: agent-instructions.md = %q", i+1, got)
		}
		if got, _ := readFile(t, filepath.Join(dir, "kit-instructions.md")); strings.Contains(got, "British") || !strings.Contains(got, "## Dashboard") {
			t.Fatalf("after re-compose %d: kit-instructions.md = %q", i+1, got)
		}
	}
}

func TestRestoringABackupFromBeforeTheMoveStillSortsTheFilesOut(t *testing.T) {
	lay, dir := instructionsFixture(t)
	args := cliargs.Args{Force: true, Profile: "foo-browser"}
	// The first re-compose backs up the profile as it was: old-style.
	if err := recomposeProfile(lay, args, "foo-browser", t.TempDir(), false, nil); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "agent-instructions.md"), "mine\n")
	if err := doRestore(lay, args, "foo-browser", t.TempDir()); err == nil {
		t.Log("restore went on to rebuild the sandbox")
	}
	// The oldest backup is the old-style profile: restored, then
	// re-composed, its kit text must not land in the user's file.
	if got, _ := readFile(t, filepath.Join(dir, "kit-instructions.md")); !strings.Contains(got, "## Dashboard") {
		t.Errorf("kit-instructions.md = %q", got)
	}
	if got, _ := readFile(t, filepath.Join(dir, "agent-instructions.md")); strings.Contains(got, "## Dashboard") {
		t.Errorf("the kits' text ended up in the user's file: %q", got)
	}
}

func TestABundledProfileIsNotCopiedIntoTheOverlay(t *testing.T) {
	lay := layout.New(t.TempDir(), t.TempDir())
	writeFile(t, lay.BundlePath("profiles", "shipped", "config.yaml"), "name: shipped\n")
	if err := prepareInstructions(lay, "shipped"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(lay.HomePath("profiles", "shipped")); !os.IsNotExist(err) {
		t.Errorf("a folder was made in ~/.vibe for a bundled profile, which would hide it: %v", err)
	}
	if got, ok := readFile(t, lay.HomePath("agent-instructions.md")); !ok || got != "" {
		t.Errorf("~/.vibe/agent-instructions.md = %q, %v", got, ok)
	}
}

// A profile built before a base feature existed is told which it lacks:
// not always the clipboard.
func TestAProfileMissingABaseFeatureIsToldWhich(t *testing.T) {
	lay := updateLayout(t)
	if err := os.RemoveAll(lay.HomePath("profiles", "p", "agent-files", ".local", "bin", "vibe-review")); err != nil {
		t.Fatal(err)
	}
	notes := strings.Join(notesOf(func() { noteMissingBase(lay, "p") }), "\n")
	if !strings.Contains(notes, "review") || strings.Contains(notes, "clipboard") {
		t.Errorf("notes: %q", notes)
	}
}

func TestReComposingKeepsTheModelAndEffortChosenOnThePage(t *testing.T) {
	lay, dir := instructionsFixture(t)
	args := cliargs.Args{Force: true, Profile: "foo-browser"}
	writeFile(t, filepath.Join(dir, "settings.yaml"), "memory: 8g\nmodel: sonnet\neffort: high\n")
	if err := recomposeProfile(lay, args, "foo-browser", t.TempDir(), false, nil); err != nil {
		t.Fatal(err)
	}
	got, err := profileChoice{lay: lay, profile: "foo-browser"}.get()
	if err != nil || got != (turnChoice{Model: "sonnet", Effort: "high"}) {
		t.Errorf("after re-composing, the choice is %+v, %v; want sonnet, high", got, err)
	}
	// And it is where it is kept now, not in the settings composing wrote.
	if text, _ := readFile(t, filepath.Join(dir, "settings.yaml")); strings.Contains(text, "model") || strings.Contains(text, "effort") {
		t.Errorf("settings.yaml = %q", text)
	}
	if prefs, _ := userprefs.Load(dir); prefs.Model != "sonnet" || prefs.Effort != "high" {
		t.Errorf("preferences = %+v", prefs)
	}
}

func TestReComposingKeepsTheThemeAndTheGitAuthorAnswerWhereverEarlierVibesPutThem(t *testing.T) {
	lay, dir := instructionsFixture(t)
	args := cliargs.Args{Force: true, Profile: "foo-browser"}
	writeFile(t, filepath.Join(dir, "claude-theme"), "amber\n")
	writeFile(t, filepath.Join(dir, "git-author"), "import\n")
	if err := recomposeProfile(lay, args, "foo-browser", t.TempDir(), false, nil); err != nil {
		t.Fatal(err)
	}
	prefs, err := userprefs.Load(dir)
	if err != nil || prefs.Theme != "amber" || prefs.GitAuthor != "import" {
		t.Errorf("after re-composing: %+v, %v", prefs, err)
	}
	for _, old := range userprefs.LegacyFiles {
		if _, err := os.Stat(filepath.Join(dir, old)); err == nil {
			t.Errorf("%s was left beside the preferences", old)
		}
	}
}

func TestReComposingKeepsWhatWasKeptInThePreferences(t *testing.T) {
	lay, dir := instructionsFixture(t)
	args := cliargs.Args{Force: true, Profile: "foo-browser"}
	writeFile(t, filepath.Join(dir, "user-preferences.yaml"), "# mine\ntheme: green\nfuture: thing\n")
	if err := recomposeProfile(lay, args, "foo-browser", t.TempDir(), false, nil); err != nil {
		t.Fatal(err)
	}
	if text, _ := readFile(t, userprefs.Path(dir)); text != "# mine\ntheme: green\nfuture: thing\n" {
		t.Errorf("the preferences are %q: carried across as they were, a key this vibe doesn't know included", text)
	}
}

func TestReComposingAProfileWithNoChoiceMakesNone(t *testing.T) {
	lay, dir := instructionsFixture(t)
	args := cliargs.Args{Force: true, Profile: "foo-browser"}
	if err := recomposeProfile(lay, args, "foo-browser", t.TempDir(), false, nil); err != nil {
		t.Fatal(err)
	}
	if got, _ := readFile(t, filepath.Join(dir, "settings.yaml")); strings.Contains(got, "model") || strings.Contains(got, "effort") {
		t.Errorf("settings.yaml = %q", got)
	}
}

// profileWith makes a profile "foo-browser" holding files, and gives its
// layout and folder.
func profileWith(t *testing.T, files map[string]string) (layout.Layout, string) {
	t.Helper()
	lay := layout.New(t.TempDir(), t.TempDir())
	dir := lay.HomePath("profiles", "foo-browser")
	writeFile(t, filepath.Join(dir, "config.yaml"), "name: foo-browser\n")
	writeFile(t, filepath.Join(dir, "install-scripts", "01-mine"), "echo mine\n")
	for name, text := range files {
		writeFile(t, filepath.Join(dir, name), text)
	}
	return lay, dir
}

// answeringKeep stands in for the user: it answers each question with answer (nil:
// none is expected), and gives what was asked.
func answeringKeep(t *testing.T, answer func(question string) bool) (asked *[]string, restore func()) {
	t.Helper()
	asked = new([]string)
	was := askKeepPreferences
	askKeepPreferences = func(question string) bool {
		*asked = append(*asked, question)
		if answer == nil {
			t.Errorf("asked %q when nothing was to be asked", question)
			return false
		}
		return answer(question)
	}
	return asked, func() { askKeepPreferences = was }
}

const kept = "# mine\ntheme: green\nmodel: opus\nfuture: thing\n"

func TestReCreateWithForceKeepsTheChoicesByteForByteAndLeavesTheRestToBeMadeAgain(t *testing.T) {
	lay, dir := profileWith(t, map[string]string{"user-preferences.yaml": kept})
	asked, restore := answeringKeep(t, nil)
	defer restore()
	doReCreate(lay, cliargs.Args{Force: true, Profile: "foo-browser"}, "foo-browser", t.TempDir())
	if len(*asked) != 0 {
		t.Errorf("asked %q", *asked)
	}
	if got, _ := readFile(t, filepath.Join(dir, "user-preferences.yaml")); got != kept {
		t.Errorf("the new profile's preferences are %q, want them as they were", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "install-scripts", "01-mine")); err == nil {
		t.Error("the rest of the old profile was kept too")
	}
}

func TestReCreateSaysWhatItKeptAndHowToDropIt(t *testing.T) {
	lay, _ := profileWith(t, map[string]string{"user-preferences.yaml": kept})
	said := strings.Join(notesOf(func() { preferencesToKeep(lay, "foo-browser", true) }), "\n")
	for _, want := range []string{"keeping your choices", "theme: green", "model: opus", "user-preferences.yaml", "to drop them"} {
		if !strings.Contains(said, want) {
			t.Errorf("it said %q, lacking %q", said, want)
		}
	}
}

func TestReCreateAsksWhetherToKeepTheChoicesListingThemAndKeepsThemOnYes(t *testing.T) {
	lay, _ := profileWith(t, map[string]string{"user-preferences.yaml": kept})
	asked, restore := answeringKeep(t, func(string) bool { return true })
	defer restore()
	data, err := preferencesToKeep(lay, "foo-browser", false)
	if err != nil || string(data) != kept {
		t.Errorf("kept %q, %v", data, err)
	}
	if len(*asked) != 1 {
		t.Fatalf("asked %q", *asked)
	}
	for _, want := range []string{"profile 'foo-browser'", "theme: green", "model: opus"} {
		if !strings.Contains((*asked)[0], want) {
			t.Errorf("the question lacks %q: %s", want, (*asked)[0])
		}
	}
}

func TestReCreateDropsTheChoicesOnNo(t *testing.T) {
	lay, _ := profileWith(t, map[string]string{"user-preferences.yaml": kept})
	_, restore := answeringKeep(t, func(string) bool { return false })
	defer restore()
	if data, err := preferencesToKeep(lay, "foo-browser", false); err != nil || data != nil {
		t.Errorf("kept %q, %v after a no", data, err)
	}
}

func TestReCreateAsksNothingWhenThereAreNoChoicesToKeep(t *testing.T) {
	for name, files := range map[string]map[string]string{
		"no file": nil, "an empty file": {"user-preferences.yaml": "  \n"}, "only comments": {"user-preferences.yaml": "# nothing\n"},
	} {
		lay, _ := profileWith(t, files)
		_, restore := answeringKeep(t, nil)
		data, err := preferencesToKeep(lay, "foo-browser", false)
		restore()
		if err != nil || data != nil {
			t.Errorf("%s: kept %q, %v", name, data, err)
		}
	}
}

func TestReCreateKeepsAFileItCannotReadAsItIsAndSaysItCouldntRead(t *testing.T) {
	broken := "model: [unclosed\n"
	lay, _ := profileWith(t, map[string]string{"user-preferences.yaml": broken})
	asked, restore := answeringKeep(t, func(string) bool { return true })
	defer restore()
	data, err := preferencesToKeep(lay, "foo-browser", false)
	if err != nil || string(data) != broken {
		t.Errorf("kept %q, %v: what the user may have been editing must be carried whole", data, err)
	}
	if len(*asked) != 1 || !strings.Contains((*asked)[0], "can't read") {
		t.Errorf("asked %q", *asked)
	}
}

func TestReCreateKeepsWhatEarlierVibesKeptInFilesOfTheirOwn(t *testing.T) {
	lay, dir := profileWith(t, map[string]string{"claude-theme": "amber\n", "git-author": "import\n"})
	asked, restore := answeringKeep(t, nil)
	defer restore()
	doReCreate(lay, cliargs.Args{Force: true, Profile: "foo-browser"}, "foo-browser", t.TempDir())
	if len(*asked) != 0 {
		t.Errorf("asked %q", *asked)
	}
	prefs, err := userprefs.Load(dir)
	if err != nil || prefs.Theme != "amber" || prefs.GitAuthor != "import" {
		t.Errorf("prefs %+v, %v", prefs, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "claude-theme")); err == nil {
		t.Error("the old theme file came back")
	}
}

func TestTheKeptChoicesAreBackOnceTheNewProfileIsMadeAndBeforeTheKitIsRead(t *testing.T) {
	lay := layout.New(t.TempDir(), t.TempDir())
	var madeFirst bool
	var theme string
	hook := func(l layout.Layout, profile string) error {
		madeFirst = l.ProfileExists(profile)
		if err := restorePreferences([]byte("theme: green\n"))(l, profile); err != nil {
			return err
		}
		prefs, _ := userprefs.Load(l.HomePath("profiles", profile))
		theme = prefs.Theme
		return nil
	}
	// With no sbx here the rebuild fails further on; the hook has run by then.
	rebuildSandbox(context.Background(), lay, cliargs.Args{Force: true, Profile: "foo-browser"}, "box", t.TempDir(), true, false, hook)
	if !madeFirst || theme != "green" {
		t.Errorf("profile made first: %v; preferences' theme: %q", madeFirst, theme)
	}
}
