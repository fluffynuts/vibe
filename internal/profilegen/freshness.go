package profilegen

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"vibe/internal/claudetheme"
	"vibe/internal/instructions"
	"vibe/internal/layout"
)

// HashFile is where a guided profile records the hash of what composing it
// wrote (see ContentHash): what it was before anyone could edit it.
const HashFile = "composed-hash"

// notComposed are the files in a profile's folder that composing doesn't
// write, so they're left out of its hash: the user's own instructions and
// theme, which carry across a re-compose, and the hash itself.
var notComposed = map[string]bool{instructions.File: true, claudetheme.ProfileFile: true, HashFile: true}

// ContentHash is a hash of what composing wrote into a profile's folder:
// every file's path and content, the user's own files aside.
func ContentHash(dir string) (string, error) {
	var paths []string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		if !d.IsDir() && !notComposed[rel] {
			paths = append(paths, filepath.ToSlash(rel))
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Strings(paths)
	h := sha256.New()
	for _, rel := range paths {
		data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
		if err != nil {
			return "", err
		}
		fmt.Fprintf(h, "%s\x00%d\x00", rel, len(data))
		h.Write(data)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// RecordHash records what dir's composition is now as what it was composed
// as, for Check to tell a hand edit from a library change by.
func RecordHash(dir string) error {
	sum, err := ContentHash(dir)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, HashFile), []byte(sum+"\n"), 0o644)
}

// RecordedHash is the hash RecordHash recorded in dir, or "" when there's
// none: a profile composed before vibe recorded it.
func RecordedHash(dir string) string {
	data, err := os.ReadFile(filepath.Join(dir, HashFile))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// Freshness is how a guided profile compares with the library now.
type Freshness int

const (
	// UpToDate: composing it again would write the same files.
	UpToDate Freshness = iota
	// OutOfDate: composing it again would differ, and nobody has edited it
	// since it was composed (or it's from before that was recorded), so
	// re-composing it loses nothing of anyone's.
	OutOfDate
	// Edited: it was changed by hand since it was composed. Re-composing it
	// would lose that, so that's for the user to do.
	Edited
)

// Check compares the guided profile in dir, named profile, with a fresh
// composition of features (what it records) from the library as it is now.
func Check(l layout.Layout, dir, profile string, features []string) (Freshness, error) {
	scratch, err := os.MkdirTemp("", "vibe-compose-")
	if err != nil {
		return 0, err
	}
	defer os.RemoveAll(scratch)
	fresh := filepath.Join(scratch, profile)
	if err := composeInto(l, fresh, profile, features); err != nil {
		return 0, fmt.Errorf("composing profile '%s' afresh to compare: %w", profile, err)
	}
	want, err := ContentHash(fresh)
	if err != nil {
		return 0, err
	}
	have, err := ContentHash(dir)
	if err != nil {
		return 0, err
	}
	recorded := RecordedHash(dir)
	switch {
	case have == want:
		return UpToDate, nil
	case recorded != "" && recorded != have:
		return Edited, nil
	default:
		return OutOfDate, nil
	}
}
