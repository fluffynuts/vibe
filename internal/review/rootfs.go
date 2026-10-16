package review

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/go-git/go-billy/v5"
)

// rootFS is a git directory as go-git reads it: from an os.Root opened on
// the folder once it was checked (so nothing swapped in after is read), no
// path leading out of it (a link, ".."), nothing opened that would wait (a
// FIFO), and nothing written: the Review tab only reads.
type rootFS struct {
	root *os.Root
	name string
}

var errReadOnly = errors.New("the Review tab only reads the repository")

func newRootFS(root *os.Root) *rootFS { return &rootFS{root: root, name: root.Name()} }

// native is name as a path in the root: go-git's names are slash-separated,
// and may start with one.
func native(name string) string {
	name = strings.TrimLeft(path.Clean("/"+filepath.ToSlash(name)), "/")
	if name == "" {
		return "."
	}
	return filepath.FromSlash(name)
}

func (fs *rootFS) Open(name string) (billy.File, error) {
	return fs.OpenFile(name, os.O_RDONLY, 0)
}

func (fs *rootFS) OpenFile(name string, flag int, _ os.FileMode) (billy.File, error) {
	if flag&(os.O_WRONLY|os.O_RDWR|os.O_CREATE|os.O_TRUNC|os.O_APPEND) != 0 {
		return nil, &os.PathError{Op: "open", Path: name, Err: errReadOnly}
	}
	f, err := fs.root.OpenFile(native(name), os.O_RDONLY|nonBlocking, 0)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	if !info.Mode().IsRegular() && !info.IsDir() {
		f.Close()
		return nil, &os.PathError{Op: "open", Path: name, Err: fmt.Errorf("it isn't a plain file")}
	}
	return &rootFile{File: f, name: name}, nil
}

func (fs *rootFS) Create(name string) (billy.File, error) {
	return nil, &os.PathError{Op: "create", Path: name, Err: errReadOnly}
}

func (fs *rootFS) Stat(name string) (os.FileInfo, error)  { return fs.root.Stat(native(name)) }
func (fs *rootFS) Lstat(name string) (os.FileInfo, error) { return fs.root.Lstat(native(name)) }
func (fs *rootFS) Readlink(name string) (string, error)   { return fs.root.Readlink(native(name)) }
func (fs *rootFS) Join(elem ...string) string             { return path.Join(elem...) }
func (fs *rootFS) Root() string                           { return fs.name }

func (fs *rootFS) Rename(from, _ string) error {
	return &os.PathError{Op: "rename", Path: from, Err: errReadOnly}
}

func (fs *rootFS) Remove(name string) error {
	return &os.PathError{Op: "remove", Path: name, Err: errReadOnly}
}

func (fs *rootFS) TempFile(dir, _ string) (billy.File, error) {
	return nil, &os.PathError{Op: "create", Path: dir, Err: errReadOnly}
}

func (fs *rootFS) MkdirAll(name string, _ os.FileMode) error {
	if info, err := fs.root.Stat(native(name)); err == nil && info.IsDir() {
		return nil
	}
	return &os.PathError{Op: "mkdir", Path: name, Err: errReadOnly}
}

func (fs *rootFS) Symlink(_, link string) error {
	return &os.PathError{Op: "symlink", Path: link, Err: errReadOnly}
}

func (fs *rootFS) ReadDir(name string) ([]os.FileInfo, error) {
	d, err := fs.root.Open(native(name))
	if err != nil {
		return nil, err
	}
	defer d.Close()
	entries, err := d.ReadDir(-1)
	if err != nil {
		return nil, err
	}
	out := make([]os.FileInfo, 0, len(entries))
	for _, e := range entries {
		if info, err := e.Info(); err == nil {
			out = append(out, info)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name() < out[j].Name() })
	return out, nil
}

func (fs *rootFS) Chroot(name string) (billy.Filesystem, error) {
	sub, err := fs.root.OpenRoot(native(name))
	if err != nil {
		return nil, err
	}
	return newRootFS(sub), nil
}

// rootFile is a file go-git reads: never written, nor locked (nothing else
// here writes it).
type rootFile struct {
	*os.File
	name string
}

func (f *rootFile) Name() string { return f.name }
func (f *rootFile) Write([]byte) (int, error) {
	return 0, &os.PathError{Op: "write", Path: f.name, Err: errReadOnly}
}
func (f *rootFile) Truncate(int64) error {
	return &os.PathError{Op: "truncate", Path: f.name, Err: errReadOnly}
}
func (f *rootFile) Lock() error   { return nil }
func (f *rootFile) Unlock() error { return nil }
