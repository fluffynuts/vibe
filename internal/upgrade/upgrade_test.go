package upgrade

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// fixture is a package, a ~/.vibe, and the originals from a previous
// install, each given as relative path → content ("" skips the file).
type fixture struct {
	pkg, home, base map[string]string
}

func (f fixture) build(t *testing.T) (pkg, home string) {
	t.Helper()
	pkg, home = t.TempDir(), t.TempDir()
	write := func(root string, files map[string]string) {
		for rel, content := range files {
			path := filepath.Join(root, filepath.FromSlash(rel))
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	write(pkg, f.pkg)
	write(home, f.home)
	write(filepath.Join(home, BaseDir), f.base)
	return pkg, home
}

func read(t *testing.T, path string) (string, bool) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	return string(b), true
}

func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("merging needs git")
	}
}

const rel = "defaults/install-scripts/01-thing"

var (
	original = "one\ntwo\nthree\nfour\nfive\nsix\nseven\n"
	// The package changed the top; the user changed the bottom: these merge.
	packageTop = "ONE\ntwo\nthree\nfour\nfive\nsix\nseven\n"
	userBottom = "one\ntwo\nthree\nfour\nfive\nsix\nSEVEN (mine)\n"
	bothMerged = "ONE\ntwo\nthree\nfour\nfive\nsix\nSEVEN (mine)\n"
	// Both changed the same line: these don't.
	packageClash = "one\ntwo\nthree\nFOUR (package)\nfive\nsix\nseven\n"
	userClash    = "one\ntwo\nthree\nFOUR (mine)\nfive\nsix\nseven\n"
)

func TestFilesOnlyOneSideChangedNeedNoDecision(t *testing.T) {
	f := fixture{
		pkg: map[string]string{
			"config.yaml":                "new config\n",
			"defaults/new-file":          "brand new\n",
			"defaults/same":              "same\n",
			"defaults/unedited":          "v2\n",
			"defaults/edited-by-me":      "v1\n",
			"defaults/deleted-by-me":     "v2\n",
			"profiles/p/config.yaml":     "p\n",
			"library/feature/config.yml": "f\n",
		},
		home: map[string]string{
			"config.yaml":            "new config\n",
			"defaults/same":          "same\n",
			"defaults/unedited":      "v1\n",
			"defaults/edited-by-me":  "v1 plus my edit\n",
			"profiles/p/config.yaml": "p\n",
			"instances/mine.yaml":    "not a package file\n",
		},
		base: map[string]string{
			"defaults/unedited":      "v1\n",
			"defaults/edited-by-me":  "v1\n",
			"defaults/deleted-by-me": "v1\n",
			"defaults/dropped":       "no longer shipped\n",
		},
	}
	pkg, home := f.build(t)
	res, err := Run(Options{Package: pkg, Home: home})
	if err != nil {
		t.Fatal(err)
	}

	check := func(label string, got, want []string) {
		t.Helper()
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s = %v, want %v", label, got, want)
		}
	}
	p := filepath.FromSlash
	check("copied", res.Copied, []string{p("defaults/new-file"), p("library/feature/config.yml")})
	check("updated", res.Updated, []string{p("defaults/unedited")})
	check("kept (only my changes)", res.KeptOwn, []string{p("defaults/edited-by-me")})
	check("left deleted", res.LeftDeleted, []string{p("defaults/deleted-by-me")})
	if res.Identical != 3 || len(res.Unresolved) != 0 {
		t.Errorf("identical = %d, unresolved = %v; want 3 and none", res.Identical, res.Unresolved)
	}

	if got, _ := read(t, filepath.Join(home, "defaults/unedited")); got != "v2\n" {
		t.Errorf("the unedited file wasn't updated: %q", got)
	}
	if got, _ := read(t, filepath.Join(home, "defaults/edited-by-me")); got != "v1 plus my edit\n" {
		t.Errorf("the user's edit was lost: %q", got)
	}
	if _, ok := read(t, filepath.Join(home, "defaults/deleted-by-me")); ok {
		t.Error("a file the user deleted came back")
	}
	if got, _ := read(t, filepath.Join(home, "instances/mine.yaml")); got != "not a package file\n" {
		t.Error("a file that isn't the package's was touched")
	}

	// Originals: advanced for everything that took the package's version,
	// left alone for the file kept, and gone for the one no longer shipped.
	base := filepath.Join(home, BaseDir)
	for rel, want := range map[string]string{
		"config.yaml": "new config\n", "defaults/new-file": "brand new\n", "defaults/unedited": "v2\n",
		"defaults/deleted-by-me": "v2\n", "defaults/edited-by-me": "v1\n",
	} {
		if got, _ := read(t, filepath.Join(base, rel)); got != want {
			t.Errorf("original of %s = %q, want %q", rel, got, want)
		}
	}
	if _, ok := read(t, filepath.Join(base, "defaults/dropped")); ok {
		t.Error("the original of a file the package dropped was kept")
	}
}

// Both sides changed the file. What each strategy does, for a change that
// merges cleanly and one that doesn't.
func TestStrategiesSettleConflicts(t *testing.T) {
	requireGit(t)
	for _, tc := range []struct {
		name, strategy       string
		user, pkg            string
		wantFile             string
		wantUpdatedCopy      bool
		wantOriginalAdvanced bool
	}{
		{"merge,keep that merges", "merge,keep", userBottom, packageTop, bothMerged, false, true},
		{"merge,keep that clashes", "merge,keep", userClash, packageClash, userClash, true, false},
		{"merge,update that clashes", "merge,update", userClash, packageClash, packageClash, false, true},
		{"merge alone is merge,keep", "merge", userClash, packageClash, userClash, true, false},
		{"keep", "keep", userBottom, packageTop, userBottom, false, false},
		{"update", "update", userBottom, packageTop, packageTop, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pkg, home := fixture{
				pkg:  map[string]string{rel: tc.pkg},
				home: map[string]string{rel: tc.user},
				base: map[string]string{rel: original},
			}.build(t)
			strategy, err := ParseStrategy(tc.strategy)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := Run(Options{Package: pkg, Home: home, Strategy: strategy}); err != nil {
				t.Fatal(err)
			}
			if got, _ := read(t, filepath.Join(home, rel)); got != tc.wantFile {
				t.Errorf("file = %q, want %q", got, tc.wantFile)
			}
			updated, hasCopy := read(t, filepath.Join(home, rel)+UpdatedSuffix)
			if hasCopy != tc.wantUpdatedCopy || (hasCopy && updated != tc.pkg) {
				t.Errorf(".updated copy present = %v (%q), want %v", hasCopy, updated, tc.wantUpdatedCopy)
			}
			gotBase, _ := read(t, filepath.Join(home, BaseDir, rel))
			if advanced := gotBase == tc.pkg; advanced != tc.wantOriginalAdvanced {
				t.Errorf("original advanced = %v, want %v", advanced, tc.wantOriginalAdvanced)
			}
		})
	}
}

// A file with no original can't be merged: a merge strategy keeps it (with
// the .updated copy), and without a strategy or anyone to ask it's left
// alone and reported.
func TestNoOriginalMeansNoMerge(t *testing.T) {
	requireGit(t)
	files := fixture{pkg: map[string]string{rel: packageTop}, home: map[string]string{rel: userBottom}}

	pkg, home := files.build(t)
	res, err := Run(Options{Package: pkg, Home: home, Strategy: Strategy{Given: true, Merge: true}})
	if err != nil || len(res.UpdatedCopies) != 1 {
		t.Fatalf("merge,keep with no original: %+v, %v; want an .updated copy", res, err)
	}

	pkg, home = files.build(t)
	res, err = Run(Options{Package: pkg, Home: home})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(res.Unresolved, []string{filepath.FromSlash(rel)}) {
		t.Errorf("unresolved = %v, want the one file", res.Unresolved)
	}
	if got, _ := read(t, filepath.Join(home, rel)); got != userBottom {
		t.Errorf("an unresolved file was changed: %q", got)
	}
	if _, ok := read(t, filepath.Join(home, BaseDir, rel)); ok {
		t.Error("an unresolved file's original moved forward")
	}
}

// Asked, the user sees whether the file merges and what it would become,
// and their choice is carried out.
func TestDecideIsAskedAndObeyed(t *testing.T) {
	requireGit(t)
	for _, tc := range []struct {
		name         string
		user, pkg    string
		choice       Choice
		wantCanMerge bool
		wantFile     string
	}{
		{"take the merge", userBottom, packageTop, TakeMerged, true, bothMerged},
		{"take the package's", userBottom, packageTop, TakeTheirs, true, packageTop},
		{"keep mine", userClash, packageClash, KeepMine, false, userClash},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pkg, home := fixture{
				pkg:  map[string]string{rel: tc.pkg},
				home: map[string]string{rel: tc.user},
				base: map[string]string{rel: original},
			}.build(t)
			var asked Conflict
			_, err := Run(Options{Package: pkg, Home: home, Decide: func(c Conflict) (Choice, error) {
				asked = c
				return tc.choice, nil
			}})
			if err != nil {
				t.Fatal(err)
			}
			if asked.CanMerge != tc.wantCanMerge {
				t.Errorf("CanMerge = %v (%s), want %v", asked.CanMerge, asked.Why, tc.wantCanMerge)
			}
			if tc.wantCanMerge && string(asked.Merged) != bothMerged {
				t.Errorf("Merged = %q, want %q", asked.Merged, bothMerged)
			}
			if !tc.wantCanMerge && !strings.Contains(asked.Why, "overlap") {
				t.Errorf("Why = %q, want it to say the changes overlap", asked.Why)
			}
			if got, _ := read(t, filepath.Join(home, rel)); got != tc.wantFile {
				t.Errorf("file = %q, want %q", got, tc.wantFile)
			}
		})
	}
}

func TestParseStrategy(t *testing.T) {
	for in, want := range map[string]Strategy{
		"keep":          {Given: true, Otherwise: Keep},
		"update":        {Given: true, Otherwise: Update},
		"merge":         {Given: true, Merge: true, Otherwise: Keep},
		"merge,keep":    {Given: true, Merge: true, Otherwise: Keep},
		"Merge, Update": {Given: true, Merge: true, Otherwise: Update},
	} {
		if got, err := ParseStrategy(in); err != nil || got != want {
			t.Errorf("ParseStrategy(%q) = %+v, %v; want %+v", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "update,merge", "keep,update", "merge,merge", "overwrite"} {
		if _, err := ParseStrategy(bad); err == nil {
			t.Errorf("ParseStrategy(%q) accepted it", bad)
		}
	}
}

func TestSeedBaseRecordsOnlyIdenticalFiles(t *testing.T) {
	pkg, home := fixture{
		pkg:  map[string]string{"settings.yaml": "s\n", "defaults/a": "a\n", "defaults/b": "b\n"},
		home: map[string]string{"settings.yaml": "s\n", "defaults/a": "a, edited\n"},
	}.build(t)
	if err := SeedBase(pkg, home); err != nil {
		t.Fatal(err)
	}
	if got, _ := read(t, filepath.Join(home, BaseDir, "settings.yaml")); got != "s\n" {
		t.Errorf("identical file's original = %q", got)
	}
	for _, rel := range []string{"defaults/a", "defaults/b"} {
		if _, ok := read(t, filepath.Join(home, BaseDir, rel)); ok {
			t.Errorf("%s got an original though it isn't the package's version", rel)
		}
	}
}
