package profilegen

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"vibe/internal/instructions"
	"vibe/internal/layout"
	"vibe/internal/userprefs"
)

// HashFile is where a guided profile records the hash of what composing it
// wrote (see ContentHash): what it was before anyone could edit it.
const HashFile = "composed-hash"

// notComposed are the files in a profile's folder that composing doesn't
// write, so they're left out of its hash: the user's own instructions and
// preferences, which carry across a re-compose (and the files earlier vibes
// kept single preferences in, until they are moved into the preferences), and
// the hash itself.
var notComposed = notComposedFiles()

func notComposedFiles() map[string]bool {
	files := map[string]bool{instructions.File: true, userprefs.File: true, HashFile: true}
	for _, old := range userprefs.LegacyFiles {
		files[old] = true
	}
	return files
}

// ContentHash is a hash of what composing wrote into a profile's folder:
// every file's path and content, the user's own files aside.
func ContentHash(dir string) (string, error) { return ContentHashWith(dir, nil) }

// ContentHashWith is ContentHash as it would be if the files in changed (by
// path from dir, with forward slashes) held what it says, or were gone for a
// nil: to know what changing a file would do to it, without changing it.
func ContentHashWith(dir string, changed map[string][]byte) (string, error) {
	var paths []string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		if slash := filepath.ToSlash(rel); !d.IsDir() && !notComposed[rel] && !removed(changed, slash) {
			paths = append(paths, slash)
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	// A file that isn't there yet, but would be.
	for rel, data := range changed {
		if data != nil && !notComposed[rel] && !slices.Contains(paths, rel) {
			paths = append(paths, rel)
		}
	}
	sort.Strings(paths)
	h := sha256.New()
	for _, rel := range paths {
		data, ok := changed[rel]
		if !ok {
			var err error
			if data, err = os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel))); err != nil {
				return "", err
			}
		}
		fmt.Fprintf(h, "%s\x00%d\x00", rel, len(data))
		h.Write(data)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// removed says rel is a file changed says is gone.
func removed(changed map[string][]byte, rel string) bool {
	data, ok := changed[rel]
	return ok && data == nil
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

// ComposedFile is the file rel (from the profile's folder, with forward
// slashes) as composing features for profile writes it now, in the library as
// it is: ok is false for a file composing doesn't write. To know what a file
// of the profile would be but for what the user has done to it.
func ComposedFile(l layout.Layout, profile string, features []string, rel string) (data []byte, ok bool, err error) {
	scratch, err := os.MkdirTemp("", "vibe-compose-")
	if err != nil {
		return nil, false, err
	}
	defer os.RemoveAll(scratch)
	fresh := filepath.Join(scratch, profile)
	if err := composeInto(l, fresh, profile, features); err != nil {
		return nil, false, fmt.Errorf("composing profile '%s' afresh: %w", profile, err)
	}
	data, err = os.ReadFile(filepath.Join(fresh, filepath.FromSlash(rel)))
	if os.IsNotExist(err) {
		return nil, false, nil
	}
	return data, err == nil, err
}

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
