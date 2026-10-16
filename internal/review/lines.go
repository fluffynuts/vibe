package review

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
)

// maxFileLines bounds a file read for its context: a review of a huge
// generated file has no use for all of it.
const maxFileLines = 200000

// maxFileBytes bounds reading a file of the folder: far more than any a
// review shows.
const maxFileBytes = 64 << 20

// FileLines is path's lines, as the diff of r has its new side (see
// NewTextOf). They're what the Review tab shows when asked for more of a
// file's context than the diff has.
func FileLines(ctx context.Context, dir string, r Review, path string) ([]string, error) {
	text, err := NewTextOf(ctx, dir, r, path)
	if err != nil {
		return nil, err
	}
	lines := splitLines(text)
	if len(lines) > maxFileLines {
		lines = lines[:maxFileLines]
	}
	return lines, nil
}

// inFolder is path cleaned, when it names something in the folder: relative,
// and not reaching out of it with "..".
func inFolder(path string) (string, error) {
	clean := filepath.Clean(filepath.FromSlash(path))
	if path == "" || filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%q isn't a file in the folder", path)
	}
	return clean, nil
}

// NewTextOf is path's text as review r has its new side: in the commit r is
// of, at HEAD for unpushed commits, else in the folder dir as it is. path is
// relative to dir, and nothing outside dir is read: not by .., not through
// a symbolic link. (A comment's file, which this reads, comes from the
// agent.)
func NewTextOf(ctx context.Context, dir string, r Review, path string) (string, error) {
	clean, err := inFolder(path)
	if err != nil {
		return "", err
	}
	switch {
	case r.Commit != "":
		if err := isCommit(ctx, dir, r.Commit); err != nil {
			return "", err
		}
		return run(ctx, dir, "show", r.Commit+":"+filepath.ToSlash(clean))
	case r.Unpushed:
		return run(ctx, dir, "show", "HEAD:"+filepath.ToSlash(clean))
	}
	root, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return "", err
	}
	full, err := filepath.EvalSymlinks(filepath.Join(root, clean))
	if err != nil {
		return "", fmt.Errorf("%q can't be read: %w", path, err)
	}
	if rel, err := filepath.Rel(root, full); err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%q isn't a file in the folder", path)
	}
	// Only a plain file: a FIFO would wait forever.
	data, err := readPlain(full, maxFileBytes)
	if err != nil {
		return "", fmt.Errorf("%q can't be read: %w", path, err)
	}
	return string(data), nil
}
