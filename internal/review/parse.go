// Package review is the companion page's Review tab's: the diff of a
// sandbox's folder, worked out on the host with git, and the review of it
// (see docs/plan-review-tab.md).
package review

import (
	"fmt"
	"regexp"
	"strconv"
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
	// Lines is how many lines the file has: as the review has it, or, for
	// a deleted one, as it had; 0 for a binary one, or when it couldn't be
	// read (then the page just doesn't say).
	Lines int `json:"lines,omitempty"`
	// Fingerprint stands for the file's change (its hunks): the same while
	// the change is, so a file ticked as viewed is known to have changed
	// since.
	Fingerprint string `json:"fingerprint,omitempty"`
	Hunks       []Hunk `json:"hunks"`
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

var hunkHeader = regexp.MustCompile(`^@@ -(\d+)(?:,\d+)? \+(\d+)(?:,\d+)? @@ ?(.*)$`)

// Parse reads a unified diff, as git diff writes it with a/ and b/
// prefixes, into its files.
func Parse(unified string) ([]File, error) {
	var files []File
	var f *File
	var h *Hunk
	old, new := 0, 0
	for n, line := range strings.Split(unified, "\n") {
		switch {
		case strings.HasPrefix(line, "diff --git "):
			files = append(files, File{Status: Modified})
			f, h = &files[len(files)-1], nil
			// Paths come from the lines below, which can't be ambiguous as
			// this one can (a path may have " b/" in it); a file with no
			// such lines (a pure rename, a binary file) keeps these.
			if a, b, ok := splitGitPaths(strings.TrimPrefix(line, "diff --git ")); ok {
				f.OldPath, f.Path = a, b
			}
		case f == nil:
			// Anything before the first file (a commit's message, say).
		case h == nil && strings.HasPrefix(line, "new file mode"):
			f.Status = Added
		case h == nil && strings.HasPrefix(line, "deleted file mode"):
			f.Status = Deleted
		case h == nil && strings.HasPrefix(line, "rename from "):
			f.Status, f.OldPath = Renamed, unquote(strings.TrimPrefix(line, "rename from "))
		case h == nil && strings.HasPrefix(line, "rename to "):
			f.Status, f.Path = Renamed, unquote(strings.TrimPrefix(line, "rename to "))
		case h == nil && strings.HasPrefix(line, "Binary files "):
			f.Binary = true
		case h == nil && strings.HasPrefix(line, "--- "):
			if p := strings.TrimPrefix(line, "--- "); p != "/dev/null" {
				f.OldPath = strings.TrimPrefix(unquote(p), "a/")
			}
		case h == nil && strings.HasPrefix(line, "+++ "):
			if p := strings.TrimPrefix(line, "+++ "); p != "/dev/null" {
				f.Path = strings.TrimPrefix(unquote(p), "b/")
			}
		case strings.HasPrefix(line, "@@ "):
			m := hunkHeader.FindStringSubmatch(line)
			if m == nil {
				return nil, fmt.Errorf("line %d: not a hunk header: %q", n+1, line)
			}
			old, _ = strconv.Atoi(m[1])
			new, _ = strconv.Atoi(m[2])
			f.Hunks = append(f.Hunks, Hunk{OldStart: old, NewStart: new, Section: m[3]})
			h = &f.Hunks[len(f.Hunks)-1]
		case h == nil:
			// index, mode and similarity lines: nothing the tab shows.
		case strings.HasPrefix(line, " "):
			h.Lines = append(h.Lines, Line{Kind: Context, Old: old, New: new, Text: line[1:]})
			old++
			new++
		case strings.HasPrefix(line, "-"):
			h.Lines = append(h.Lines, Line{Kind: Deletion, Old: old, Text: line[1:]})
			old++
		case strings.HasPrefix(line, "+"):
			h.Lines = append(h.Lines, Line{Kind: Addition, New: new, Text: line[1:]})
			new++
		case strings.HasPrefix(line, `\`):
			// "\ No newline at end of file": about the line before.
		}
	}
	for i := range files {
		switch files[i].Status {
		case Added:
			files[i].OldPath = ""
		case Deleted:
			files[i].Path = files[i].OldPath
		}
	}
	return files, nil
}

// splitGitPaths splits the a/… b/… of a "diff --git" line, quoted or not.
// Unquoted, it can only be split surely when both paths are the same, which
// is the case it's needed for (a binary file, a mode change).
func splitGitPaths(s string) (a, b string, ok bool) {
	if strings.HasPrefix(s, `"`) {
		end := closingQuote(s)
		if end < 0 {
			return "", "", false
		}
		a = unquote(s[:end+1])
		b = unquote(strings.TrimSpace(s[end+1:]))
		return strings.TrimPrefix(a, "a/"), strings.TrimPrefix(b, "b/"), true
	}
	half := (len(s) - 1) / 2
	if len(s)%2 == 1 && s[half] == ' ' && strings.HasPrefix(s, "a/") && s[half+1:half+3] == "b/" && s[2:half] == s[half+3:] {
		return s[2:half], s[half+3:], true
	}
	return "", "", false
}

// closingQuote is the index of the quote ending the quoted string s starts
// with, or -1.
func closingQuote(s string) int {
	for i := 1; i < len(s); i++ {
		switch s[i] {
		case '\\':
			i++
		case '"':
			return i
		}
	}
	return -1
}

// unquote undoes git's C-style quoting of a path, when it's quoted.
func unquote(p string) string {
	if strings.HasPrefix(p, `"`) {
		if s, err := strconv.Unquote(p); err == nil {
			return s
		}
	}
	return p
}
