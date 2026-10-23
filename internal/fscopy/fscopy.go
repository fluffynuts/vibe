// Package fscopy copies files and directory trees between vibe's layers,
// preserving file modes (install scripts have to stay executable).
package fscopy

import (
	"archive/zip"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"time"
	"vibe/internal/pathresolve"
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
	return WriteFrom(in, dst, mode)
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

// TreeKeepTimes is Tree, giving every file and directory copied its
// source's modification time too — for a copy that stands in for the
// original, such as a backup put back. Either folder may be one a sandbox
// can write to while the copy runs (a memory store), so both are opened as
// roots: nothing is read or written outside them, whatever links appear in
// them, and a link is copied as a link, never followed.
func TreeKeepTimes(src, dst string) error {
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	from, err := os.OpenRoot(src)
	if err != nil {
		return err
	}
	defer from.Close()
	to, err := os.OpenRoot(dst)
	if err != nil {
		return err
	}
	defer to.Close()
	type stamp struct {
		path string
		mod  time.Time
	}
	var stamps []stamp
	err = fs.WalkDir(from.FS(), ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		switch {
		case d.IsDir():
			if err := to.MkdirAll(path, info.Mode().Perm()|0o700); err != nil {
				return err
			}
		case info.Mode()&os.ModeSymlink != 0:
			link, err := from.Readlink(path)
			if err != nil {
				return err
			}
			return to.Symlink(link, path)
		case !info.Mode().IsRegular():
			return nil
		default:
			if err := copyInRoots(from, to, path, info.Mode().Perm()); err != nil {
				return err
			}
		}
		stamps = append(stamps, stamp{path, info.ModTime()})
		return nil
	})
	if err != nil {
		return err
	}
	// Only once everything is copied: copying into a directory updates its
	// modification time.
	for _, s := range stamps {
		if err := to.Chtimes(s.path, s.mod, s.mod); err != nil {
			return err
		}
	}
	return nil
}

// copyInRoots copies the file path in from to the same path in to.
func copyInRoots(from, to *os.Root, path string, mode os.FileMode) error {
	in, err := from.Open(path)
	if err != nil {
		return err
	}
	defer in.Close()
	if info, err := in.Stat(); err != nil || !info.Mode().IsRegular() {
		return fmt.Errorf("%s changed while it was being copied", path)
	}
	out, err := to.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// WriteWhole makes path hold data, readable by the user alone, through a
// temporary file of its own (created afresh, never one already there)
// renamed over it: a reader never sees it half-written, and it never sits
// in a file someone else could read, even briefly.
func WriteWhole(path string, data []byte) error {
	return WriteWholeMode(path, data, 0o600)
}

// WriteWholeMode is WriteWhole for a file that others may read (a settings
// file the user edits, say): it ends up with mode perm, not the temporary
// file's 0600, and never half-written, as os.WriteFile leaves it to a reader
// that comes between its truncating the file and finishing writing it.
func WriteWholeMode(path string, data []byte, perm os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Chmod(perm); err != nil && runtime.GOOS != "windows" {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	if err := retryInUse(func() error { return rename(tmp.Name(), path) }); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return nil
}

// WriteFrom writes what r holds to target, making its folder, with mode
// (0644 when 0) applied past the umask, so an executable stays one.
func WriteFrom(r io.Reader, target string, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	if mode == 0 {
		mode = 0o644
	}
	out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, r); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Chmod(target, mode)
}

// Unzip extracts the zip at zipPath into dest, modes and all, and returns
// its entries' names. Every entry must land inside dest: one whose name
// reaches out of it ("../", "zip slip") is refused, not followed.
func Unzip(zipPath, dest string) ([]string, error) {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return nil, err
	}
	var names []string
	for _, f := range zr.File {
		target := filepath.Join(dest, filepath.FromSlash(f.Name))
		if !pathresolve.Within(dest, target) {
			return nil, fmt.Errorf("%s: entry %q would land outside %s", zipPath, f.Name, dest)
		}
		names = append(names, f.Name)
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return nil, err
			}
			continue
		}
		in, err := f.Open()
		if err != nil {
			return nil, err
		}
		err = WriteFrom(in, target, f.Mode().Perm())
		in.Close()
		if err != nil {
			return nil, err
		}
	}
	return names, nil
}
