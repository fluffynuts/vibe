// Package fscopy copies files and directory trees between vibe's layers,
// preserving file modes (install scripts have to stay executable).
package fscopy

import (
	"io"
	"os"
	"path/filepath"
)

// File copies src to dst, creating dst's parent directory and giving dst
// src's permissions.
func File(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	return fileWithMode(src, dst, info.Mode().Perm())
}

func fileWithMode(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// Tree copies the directory tree rooted at src to dst, creating dst.
// Symlinks are recreated as symlinks; anything that is neither a regular
// file, a directory nor a symlink is skipped.
func Tree(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		switch {
		case info.IsDir():
			return os.MkdirAll(target, info.Mode().Perm()|0o700)
		case info.Mode()&os.ModeSymlink != 0:
			link, err := os.Readlink(path)
			if err != nil {
				return err
			}
			return os.Symlink(link, target)
		case !info.Mode().IsRegular():
			return nil // sockets, devices and the like have no place in a profile
		default:
			return fileWithMode(path, target, info.Mode().Perm())
		}
	})
}

// TreeMerge copies the directory tree rooted at src into dst like Tree,
// except a file already present at the destination is left exactly as it
// is rather than overwritten — so merging the same source in twice (e.g.
// --install run again after fetching a newer bundle) never clobbers a
// local edit. Directories are still created as needed.
func TreeMerge(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		switch {
		case info.IsDir():
			return os.MkdirAll(target, info.Mode().Perm()|0o700)
		default:
			if _, statErr := os.Stat(target); statErr == nil {
				return nil // already there — leave it alone
			}
			if info.Mode()&os.ModeSymlink != 0 {
				link, err := os.Readlink(path)
				if err != nil {
					return err
				}
				return os.Symlink(link, target)
			}
			if !info.Mode().IsRegular() {
				return nil
			}
			return fileWithMode(path, target, info.Mode().Perm())
		}
	})
}
