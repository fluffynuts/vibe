// Package review is the companion page's Review tab's: the diff of a
// sandbox's folder, worked out on the host with git, and the review of it
// (see docs/adr/2026-10-15-review-tab.md).
package review

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"
)

// A file's Status.
const (
	Modified = "modified"
	Added    = "added"
	Deleted  = "deleted"
	Renamed  = "renamed"
)

// A line's Kind.
const (
	Context  = "context"
	Addition = "add"
	Deletion = "del"
)

// File is one file's part of a diff. OldPath is "" for an added file; Path
// is the old path for a deleted one, so every file has one to be known by.
type File struct {
	Path    string `json:"path"`
	OldPath string `json:"oldPath,omitempty"`
	Status  string `json:"status"`
	Binary  bool   `json:"binary,omitempty"`
	// TooLarge is an untracked file too large to show the lines of.
	TooLarge bool `json:"tooLarge,omitempty"`
	// Unreadable is why an untracked file's content couldn't be read.
	Unreadable string `json:"unreadable,omitempty"`
	// Lines is how many lines the file has: as the review has it, or, for
	// a deleted one, as it had; 0 for a binary one, or when it couldn't be
	// read (then the page just doesn't say).
	Lines int `json:"lines,omitempty"`
	// Fingerprint stands for the file's change (its hunks): the same while
	// the change is, so a file ticked as viewed is known to have changed
	// since.
	Fingerprint string `json:"fingerprint,omitempty"`
	Hunks       []Hunk `json:"hunks"`
	// stamp is what git says of the change besides its hunks (the blobs'
	// ids, modes; for an untracked file, a digest of its content, else its
	// size and time): what the fingerprint is made of.
	stamp string
}

// DiffKey stands for a diff as a whole: the same while no file's change is (its path, its status, its fingerprint), whatever order
// the files came in, and other when any is. The page holds the key of the diff it shows, and asks, once the agent's turn is over,
// whether the code is still the one it stood for.
func DiffKey(files []File) string {
	lines := make([]string, 0, len(files))
	for _, f := range files {
		lines = append(lines, f.Path+"\x00"+f.Status+"\x00"+f.Fingerprint)
	}
	sort.Strings(lines)
	sum := sha256.Sum256([]byte(strings.Join(lines, "\n")))
	return hex.EncodeToString(sum[:])
}

// Hunk is a run of changed lines with their context. Section is what git
// says the hunk is in (a function, say), when it says.
type Hunk struct {
	OldStart int    `json:"oldStart"`
	NewStart int    `json:"newStart"`
	Section  string `json:"section,omitempty"`
	Lines    []Line `json:"lines"`
}

// Line is a line of a hunk. Old is its number in the old file and New in
// the new one; 0 where it isn't in that file (an added line has no Old).
type Line struct {
	Kind string `json:"kind"`
	Old  int    `json:"old,omitempty"`
	New  int    `json:"new,omitempty"`
	Text string `json:"text"`
}
