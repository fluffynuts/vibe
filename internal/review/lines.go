package review

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// maxFileLines bounds a file read for its context: a review of a huge
// generated file has no use for all of it.
const maxFileLines = 200000

// FileLines is path's lines, as the diff of r has its new side: the file as
// it is in dir, or as the commit r is of has it. They're what the Review
// tab shows when asked for more of a file's context than the diff has.
// path is relative to dir, and nothing outside dir is read: not by .., not
// through a symbolic link.
func FileLines(ctx context.Context, dir string, r Review, path string) ([]string, error) {
	clean := filepath.Clean(filepath.FromSlash(path))
	if path == "" || filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return nil, fmt.Errorf("%q isn't a file in the folder", path)
	}
	var text string
	if r.Commit != "" {
		if err := isCommit(ctx, dir, r.Commit); err != nil {
			return nil, err
		}
		out, err := run(ctx, dir, "show", r.Commit+":"+filepath.ToSlash(clean))
		if err != nil {
			return nil, err
		}
		text = out
	} else {
		root, err := filepath.EvalSymlinks(dir)
		if err != nil {
			return nil, err
		}
		full, err := filepath.EvalSymlinks(filepath.Join(root, clean))
		if err != nil {
			return nil, fmt.Errorf("%q can't be read: %w", path, err)
		}
		if rel, err := filepath.Rel(root, full); err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return nil, fmt.Errorf("%q isn't a file in the folder", path)
		}
		data, err := os.ReadFile(full)
		if err != nil {
			return nil, fmt.Errorf("%q can't be read: %w", path, err)
		}
		text = string(data)
	}
	lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	if len(lines) > maxFileLines {
		lines = lines[:maxFileLines]
	}
	for i, l := range lines {
		lines[i] = strings.TrimSuffix(l, "\r")
	}
	return lines, nil
}
