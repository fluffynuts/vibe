// Package upgrade installs a release's bundle into ~/.vibe over whatever an
// earlier release put there, keeping the user's own edits.
//
// Every file the package ships is compared three ways: the user's copy in
// ~/.vibe, the new package's, and the package's version from the last
// install, kept in ~/.vibe/package-files. That original is what tells who
// changed a file since, the same way git uses a merge base:
//
//   - only the package changed it: the user never edited it, so it is
//     updated without asking;
//   - only the user changed it: nothing new upstream, so it is kept;
//   - both changed it (or there's no original yet): it's a conflict, for the
//     user or the --update-strategy to settle — merging where git's
//     three-way merge can, keeping or overwriting where it can't.
//
// A file's original only moves forward when the user's copy took in the
// package's version (copied, updated, merged or overwritten, or already
// identical). A file the user keeps holds on to its old original, so a
// later upgrade can still merge against it.
package upgrade

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"vibe/internal/fscopy"
)

// BaseDir is where, under ~/.vibe, the package's version of every file from
// the last install is kept, as the base for three-way merges.
const BaseDir = "package-files"

// UpdatedSuffix is added to the name of the package's version of a file that
// couldn't be merged, written beside the user's copy for them to merge.
const UpdatedSuffix = ".updated"

// packageFiles and packageDirs are what a release ships into ~/.vibe.
var (
	packageFiles = []string{"config.yaml", "settings.yaml"}
	packageDirs  = []string{"defaults", "profiles", "library"}
)

// Fallback is what a strategy does with a file it doesn't merge.
type Fallback int

const (
	// Keep leaves the user's copy as it is.
	Keep Fallback = iota
	// Update replaces the user's copy with the package's.
	Update
)

// Strategy settles conflicts without asking: merge where possible if Merge
// is set, and otherwise fall back. The zero value is "no strategy given".
type Strategy struct {
	Given     bool
	Merge     bool
	Otherwise Fallback
}

// StrategyValues are the --update-strategy values ParseStrategy accepts.
const StrategyValues = "keep, update, merge,keep or merge,update (merge alone means merge,keep)"

// ParseStrategy reads an --update-strategy value.
func ParseStrategy(s string) (Strategy, error) {
	switch strings.ReplaceAll(strings.ToLower(s), " ", "") {
	case "keep":
		return Strategy{Given: true, Otherwise: Keep}, nil
	case "update":
		return Strategy{Given: true, Otherwise: Update}, nil
	case "merge", "merge,keep":
		return Strategy{Given: true, Merge: true, Otherwise: Keep}, nil
	case "merge,update":
		return Strategy{Given: true, Merge: true, Otherwise: Update}, nil
	}
	return Strategy{}, fmt.Errorf("unknown --update-strategy %q: use %s", s, StrategyValues)
}

// Conflict is a file both the user and the package have changed (or one
// with no original to tell by), as offered to Options.Decide.
type Conflict struct {
	Rel          string // path relative to ~/.vibe
	Mine, Theirs string // the user's copy and the package's
	MineText     []byte
	TheirsText   []byte
	// Merged is the result of merging the package's changes into the
	// user's copy, when CanMerge; Why says why not otherwise.
	Merged   []byte
	CanMerge bool
	Why      string
}

// Choice is how a conflict was settled.
type Choice int

const (
	KeepMine Choice = iota
	TakeMerged
	TakeTheirs
)

// Options configures Run.
type Options struct {
	Package  string // the release's bundle: where the vibe binary is
	Home     string // ~/.vibe
	Strategy Strategy
	// Decide asks the user to settle a conflict; nil when there's no one to
	// ask. It isn't consulted when a Strategy is given.
	Decide func(Conflict) (Choice, error)
	// Log reports each file acted on.
	Log func(format string, a ...interface{})
}

// Result records what Run did, as paths relative to ~/.vibe.
type Result struct {
	Copied      []string // new in the package
	Updated     []string // unedited, so updated without asking
	KeptOwn     []string // only the user had changed it
	Merged      []string
	Overwritten []string
	Kept        []string // a conflict settled by keeping the user's copy
	LeftDeleted []string // shipped before and deleted by the user since
	Identical   int
	// UpdatedCopies are package versions written beside unmergeable files.
	UpdatedCopies []string
	// Unresolved are conflicts left alone: no strategy, and no one to ask.
	Unresolved []string
}

// Run installs the package into Home.
func Run(o Options) (Result, error) {
	var res Result
	if o.Log == nil {
		o.Log = func(string, ...interface{}) {}
	}
	rels, err := PackageFiles(o.Package)
	if err != nil {
		return res, err
	}
	for _, rel := range rels {
		if err := o.install(rel, &res); err != nil {
			return res, err
		}
	}
	if err := pruneBase(filepath.Join(o.Home, BaseDir), rels); err != nil {
		return res, err
	}
	return res, nil
}

// SeedBase records, as the original of every package file, the copy in
// Home that is identical to it — for files seeded into ~/.vibe some other
// way (vibe's first run), so the next upgrade has something to merge
// against.
func SeedBase(pkg, home string) error {
	rels, err := PackageFiles(pkg)
	if err != nil {
		return err
	}
	for _, rel := range rels {
		theirs, err := os.ReadFile(filepath.Join(pkg, rel))
		if err != nil {
			return err
		}
		mine, err := os.ReadFile(filepath.Join(home, rel))
		if err != nil || !bytes.Equal(mine, theirs) {
			continue
		}
		if err := fscopy.File(filepath.Join(pkg, rel), filepath.Join(home, BaseDir, rel)); err != nil {
			return err
		}
	}
	return nil
}

// PackageFiles lists, sorted, every regular file a release ships into
// ~/.vibe, relative to the bundle root.
func PackageFiles(pkg string) ([]string, error) {
	var rels []string
	for _, rel := range packageFiles {
		if info, err := os.Stat(filepath.Join(pkg, rel)); err == nil && info.Mode().IsRegular() {
			rels = append(rels, rel)
		}
	}
	for _, dir := range packageDirs {
		root := filepath.Join(pkg, dir)
		if info, err := os.Stat(root); err != nil || !info.IsDir() {
			continue
		}
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil || !d.Type().IsRegular() || strings.HasSuffix(d.Name(), UpdatedSuffix) {
				return err
			}
			rel, err := filepath.Rel(pkg, path)
			if err != nil {
				return err
			}
			rels = append(rels, rel)
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	sort.Strings(rels)
	return rels, nil
}

func (o Options) install(rel string, res *Result) error {
	theirs := filepath.Join(o.Package, rel)
	mine := filepath.Join(o.Home, rel)
	base := filepath.Join(o.Home, BaseDir, rel)
	advance := func() error { return fscopy.File(theirs, base) }

	theirsText, err := os.ReadFile(theirs)
	if err != nil {
		return err
	}
	baseText, baseErr := os.ReadFile(base)
	hasBase := baseErr == nil

	mineText, err := os.ReadFile(mine)
	if errors.Is(err, fs.ErrNotExist) {
		if hasBase {
			res.LeftDeleted = append(res.LeftDeleted, rel)
			o.Log("left deleted: %s (you removed it)", rel)
			return advance()
		}
		if err := fscopy.File(theirs, mine); err != nil {
			return err
		}
		res.Copied = append(res.Copied, rel)
		o.Log("copied %s", rel)
		return advance()
	}
	if err != nil {
		return err
	}

	switch {
	case bytes.Equal(mineText, theirsText):
		res.Identical++
		return advance()
	case hasBase && bytes.Equal(mineText, baseText):
		if err := fscopy.File(theirs, mine); err != nil {
			return err
		}
		res.Updated = append(res.Updated, rel)
		o.Log("updated %s", rel)
		return advance()
	case hasBase && bytes.Equal(theirsText, baseText):
		res.KeptOwn = append(res.KeptOwn, rel)
		o.Log("kept %s (only your changes)", rel)
		return nil
	}

	c := Conflict{Rel: rel, Mine: mine, Theirs: theirs, MineText: mineText, TheirsText: theirsText}
	c.Merged, c.CanMerge, c.Why = merge(mine, base, theirs, hasBase, mineText, baseText, theirsText)
	return o.settle(c, res, advance)
}

func (o Options) settle(c Conflict, res *Result, advance func() error) error {
	switch {
	case o.Strategy.Given && o.Strategy.Merge && c.CanMerge:
		return o.take(c, TakeMerged, res, advance)
	case o.Strategy.Given && o.Strategy.Otherwise == Update:
		return o.take(c, TakeTheirs, res, advance)
	case o.Strategy.Given && o.Strategy.Merge:
		// merge,keep, and this one wouldn't merge: keep it, with the
		// package's version beside it to merge by hand.
		if err := fscopy.File(c.Theirs, c.Mine+UpdatedSuffix); err != nil {
			return err
		}
		res.Kept = append(res.Kept, c.Rel)
		res.UpdatedCopies = append(res.UpdatedCopies, c.Rel+UpdatedSuffix)
		o.Log("kept %s: couldn't merge (%s) — the new version is beside it as %s", c.Rel, c.Why, c.Rel+UpdatedSuffix)
		return nil
	case o.Strategy.Given:
		return o.take(c, KeepMine, res, advance)
	case o.Decide != nil:
		choice, err := o.Decide(c)
		if err != nil {
			return err
		}
		return o.take(c, choice, res, advance)
	default:
		res.Unresolved = append(res.Unresolved, c.Rel)
		o.Log("left alone: %s (changed both here and in the package)", c.Rel)
		return nil
	}
}

func (o Options) take(c Conflict, choice Choice, res *Result, advance func() error) error {
	switch choice {
	case TakeMerged:
		info, err := os.Stat(c.Mine)
		if err != nil {
			return err
		}
		if err := os.WriteFile(c.Mine, c.Merged, info.Mode().Perm()); err != nil {
			return err
		}
		res.Merged = append(res.Merged, c.Rel)
		o.Log("merged %s", c.Rel)
		return advance()
	case TakeTheirs:
		if err := fscopy.File(c.Theirs, c.Mine); err != nil {
			return err
		}
		res.Overwritten = append(res.Overwritten, c.Rel)
		o.Log("overwrote %s with the new version", c.Rel)
		return advance()
	default:
		res.Kept = append(res.Kept, c.Rel)
		o.Log("kept %s", c.Rel)
		return nil
	}
}

// merge merges the package's changes to a file (base → theirs) into the
// user's copy, with git's own three-way merge. ok is false, with a reason,
// when that can't be done cleanly.
func merge(mine, base, theirs string, hasBase bool, mineText, baseText, theirsText []byte) (merged []byte, ok bool, why string) {
	switch {
	case !hasBase:
		return nil, false, "there's no saved original to merge against yet"
	case isBinary(mineText) || isBinary(baseText) || isBinary(theirsText):
		return nil, false, "it isn't a text file"
	}
	git, err := exec.LookPath("git")
	if err != nil {
		return nil, false, "git isn't installed"
	}
	var out, errOut bytes.Buffer
	cmd := exec.Command(git, "merge-file", "-p", "-L", "yours", "-L", "original", "-L", "package", mine, base, theirs)
	cmd.Stdout, cmd.Stderr = &out, &errOut
	err = cmd.Run()
	var exit *exec.ExitError
	switch {
	case err == nil:
		return out.Bytes(), true, ""
	case errors.As(err, &exit) && exit.ExitCode() > 0 && exit.ExitCode() < 128:
		return nil, false, "your changes and the package's overlap"
	default:
		return nil, false, "git merge-file failed: " + strings.TrimSpace(errOut.String()+" "+err.Error())
	}
}

// isBinary guesses the way git does: a NUL byte in the first 8000.
func isBinary(b []byte) bool {
	if len(b) > 8000 {
		b = b[:8000]
	}
	return bytes.IndexByte(b, 0) >= 0
}

// pruneBase drops originals of files the package no longer ships.
func pruneBase(base string, rels []string) error {
	keep := map[string]bool{}
	for _, rel := range rels {
		keep[rel] = true
	}
	err := filepath.WalkDir(base, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, err := filepath.Rel(base, path)
		if err != nil || keep[rel] {
			return err
		}
		return os.Remove(path)
	})
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}
