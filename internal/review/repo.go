package review

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/go-git/go-billy/v5"
	"github.com/go-git/go-billy/v5/osfs"
	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/cache"
	"github.com/go-git/go-git/v5/plumbing/filemode"
	"github.com/go-git/go-git/v5/plumbing/format/gitignore"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/storer"
	"github.com/go-git/go-git/v5/storage/filesystem"
	"github.com/go-git/go-git/v5/storage/filesystem/dotgit"
	"github.com/go-git/go-git/v5/storage/memory"
)

// The sandbox's folder is the sandbox's to write, .git and all: the
// repository is read here with go-git, which runs nothing a repository's
// config names (no hooks, filters, file system monitor, signature checks,
// fetches), rather than with git, which would. Its files and git directory
// are read through file systems bound to their folders, so no link leads
// out of them; and a git directory the sandbox could have pointed
// elsewhere is refused (see openRepo).

// repository is the repository a folder is in, opened safely.
type repository struct {
	git *gogit.Repository
	// top is the top of its work tree: the folder, or a folder above it.
	top string
	// autocrlf is core.autocrlf: text files' CRLFs are LFs in the
	// repository, as git would make them.
	autocrlf bool
	// gitDirs are its git directory, and the one it shares, if any.
	gitDirs []string
}

// errNotRepository is a folder in no repository.
var errNotRepository = errors.New("not a git repository")

// openRepo opens the repository the folder dir is in. A .git the sandbox
// can write (one in dir) may only lead to a git directory in dir, with no
// common directory or object alternates elsewhere: else it could make the
// host read another repository. One above dir is the host's, as is all it
// leads to.
func openRepo(dir string) (*repository, error) {
	ws, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return nil, err
	}
	top := ws
	var dotGit os.FileInfo
	for {
		if dotGit, err = os.Lstat(filepath.Join(top, ".git")); err == nil {
			break
		}
		parent := filepath.Dir(top)
		if parent == top {
			return nil, errNotRepository
		}
		top = parent
	}
	writable := top == ws
	gitDir := filepath.Join(top, ".git")
	refuse := func(why string) (*repository, error) {
		return nil, fmt.Errorf("its .git %s: the Review tab won't read a repository the sandbox could have pointed anywhere (a linked worktree? start vibe in the main checkout)", why)
	}
	switch {
	case dotGit.Mode()&os.ModeSymlink != 0:
		if writable {
			return refuse("is a link")
		}
		if gitDir, err = filepath.EvalSymlinks(gitDir); err != nil {
			return nil, err
		}
	case dotGit.Mode().IsRegular():
		data, err := readPlain(gitDir, 4096)
		if err != nil {
			return nil, err
		}
		target, ok := strings.CutPrefix(strings.TrimSpace(string(data)), "gitdir:")
		if !ok {
			return nil, fmt.Errorf("its .git file doesn't say where the git directory is")
		}
		target = strings.TrimSpace(target)
		if !filepath.IsAbs(target) {
			target = filepath.Join(top, target)
		}
		if gitDir, err = filepath.EvalSymlinks(target); err != nil {
			return nil, err
		}
		if writable && !inFolderTree(gitDir, ws) {
			return refuse("leads to a git directory outside the folder, " + gitDir)
		}
	}
	var dot billy.Filesystem = osfs.New(gitDir, osfs.WithBoundOS())
	gitDirs := []string{gitDir}
	if data, err := readPlain(filepath.Join(gitDir, "commondir"), 4096); err == nil {
		common := strings.TrimSpace(string(data))
		if !filepath.IsAbs(common) {
			common = filepath.Join(gitDir, common)
		}
		if common, err = filepath.EvalSymlinks(common); err != nil {
			return nil, err
		}
		if writable && !inFolderTree(common, ws) {
			return refuse("shares a git directory outside the folder, " + common)
		}
		dot = dotgit.NewRepositoryFilesystem(dot, osfs.New(common, osfs.WithBoundOS()))
		gitDirs = append(gitDirs, common)
	}
	if writable {
		if _, err := os.Lstat(filepath.Join(gitDir, "objects", "info", "alternates")); err == nil {
			return refuse("borrows objects from other repositories (objects/info/alternates)")
		}
	}
	st := filesystem.NewStorage(dot, cache.NewObjectLRUDefault())
	r, err := gogit.Open(st, osfs.New(top, osfs.WithBoundOS()))
	if err != nil {
		if errors.Is(err, gogit.ErrRepositoryNotExists) {
			return nil, errNotRepository
		}
		return nil, err
	}
	repo := &repository{git: r, top: top, gitDirs: gitDirs}
	repo.autocrlf = repo.setting("core", "autocrlf") == "true" || repo.setting("core", "autocrlf") == "input"
	return repo, nil
}

// inFolderTree says path is folder or in it.
func inFolderTree(path, folder string) bool {
	rel, err := filepath.Rel(folder, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// setting is a git setting, as the repository's config, else the user's,
// else the system's, says.
func (r *repository) setting(section, key string) string {
	for _, scope := range []config.Scope{config.LocalScope, config.GlobalScope, config.SystemScope} {
		cfg, err := r.git.ConfigScoped(scope)
		if err != nil {
			continue
		}
		if v := cfg.Raw.Section(section).Option(key); v != "" {
			return strings.ToLower(v)
		}
	}
	return ""
}

// commit is the commit ref names: a branch, a tag, a hash, HEAD~2.
func (r *repository) commit(ref string) (*object.Commit, error) {
	if strings.HasPrefix(ref, "-") {
		return nil, fmt.Errorf("%q isn't a branch or commit", ref)
	}
	h, err := r.git.ResolveRevision(plumbing.Revision(ref))
	if err != nil {
		return nil, fmt.Errorf("there's no branch or commit %q", ref)
	}
	c, err := r.git.CommitObject(*h)
	if err != nil {
		return nil, fmt.Errorf("%q isn't a commit", ref)
	}
	return c, nil
}

// head is the commit HEAD is at; nil in a repository with none yet.
func (r *repository) head() (*object.Commit, error) {
	ref, err := r.git.Head()
	if errors.Is(err, plumbing.ErrReferenceNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return r.git.CommitObject(ref.Hash())
}

// mergeBase is where a and b parted.
func mergeBase(a, b *object.Commit) (*object.Commit, error) {
	bases, err := a.MergeBase(b)
	if err != nil {
		return nil, err
	}
	if len(bases) == 0 {
		return nil, fmt.Errorf("they have no history in common")
	}
	return bases[0], nil
}

// treeOf is c's tree; an empty one for no commit.
func treeOf(c *object.Commit) (*object.Tree, error) {
	if c == nil {
		return &object.Tree{}, nil
	}
	return c.Tree()
}

// overlay keeps objects made here (the work tree's blobs and trees) apart
// from the repository's, which it reads through: nothing is written to the
// repository.
type overlay struct {
	*memory.ObjectStorage
	base storer.EncodedObjectStorer
	// large are the blobs standing for files too large to read.
	large map[plumbing.Hash]bool
}

func newOverlay(base storer.EncodedObjectStorer) *overlay {
	mem := memory.NewStorage()
	return &overlay{ObjectStorage: &mem.ObjectStorage, base: base, large: map[plumbing.Hash]bool{}}
}

func (o *overlay) EncodedObject(t plumbing.ObjectType, h plumbing.Hash) (plumbing.EncodedObject, error) {
	if obj, err := o.ObjectStorage.EncodedObject(t, h); err == nil {
		return obj, nil
	}
	return o.base.EncodedObject(t, h)
}

func (o *overlay) HasEncodedObject(h plumbing.Hash) error {
	if o.ObjectStorage.HasEncodedObject(h) == nil {
		return nil
	}
	return o.base.HasEncodedObject(h)
}

func (o *overlay) EncodedObjectSize(h plumbing.Hash) (int64, error) {
	if n, err := o.ObjectStorage.EncodedObjectSize(h); err == nil {
		return n, nil
	}
	return o.base.EncodedObjectSize(h)
}

func (o *overlay) blob(data []byte) (plumbing.Hash, error) {
	obj := o.NewEncodedObject()
	obj.SetType(plumbing.BlobObject)
	w, err := obj.Writer()
	if err != nil {
		return plumbing.ZeroHash, err
	}
	if _, err := w.Write(data); err != nil {
		return plumbing.ZeroHash, err
	}
	if err := w.Close(); err != nil {
		return plumbing.ZeroHash, err
	}
	return o.SetEncodedObject(obj)
}

// edit is a change to a tree's entry: a blob, with its mode; or, gone,
// the entry taken out.
type edit struct {
	hash plumbing.Hash
	mode filemode.FileMode
	gone bool
}

// editTree is base with edits (by slash-separated path) made, the trees
// it takes made in o.
func editTree(o *overlay, base *object.Tree, edits map[string]edit) (plumbing.Hash, error) {
	entries := map[string]object.TreeEntry{}
	if base != nil {
		for _, e := range base.Entries {
			entries[e.Name] = e
		}
	}
	below := map[string]map[string]edit{}
	for p, e := range edits {
		name, rest, deeper := strings.Cut(p, "/")
		if !deeper {
			if e.gone {
				delete(entries, name)
			} else {
				entries[name] = object.TreeEntry{Name: name, Mode: e.mode, Hash: e.hash}
			}
			continue
		}
		if below[name] == nil {
			below[name] = map[string]edit{}
		}
		below[name][rest] = e
	}
	for name, sub := range below {
		var subtree *object.Tree
		if e, ok := entries[name]; ok && e.Mode == filemode.Dir {
			t, err := object.GetTree(o, e.Hash)
			if err != nil {
				return plumbing.ZeroHash, err
			}
			subtree = t
		}
		h, err := editTree(o, subtree, sub)
		if err != nil {
			return plumbing.ZeroHash, err
		}
		if h == emptyTreeHash {
			delete(entries, name)
		} else {
			entries[name] = object.TreeEntry{Name: name, Mode: filemode.Dir, Hash: h}
		}
	}
	t := &object.Tree{}
	for _, e := range entries {
		t.Entries = append(t.Entries, e)
	}
	// git's order: a folder sorts as its name with a slash after it.
	key := func(e object.TreeEntry) string {
		if e.Mode == filemode.Dir {
			return e.Name + "/"
		}
		return e.Name
	}
	sort.Slice(t.Entries, func(i, j int) bool { return key(t.Entries[i]) < key(t.Entries[j]) })
	if len(t.Entries) == 0 {
		return emptyTreeHash, nil
	}
	obj := o.NewEncodedObject()
	if err := t.Encode(obj); err != nil {
		return plumbing.ZeroHash, err
	}
	return o.SetEncodedObject(obj)
}

var emptyTreeHash = plumbing.NewHash(emptyTree)

// workTree is the folder's work tree as a tree (HEAD's, with what has
// changed since made in o), and its untracked files, which git doesn't
// list in a diff but the tab shows as added.
func (r *repository) workTree(o *overlay, head *object.Commit) (*object.Tree, []string, error) {
	wt, err := r.git.Worktree()
	if err != nil {
		return nil, nil, err
	}
	wt.Excludes = append(wt.Excludes, r.ignored()...)
	status, err := wt.Status()
	if err != nil {
		return nil, nil, err
	}
	base, err := treeOf(head)
	if err != nil {
		return nil, nil, err
	}
	root, err := os.OpenRoot(r.top)
	if err != nil {
		return nil, nil, err
	}
	defer root.Close()
	edits := map[string]edit{}
	var untracked []string
	for p, s := range status {
		switch {
		case s.Worktree == gogit.Untracked:
			untracked = append(untracked, p)
			continue
		case s.Staging == gogit.Unmodified && s.Worktree == gogit.Unmodified:
			continue
		}
		e, ok, err := r.onDisk(o, root, base, p)
		if err != nil {
			return nil, nil, err
		}
		if ok {
			edits[p] = e
		}
	}
	h, err := editTree(o, base, edits)
	if err != nil {
		return nil, nil, err
	}
	if h == emptyTreeHash {
		sort.Strings(untracked)
		return &object.Tree{}, untracked, nil
	}
	t, err := object.GetTree(o, h)
	if err != nil {
		return nil, nil, err
	}
	sort.Strings(untracked)
	return t, untracked, nil
}

// ignored are the user's own and the system's ignore patterns, which
// go-git's status doesn't read by itself.
func (r *repository) ignored() []gitignore.Pattern {
	var out []gitignore.Pattern
	root := osfs.New("/")
	if ps, err := gitignore.LoadGlobalPatterns(root); err == nil {
		out = append(out, ps...)
	}
	if ps, err := gitignore.LoadSystemPatterns(root); err == nil {
		out = append(out, ps...)
	}
	return out
}

// onDisk is the edit making path in base what it is on disk: a file's
// content, a link's target, or gone; ok false for what a tree doesn't hold
// (a folder: a submodule, say).
func (r *repository) onDisk(o *overlay, root *os.Root, base *object.Tree, p string) (edit, bool, error) {
	native := filepath.FromSlash(p)
	info, err := root.Lstat(native)
	if errors.Is(err, os.ErrNotExist) {
		return edit{gone: true}, true, nil
	}
	if err != nil {
		return edit{}, false, err
	}
	var was *object.TreeEntry
	if base != nil {
		if e, err := base.FindEntry(p); err == nil {
			was = e
		}
	}
	var data []byte
	mode := filemode.Regular
	switch {
	case info.Mode()&os.ModeSymlink != 0:
		target, err := root.Readlink(native)
		if err != nil {
			return edit{}, false, err
		}
		data, mode = []byte(filepath.ToSlash(target)), filemode.Symlink
	case info.Mode().IsRegular():
		if info.Size() > maxFileBytes {
			// Too large to read: a blob standing for it, so the tab lists it.
			h, err := o.blob([]byte(fmt.Sprintf("%d bytes, %d", info.Size(), info.ModTime().UnixNano())))
			if err != nil {
				return edit{}, false, err
			}
			o.large[h] = true
			return edit{hash: h, mode: mode}, true, nil
		}
		if data, err = readIn(root, native, maxFileBytes); err != nil {
			return edit{}, false, err
		}
		if info.Mode().Perm()&0o111 != 0 {
			mode = filemode.Executable
		}
		// Windows has no executable bit: the mode the repository had stands.
		if was != nil && os.PathSeparator == '\\' && was.Mode == filemode.Executable {
			mode = filemode.Executable
		}
		data = r.asCommitted(o, was, data)
	default:
		return edit{}, false, nil
	}
	h, err := o.blob(data)
	if err != nil {
		return edit{}, false, err
	}
	return edit{hash: h, mode: mode}, true, nil
}

// asCommitted is a file's content as git would commit it, without running
// anything: CRLFs as LFs with core.autocrlf; and a file kept in Git LFS
// (its committed blob a pointer) as the pointer to its content, as git-lfs
// would make it.
func (r *repository) asCommitted(o *overlay, was *object.TreeEntry, data []byte) []byte {
	if was != nil && !isPointer(data) {
		if old, err := o.EncodedObject(plumbing.BlobObject, was.Hash); err == nil && old.Size() < 1024 {
			if rd, err := old.Reader(); err == nil {
				committed, _ := io.ReadAll(rd)
				rd.Close()
				if isPointer(committed) {
					sum := sha256.Sum256(data)
					return []byte("version https://git-lfs.github.com/spec/v1\noid sha256:" + hex.EncodeToString(sum[:]) + "\nsize " + strconv.Itoa(len(data)) + "\n")
				}
			}
		}
	}
	if r.autocrlf && !isBinary(data) {
		return bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n"))
	}
	return data
}

func isPointer(data []byte) bool {
	return bytes.HasPrefix(data, []byte("version https://git-lfs.github.com/spec/"))
}

// isBinary is git's test: a NUL early on.
func isBinary(data []byte) bool {
	return bytes.IndexByte(data[:min(len(data), 8000)], 0) >= 0
}

// readIn reads the plain file name in root, up to limit bytes: nothing
// outside root, through a link or "..", and not a FIFO.
func readIn(root *os.Root, name string, limit int64) ([]byte, error) {
	f, err := root.OpenFile(name, os.O_RDONLY|nonBlocking, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("it isn't a plain file")
	}
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("it's too large (over %d bytes)", limit)
	}
	return data, nil
}

// diffTrees is the change from tree a to tree b, as the tab's files;
// large, the blobs standing for files too large to read.
func diffTrees(ctx context.Context, a, b *object.Tree, how []Diffing, large map[plumbing.Hash]bool) ([]File, error) {
	changes, err := object.DiffTreeWithOptions(ctx, a, b, &object.DiffTreeOptions{DetectRenames: true, RenameScore: 50, RenameLimit: 1000, OnlyExactRenames: false})
	if err != nil {
		return nil, err
	}
	ignoreSpace := false
	for _, h := range how {
		ignoreSpace = ignoreSpace || h.IgnoreSpace
	}
	var files []File
	for _, c := range changes {
		if c.From.TreeEntry.Mode == filemode.Submodule || c.To.TreeEntry.Mode == filemode.Submodule {
			continue
		}
		f, keep, err := fileOfChange(c, ignoreSpace, large)
		if err != nil {
			return nil, err
		}
		if keep {
			files = append(files, f)
		}
	}
	return files, nil
}

// fileOfChange is change c as a file of the diff; keep false for one with
// nothing to show (only whitespace changed, while that's left out).
func fileOfChange(c *object.Change, ignoreSpace bool, large map[plumbing.Hash]bool) (File, bool, error) {
	from, to := c.From, c.To
	f := File{Path: to.Name, OldPath: from.Name, Status: Modified}
	switch {
	case from.Name == "":
		f.Status, f.Path, f.OldPath = Added, to.Name, ""
	case to.Name == "":
		f.Status, f.Path, f.OldPath = Deleted, from.Name, from.Name
	case from.Name != to.Name:
		f.Status = Renamed
	}
	f.stamp = fmt.Sprintf("%s %o..%s %o\n", from.TreeEntry.Hash, from.TreeEntry.Mode, to.TreeEntry.Hash, to.TreeEntry.Mode)
	old, err := contentOf(c, true, large)
	if err != nil {
		return f, false, err
	}
	now, err := contentOf(c, false, large)
	if err != nil {
		return f, false, err
	}
	switch {
	case old.tooLarge || now.tooLarge:
		f.TooLarge = true
	case isBinary(old.data) || isBinary(now.data):
		f.Binary = true
	default:
		f.Hunks = hunksOf(old.data, now.data, ignoreSpace)
		if f.Status == Deleted {
			f.Lines = len(splitLines(string(old.data)))
		} else if len(now.data) > 0 {
			f.Lines = len(splitLines(string(now.data)))
		}
	}
	if len(f.Hunks) == 0 && ignoreSpace && f.Status == Modified && !f.Binary && !f.TooLarge && from.TreeEntry.Mode == to.TreeEntry.Mode {
		return f, false, nil
	}
	if f.Hunks == nil {
		f.Hunks = []Hunk{}
	}
	f.Fingerprint = fingerprint(f)
	return f, true, nil
}

type content struct {
	data     []byte
	tooLarge bool
}

// contentOf is one side of change c (old, else new): empty where there's
// none.
func contentOf(c *object.Change, old bool, large map[plumbing.Hash]bool) (content, error) {
	from, to, err := c.Files()
	if err != nil {
		return content{}, err
	}
	f := to
	if old {
		f = from
	}
	if f == nil {
		return content{}, nil
	}
	if f.Size > maxFileBytes || large[f.Hash] {
		return content{tooLarge: true}, nil
	}
	rd, err := f.Reader()
	if err != nil {
		return content{}, err
	}
	defer rd.Close()
	data, err := io.ReadAll(io.LimitReader(rd, maxFileBytes+1))
	if err != nil {
		return content{}, err
	}
	return content{data: data}, nil
}

// blobText is path's content at commit c, up to maxFileBytes.
func blobText(c *object.Commit, p string) (string, error) {
	f, err := c.File(path.Clean(p))
	if err != nil {
		return "", fmt.Errorf("%q isn't in commit %s", p, c.Hash.String()[:7])
	}
	if f.Size > maxFileBytes {
		return "", fmt.Errorf("%q is too large to read (%d bytes)", p, f.Size)
	}
	return f.Contents()
}
