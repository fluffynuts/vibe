package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"vibe/internal/layout"
)

// shadowLayout is a layout over the real library whose user keeps their own copy of a base feature,
// changed in one file (and, if more, in others too).
func shadowLayout(t *testing.T) (layout.Layout, string) {
	t.Helper()
	root, _ := goldenBundle(t)
	lay := layout.New(t.TempDir(), root)
	mine := lay.HomePath("library", "base", "clipboard-bridge")
	if err := copyTree(lay.BundlePath("library", "base", "clipboard-bridge"), mine); err != nil {
		t.Fatal(err)
	}
	return lay, mine
}

func copyTree(from, to string) error {
	return filepath.WalkDir(from, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(from, path)
		if d.IsDir() {
			return os.MkdirAll(filepath.Join(to, rel), 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(to, rel), data, 0o644)
	})
}

func TestAnOldCopyOfAFeatureThatBeatsTheShippedOneIsSaidSoAndWhatDiffers(t *testing.T) {
	lay, mine := shadowLayout(t)
	hook := filepath.Join(mine, "agent-files", ".local", "share", "vibe", "clipboard-hook.js")
	writeFile(t, hook, "// an old one\n")
	said := strings.Join(notesOf(func() { noteShadowedFeatures(lay, nil) }), "\n")
	for _, want := range []string{"clipboard-bridge", "your own copy", "clipboard-hook.js", "diff -r", lay.BundlePath("library", "base", "clipboard-bridge")} {
		if !strings.Contains(said, want) {
			t.Errorf("it didn't say %q: %q", want, said)
		}
	}
}

func TestACopyThatIsTheSameAsTheShippedOneIsNotSpokenOf(t *testing.T) {
	lay, _ := shadowLayout(t)
	if said := strings.Join(notesOf(func() { noteShadowedFeatures(lay, nil) }), "\n"); said != "" {
		t.Errorf("it said %q", said)
	}
}

func TestWithNoCopiesOfTheUsersOwnNothingIsSaid(t *testing.T) {
	root, _ := goldenBundle(t)
	lay := layout.New(t.TempDir(), root)
	if said := strings.Join(notesOf(func() { noteShadowedFeatures(lay, []string{"mysql"}) }), "\n"); said != "" {
		t.Errorf("it said %q", said)
	}
}

func TestAChosenFeatureTheUserOverrodeIsSaidToo(t *testing.T) {
	root, _ := goldenBundle(t)
	lay := layout.New(t.TempDir(), root)
	if err := copyTree(lay.BundlePath("library", "mysql"), lay.HomePath("library", "mysql")); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(lay.HomePath("library", "mysql"), "install-scripts", "99-mine"), "echo mine\n")
	said := strings.Join(notesOf(func() { noteShadowedFeatures(lay, []string{"mysql"}) }), "\n")
	if !strings.Contains(said, "'mysql'") || !strings.Contains(said, "99-mine") {
		t.Errorf("it said %q", said)
	}
	// Not one that isn't being composed in.
	if said := strings.Join(notesOf(func() { noteShadowedFeatures(lay, []string{"redis"}) }), "\n"); said != "" {
		t.Errorf("it spoke of a feature not being composed: %q", said)
	}
}

func TestOnlyTheFirstFewDifferencesAreNamedAndTheRestCounted(t *testing.T) {
	lay, mine := shadowLayout(t)
	for _, name := range []string{"a", "b", "c", "d", "e"} {
		writeFile(t, filepath.Join(mine, "agent-files", "extra-"+name), "x\n")
	}
	said := strings.Join(notesOf(func() { noteShadowedFeatures(lay, nil) }), "\n")
	if !strings.Contains(said, "and 2 more") {
		t.Errorf("it said %q", said)
	}
}

// Wherever a profile is composed it is said: creating one, blank or guided, and composing it again.
func TestReComposingSaysWhenACopyOfAFeatureBeatsTheShippedOne(t *testing.T) {
	lay, mine := shadowLayout(t)
	writeFile(t, filepath.Join(mine, "agent-files", ".local", "bin", "start-clipboard"), "# an old one\n")
	var err error
	said := strings.Join(notesOf(func() { err = replaceProfile(lay, "p", "p", nil) }), "\n")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(said, "your own copy") || !strings.Contains(said, "start-clipboard") {
		t.Errorf("it said %q", said)
	}
}

func TestCreatingABlankProfileSaysWhenACopyOfAFeatureBeatsTheShippedOne(t *testing.T) {
	lay, mine := shadowLayout(t)
	writeFile(t, filepath.Join(mine, "agent-files", ".local", "bin", "start-clipboard"), "# an old one\n")
	var err error
	said := strings.Join(notesOf(func() { err = createProfileIfMissing(lay, "fresh", true, "claude") }), "\n")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(said, "your own copy") {
		t.Errorf("it said %q", said)
	}
}

// A warning that goes by between questions is missed, so it is drawn as vibe's other warnings are.
const warningStyle = "\x1b[1;93m"

func TestTheWarningOfACopyBeatingTheShippedOneIsBoldBrightYellowOnATerminal(t *testing.T) {
	lay, mine := shadowLayout(t)
	writeFile(t, filepath.Join(mine, "agent-files", ".local", "bin", "start-clipboard"), "# an old one\n")
	styled(t, true)
	got := stderrOf(t, func() { noteShadowedFeatures(lay, nil) })
	lines := strings.Split(strings.TrimRight(got, "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("%d lines: %q", len(lines), got)
	}
	for i, line := range lines {
		if !strings.HasPrefix(line, "vibe: "+warningStyle) || !strings.HasSuffix(line, "\x1b[0m") {
			t.Errorf("line %d isn't drawn as a warning: %q", i+1, line)
		}
	}
	if !strings.Contains(lines[0], "WARNING: ") {
		t.Errorf("the first line doesn't say it is a warning: %q", lines[0])
	}
}

func TestTheWarningIsPlainWhereThereIsNoTerminalOrNoColourIsWanted(t *testing.T) {
	lay, mine := shadowLayout(t)
	writeFile(t, filepath.Join(mine, "agent-files", ".local", "bin", "start-clipboard"), "# an old one\n")
	styled(t, false)
	got := stderrOf(t, func() { noteShadowedFeatures(lay, nil) })
	if strings.Contains(got, "\x1b") || !strings.Contains(got, "vibe: WARNING: ") {
		t.Errorf("got %q", got)
	}
}

func TestTheWarningIsPassedOnToTheLogWithoutItsStyling(t *testing.T) {
	lay, mine := shadowLayout(t)
	writeFile(t, filepath.Join(mine, "agent-files", ".local", "bin", "start-clipboard"), "# an old one\n")
	styled(t, true)
	var lines []string
	stderrOf(t, func() { lines = notesOf(func() { noteShadowedFeatures(lay, nil) }) })
	if len(lines) != 3 || !strings.HasPrefix(lines[0], "WARNING: ") || strings.Contains(strings.Join(lines, ""), "\x1b") {
		t.Errorf("lines: %q", lines)
	}
}
