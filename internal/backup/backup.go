// Package backup keeps zipped copies of a sandbox's profile, so a profile
// that is about to be regenerated can be rolled back to.
package backup

import (
	"archive/zip"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Keep is how many backups are kept per sandbox.
const Keep = 3

const stamp = "2006-01-02_150405"

// Backup is one zipped profile.
type Backup struct {
	Path string
	Time time.Time
}

// Label is how the backup is shown in a list.
func (b Backup) Label() string {
	return b.Time.Format("2006-01-02 15:04:05")
}

// Create zips profileDir into dir as <sandbox>-<date>_<time>.zip, then
// removes all but the newest Keep backups of that sandbox.
func Create(dir, sandbox, profileDir string, now time.Time) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, sandbox+"-"+now.Format(stamp)+".zip")
	if err := zipDir(profileDir, path); err != nil {
		os.Remove(path)
		return "", err
	}
	all, err := List(dir, sandbox)
	if err != nil {
		return path, err
	}
	for i := Keep; i < len(all); i++ {
		os.Remove(all[i].Path)
	}
	return path, nil
}

// List returns sandbox's backups in dir, newest first. A missing dir has none.
func List(dir, sandbox string) ([]Backup, error) {
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Backup
	prefix := sandbox + "-"
	for _, e := range entries {
		n := e.Name()
		if e.IsDir() || !strings.HasPrefix(n, prefix) || !strings.HasSuffix(n, ".zip") {
			continue
		}
		t, err := time.ParseInLocation(stamp, strings.TrimSuffix(strings.TrimPrefix(n, prefix), ".zip"), time.Local)
		if err != nil {
			continue
		}
		out = append(out, Backup{Path: filepath.Join(dir, n), Time: t})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Time.After(out[j].Time) })
	return out, nil
}

// Restore replaces profileDir with the contents of the backup. It unpacks
// beside the profile first, so a bad zip leaves the profile as it was.
func Restore(zipPath, profileDir string) error {
	tmp := profileDir + ".restoring"
	os.RemoveAll(tmp)
	if err := unzipTo(zipPath, tmp); err != nil {
		os.RemoveAll(tmp)
		return err
	}
	if err := os.RemoveAll(profileDir); err != nil {
		os.RemoveAll(tmp)
		return err
	}
	return os.Rename(tmp, profileDir)
}

func zipDir(src, dest string) (err error) {
	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := f.Close(); err == nil {
			err = cerr
		}
	}()
	zw := zip.NewWriter(f)
	err = filepath.WalkDir(src, func(p string, d fs.DirEntry, werr error) error {
		if werr != nil {
			return werr
		}
		rel, err := filepath.Rel(src, p)
		if err != nil || rel == "." || !d.Type().IsRegular() {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		h, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}
		h.Name = filepath.ToSlash(rel)
		h.Method = zip.Deflate
		w, err := zw.CreateHeader(h)
		if err != nil {
			return err
		}
		in, err := os.Open(p)
		if err != nil {
			return err
		}
		defer in.Close()
		_, err = io.Copy(w, in)
		return err
	})
	if err != nil {
		zw.Close()
		return err
	}
	return zw.Close()
}

func unzipTo(zipPath, dest string) error {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer zr.Close()
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return err
	}
	root := filepath.Clean(dest) + string(os.PathSeparator)
	for _, zf := range zr.File {
		target := filepath.Join(dest, filepath.FromSlash(zf.Name))
		if !strings.HasPrefix(target, root) {
			return fmt.Errorf("backup %s has an entry outside the profile: %s", zipPath, zf.Name)
		}
		if zf.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		if err := extract(zf, target); err != nil {
			return err
		}
	}
	return nil
}

func extract(zf *zip.File, target string) error {
	in, err := zf.Open()
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, zf.Mode().Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
