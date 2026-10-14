package profilegen

import (
	"os"
	"path/filepath"
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
	write(t, filepath.Join(dir, "claude-theme"), "dark\n", 0o644)
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
