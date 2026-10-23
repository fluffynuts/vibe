package profilegen

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"vibe/internal/layout"
)

// Everything else here rests on this: composing the same features twice,
// from the real library, writes the same bytes.
func TestComposingIsDeterministic(t *testing.T) {
	repo, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	l := layout.New(t.TempDir(), repo)
	features := l.Features()
	if len(features) == 0 {
		t.Fatal("no features in the real library")
	}
	var sums []string
	for i := 0; i < 2; i++ {
		dir := filepath.Join(t.TempDir(), "p")
		if err := composeInto(l, dir, "p", features); err != nil {
			t.Fatal(err)
		}
		sum, err := ContentHash(dir)
		if err != nil {
			t.Fatal(err)
		}
		sums = append(sums, sum)
	}
	if sums[0] != sums[1] {
		t.Errorf("composing %v twice gave different profiles", features)
	}
}

// guidedLayout is a library with one feature, mysql, and a guided profile
// composed from it.
func guidedLayout(t *testing.T) (layout.Layout, string) {
	t.Helper()
	l := testLayout(t)
	write(t, mkdirAndPath(t, l.BundlePath("library", "mysql", "install-scripts"), "01-install-mysql"),
		"#!/bin/sh\necho install-mysql\n", 0o644)
	dir, err := CreateGuided(l, "phoenix", []string{"mysql"})
	if err != nil {
		t.Fatal(err)
	}
	return l, dir
}

func check(t *testing.T, l layout.Layout, dir string) Freshness {
	t.Helper()
	got, err := Check(l, dir, "phoenix", ComposedFrom(dir))
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestAProfileJustComposedIsUpToDate(t *testing.T) {
	l, dir := guidedLayout(t)
	if got := check(t, l, dir); got != UpToDate {
		t.Errorf("Check = %v, want UpToDate", got)
	}
}

// The user's own files carry across a re-compose, so changing them doesn't
// make the profile differ from a fresh composition.
func TestTheUsersOwnFilesDontCount(t *testing.T) {
	l, dir := guidedLayout(t)
	write(t, filepath.Join(dir, "agent-instructions.md"), "be brief\n", 0o644)
	write(t, filepath.Join(dir, "user-preferences.yaml"), "theme: dark\nmodel: opus\n", 0o644)
	if got := check(t, l, dir); got != UpToDate {
		t.Errorf("Check = %v, want UpToDate", got)
	}
}

// What earlier vibes kept in files of their own isn't composed either, until
// it has been moved into user-preferences.yaml.
func TestWhatEarlierVibesKeptDoesntCountEither(t *testing.T) {
	l, dir := guidedLayout(t)
	write(t, filepath.Join(dir, "claude-theme"), "dark\n", 0o644)
	write(t, filepath.Join(dir, "git-author"), "import\n", 0o644)
	if got := check(t, l, dir); got != UpToDate {
		t.Errorf("Check = %v, want UpToDate", got)
	}
}

func TestALibraryChangeMakesAnUneditedProfileOutOfDate(t *testing.T) {
	l, dir := guidedLayout(t)
	write(t, l.BundlePath("library", "mysql", "install-scripts", "01-install-mysql"), "#!/bin/sh\necho v2\n", 0o644)
	if got := check(t, l, dir); got != OutOfDate {
		t.Errorf("Check = %v, want OutOfDate", got)
	}
}

func TestAHandEditIsNeverTakenForALibraryChange(t *testing.T) {
	l, dir := guidedLayout(t)
	write(t, l.BundlePath("library", "mysql", "install-scripts", "01-install-mysql"), "#!/bin/sh\necho v2\n", 0o644)
	write(t, filepath.Join(dir, "settings.yaml"), "memory: 24g\n", 0o644)
	if got := check(t, l, dir); got != Edited {
		t.Errorf("Check = %v, want Edited", got)
	}
}

// A profile from before the hash was recorded is older than this vibe:
// different from a fresh composition, it's out of date.
func TestAProfileWithNoRecordedHashIsOutOfDateWhenItDiffers(t *testing.T) {
	l, dir := guidedLayout(t)
	if err := os.Remove(filepath.Join(dir, HashFile)); err != nil {
		t.Fatal(err)
	}
	if got := check(t, l, dir); got != UpToDate {
		t.Errorf("unchanged: Check = %v, want UpToDate", got)
	}
	write(t, filepath.Join(dir, "settings.yaml"), "memory: 24g\n", 0o644)
	if got := check(t, l, dir); got != OutOfDate {
		t.Errorf("changed: Check = %v, want OutOfDate", got)
	}
}

func TestTheHashWithAFileChangedIsTheHashItWouldHaveOnceChanged(t *testing.T) {
	_, dir := guidedLayout(t)
	changed := []byte("memory: 24g\n")
	was, _ := ContentHash(dir)
	with, err := ContentHashWith(dir, map[string][]byte{"settings.yaml": changed})
	if err != nil || with == was {
		t.Fatalf("with = %q (%v), was %q", with, err, was)
	}
	if now, _ := ContentHash(dir); now != was {
		t.Error("looking at it changed the folder")
	}
	write(t, filepath.Join(dir, "settings.yaml"), string(changed), 0o644)
	if now, _ := ContentHash(dir); now != with {
		t.Errorf("once written, the hash is %q, not the %q it was said to be", now, with)
	}
}

func TestTheHashWithAFileGoneIsTheHashItWouldHaveOnceItWas(t *testing.T) {
	_, dir := guidedLayout(t)
	was, _ := ContentHash(dir)
	write(t, filepath.Join(dir, "settings.yaml"), "model: opus\n", 0o644)
	with, err := ContentHashWith(dir, map[string][]byte{"settings.yaml": nil})
	if err != nil || with != was {
		t.Errorf("with the file gone: %q (%v), want the %q before it was written", with, err, was)
	}
}

func TestAFileAsComposingWouldWriteItNowCanBeAskedFor(t *testing.T) {
	l, dir := guidedLayout(t)
	got, ok, err := ComposedFile(l, "phoenix", ComposedFrom(dir), "no-such-file")
	if err != nil || ok || got != nil {
		t.Errorf("a file composing doesn't write: %q, %v, %v", got, ok, err)
	}
	// The same bytes the profile has now, as it was just composed.
	have, _ := os.ReadFile(filepath.Join(dir, "config.yaml"))
	got, ok, err = ComposedFile(l, "phoenix", ComposedFrom(dir), "config.yaml")
	if err != nil || !ok || string(got) != string(have) {
		t.Errorf("config.yaml: %q, %v, %v; want %q", got, ok, err, have)
	}
}

func TestAGuidedProfileSaysItIsGeneratedAndWhatSurvivesARecompose(t *testing.T) {
	_, dir := guidedLayout(t)
	data, err := os.ReadFile(filepath.Join(dir, ReadmeFile))
	if err != nil {
		t.Fatalf("a generated profile has no %s: %v", ReadmeFile, err)
	}
	for _, want := range []string{"generated by vibe", "vibe -C", "vibe --restore", "user-preferences.yaml", "agent-instructions.md"} {
		if !strings.Contains(string(data), want) {
			t.Errorf("%s lacks %q:\n%s", ReadmeFile, want, data)
		}
	}
}

// Said by the profile's own file, not by a header in the files composing
// wrote: that would change what every existing profile's hash is of.
func TestTheReadmeIsNotPartOfWhatMakesAProfileOutOfDateOrEdited(t *testing.T) {
	l, dir := guidedLayout(t)
	path := filepath.Join(dir, ReadmeFile)
	for name, change := range map[string]func(){
		"as written": func() {},
		"rewritten":  func() { write(t, path, "my notes on this profile\n", 0o644) },
		"deleted":    func() { os.Remove(path) },
	} {
		change()
		if got := check(t, l, dir); got != UpToDate {
			t.Errorf("%s: Check = %v, want UpToDate", name, got)
		}
	}
}

func TestAHashRecordedBeforeTheReadmeWasMadeStillHolds(t *testing.T) {
	l, dir := guidedLayout(t)
	os.Remove(filepath.Join(dir, ReadmeFile))
	if RecordedHash(dir) == "" {
		t.Fatal("no recorded hash")
	}
	if got := check(t, l, dir); got != UpToDate {
		t.Errorf("a profile composed before there was a readme: Check = %v", got)
	}
}

func TestAReadmeIsAddedToAGuidedProfileThatLacksOneAndNeverOverwritten(t *testing.T) {
	_, dir := guidedLayout(t)
	os.Remove(filepath.Join(dir, ReadmeFile))
	if added, err := AddReadme(dir); err != nil || !added {
		t.Fatalf("added %v, %v", added, err)
	}
	if _, err := os.Stat(filepath.Join(dir, ReadmeFile)); err != nil {
		t.Errorf("not added: %v", err)
	}
	write(t, filepath.Join(dir, ReadmeFile), "mine\n", 0o644)
	if added, err := AddReadme(dir); err != nil || added {
		t.Errorf("a readme that was there was added over: %v, %v", added, err)
	}
	if data, _ := os.ReadFile(filepath.Join(dir, ReadmeFile)); string(data) != "mine\n" {
		t.Errorf("the readme is %q", data)
	}
}

func TestAProfileThatWasNotGeneratedGetsNoReadme(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "config.yaml"), "name: mine\n", 0o644)
	if added, err := AddReadme(dir); err != nil || added {
		t.Errorf("added %v, %v", added, err)
	}
	if _, err := os.Stat(filepath.Join(dir, ReadmeFile)); err == nil {
		t.Error("a hand-made profile was given a note saying vibe generated it")
	}
}
