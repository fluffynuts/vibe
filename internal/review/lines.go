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

// fileLines is path's lines, as the diff of r has its new side (see
// NewTextOf). They're what the Review tab shows when asked for more of a
// file's context than the diff has.
func fileLines(ctx context.Context, dir string, r Review, path string) ([]string, error) {
	text, err := newTextOf(ctx, dir, r, path)
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

// newTextOf is path's text as review r has its new side: in the commit r is
// of, at HEAD for unpushed commits, else in the folder dir as it is. path is
// relative to the repository's top, and nothing outside it is read: not by
// "..", not through a link. (A comment's file, which this reads, comes from
// the agent.)
func newTextOf(ctx context.Context, dir string, r Review, path string) (string, error) {
	clean, err := inFolder(path)
	if err != nil {
		return "", err
	}
	repo, err := open(dir)
	if err != nil {
		return "", err
	}
	defer repo.close()
	switch {
	case r.Commit != "":
		c, err := repo.commit(r.Commit)
		if err != nil {
			return "", err
		}
		return blobText(c, filepath.ToSlash(clean))
	case r.Unpushed:
		head, err := repo.head()
		if err != nil {
			return "", err
		}
		if head == nil {
			return "", fmt.Errorf("the folder has no commits")
		}
		return blobText(head, filepath.ToSlash(clean))
	}
	// Only a plain file in the folder: a FIFO would wait forever, and a link
	// out of it isn't followed.
	data, err := readIn(repo.work, clean, maxFileBytes)
	if err != nil {
		return "", fmt.Errorf("%q can't be read: %w", path, err)
	}
	return string(data), nil
}
