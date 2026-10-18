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
	"os/exec"
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
	formatcfg "github.com/go-git/go-git/v5/plumbing/format/config"
	"github.com/go-git/go-git/v5/plumbing/format/gitattributes"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/storer"
	"github.com/go-git/go-git/v5/storage/filesystem"
	"github.com/go-git/go-git/v5/storage/filesystem/dotgit"
	"github.com/go-git/go-git/v5/storage/memory"

	"vibe/internal/filekind"
	"vibe/internal/pathresolve"
)

// The sandbox's folder is the sandbox's to write, .git and all: the
// repository is read here with go-git, which runs nothing a repository's
// config names (no hooks, filters, file system monitor, signature checks,
// fetches), rather than with git, which would. go-git only reads the git
// directory, through rootFS (pinned to the folder checked, read-only,
// nothing that would wait); the work tree is read here (see scan), through
// an os.Root: no link leads out of either. A git directory the sandbox
// could have pointed elsewhere is refused (see openRepo). And all of it is
// done in a helper process (see isolated), so what can't be bounded here (a
// zlib bomb inflating in go-git) costs that process, not vibe.

// repository is the repository a folder is in, opened safely.
type repository struct {
	git *gogit.Repository
	// top is the top of its work tree: the folder, or a folder above it;
	// work, the same, opened.
	top  string
	work *os.Root
	// gitDir is its git directory, opened; common, the one it shares, if
	// any (else gitDir).
	gitDir, common *os.Root
	// trusted says the git directory is the host's, not the sandbox's: it's
	// above the sandbox's folder.
	trusted bool
	// autocrlf is core.autocrlf; fileMode, core.fileMode (true unless set
	// false): whether an executable bit's change is a change.
	autocrlf, fileMode bool
	// attributes are .gitattributes files read so far, by folder.
	attributes map[string][]gitattributes.MatchAttribute
}

// errNotRepository is a folder in no repository.
var errNotRepository = errors.New("not a git repository")

// openRepo opens the repository the folder dir is in. A .git the sandbox
// can write (one in dir) may only lead to a git directory in dir, with no
// common directory or object alternates elsewhere: else it could make the
// host read another repository. One above dir is the host's, as is all it
// leads to. Each git directory is opened once checked, and checked to be
// the one opened: one swapped in after isn't read.
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
	writable := pathresolve.Within(ws, filepath.Join(top, ".git"))
	refuse := func(why string) (*repository, error) {
		return nil, fmt.Errorf("its .git %s: the Review tab won't read a repository the sandbox could have pointed anywhere (a linked worktree? start vibe in the main checkout)", why)
	}
	gitDir := filepath.Join(top, ".git")
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
		if writable && !pathresolve.Within(ws, gitDir) {
			return refuse("leads to a git directory outside the folder, " + gitDir)
		}
	}
	r := &repository{top: top, trusted: !writable, attributes: map[string][]gitattributes.MatchAttribute{}}
	// What the sandbox can write is opened from within its folder.
	within := ""
	if writable {
		within = ws
	}
	if r.gitDir, err = openChecked(gitDir, within); err != nil {
		return nil, err
	}
	r.common = r.gitDir
	var dot billy.Filesystem = newRootFS(r.gitDir)
	if data, err := readIn(r.gitDir, "commondir", 4096); err == nil {
		common := strings.TrimSpace(string(data))
		if !filepath.IsAbs(common) {
			common = filepath.Join(gitDir, common)
		}
		if common, err = filepath.EvalSymlinks(common); err != nil {
			r.close()
			return nil, err
		}
		if writable && !pathresolve.Within(ws, common) {
			r.close()
			return refuse("shares a git directory outside the folder, " + common)
		}
		if r.common, err = openChecked(common, within); err != nil {
			r.close()
			return nil, err
		}
		dot = dotgit.NewRepositoryFilesystem(dot, newRootFS(r.common))
	}
	// Borrowed objects: the host's own repository may (a clone --shared, say),
	// read where they are; the sandbox's may not.
	alternates := billy.Filesystem(newRootFS(r.common))
	if _, err := r.common.Lstat(filepath.Join("objects", "info", "alternates")); err == nil {
		if writable {
			r.close()
			return refuse("borrows objects from other repositories (objects/info/alternates)")
		}
		alternates = hostFS{osfs.New("/")}
	}
	st := filesystem.NewStorageWithOptions(dot, cache.NewObjectLRUDefault(), filesystem.Options{LargeObjectThreshold: maxFileBytes, AlternatesFS: alternates})
	if r.git, err = gogit.Open(st, nil); err != nil {
		r.close()
		switch {
		case errors.Is(err, gogit.ErrRepositoryNotExists):
			return nil, errNotRepository
		case strings.Contains(strings.ToLower(err.Error()), "worktreeconfig"):
			return nil, fmt.Errorf("it uses per-worktree config (extensions.worktreeConfig, which git sparse-checkout turns on), which vibe can't read yet: the Review tab and the header's git status don't work in this repository")
		}
		return nil, err
	}
	if r.work, err = os.OpenRoot(top); err != nil {
		r.close()
		return nil, err
	}
	crlf := r.setting("core", "autocrlf")
	r.autocrlf = crlf == "true" || crlf == "input"
	r.fileMode = r.setting("core", "filemode") != "false"
	return r, nil
}

// hostFS reads the host's own folders by absolute path, as the host's
// repository names them for objects it borrows (objects/info/alternates):
// go-git makes such a path relative to its file system's root, which can't
// be done for a Windows path (C:\...) from "/".
type hostFS struct{ billy.Filesystem }

func (h hostFS) Stat(name string) (os.FileInfo, error) { return os.Stat(name) }

func (h hostFS) Chroot(name string) (billy.Filesystem, error) {
	if !filepath.IsAbs(name) {
		return h.Filesystem.Chroot(name)
	}
	return osfs.New(name), nil
}

// openChecked opens the folder at path, which must be a folder (not a link
// to one), and be the one opened: else something was swapped in meanwhile.
// One checked to be in the folder within ("" for none) is opened from
// within, through nothing leading out of it: a folder on its way swapped
// for a link since the check isn't followed.
func openChecked(path, within string) (*os.Root, error) {
	lstat, openRoot := os.Lstat, os.OpenRoot
	if within != "" {
		folder, err := os.OpenRoot(within)
		if err != nil {
			return nil, err
		}
		defer folder.Close()
		rel, err := filepath.Rel(within, path)
		if err != nil {
			return nil, err
		}
		lstat = func(string) (os.FileInfo, error) { return folder.Lstat(rel) }
		openRoot = func(string) (*os.Root, error) { return folder.OpenRoot(rel) }
	}
	before, err := lstat(path)
	if err != nil {
		return nil, err
	}
	if !before.IsDir() {
		return nil, fmt.Errorf("%s isn't a folder", path)
	}
	root, err := openRoot(path)
	if err != nil {
		return nil, err
	}
	opened, err := root.Stat(".")
	if err != nil || !os.SameFile(before, opened) {
		root.Close()
		return nil, fmt.Errorf("%s changed while it was being opened", path)
	}
	return root, nil
}

func (r *repository) close() {
	for _, root := range []*os.Root{r.work, r.common, r.gitDir} {
		if root != nil {
			root.Close()
		}
	}
}

// setting is a git setting, as the repository's config, else the user's,
// else the system's, says.
func (r *repository) setting(section, key string) string {
	for _, scope := range []config.Scope{config.LocalScope, config.GlobalScope} {
		cfg, err := r.git.ConfigScoped(scope)
		if err != nil {
			continue
		}
		if v := cfg.Raw.Section(section).Option(key); v != "" {
			return strings.ToLower(v)
		}
	}
	for _, file := range systemConfigs() {
		if v := hostSetting(file, section, key); v != "" {
			return strings.ToLower(v)
		}
	}
	return ""
}

// hostSetting is a setting in a config file of the host's own (not the
// sandbox's: a link is followed, as git would), "" if it isn't there.
func hostSetting(file, section, key string) string {
	f, err := os.Open(file)
	if err != nil {
		return ""
	}
	defer f.Close()
	if info, err := f.Stat(); err != nil || !info.Mode().IsRegular() {
		return ""
	}
	cfg := formatcfg.New()
	if err := formatcfg.NewDecoder(io.LimitReader(f, 1<<20)).Decode(cfg); err != nil {
		return ""
	}
	return cfg.Section(section).Option(key)
}

// systemConfigs are where git keeps its system config, as git finds it:
// $GIT_CONFIG_SYSTEM, else beside git itself (Git for Windows keeps it in
// its own etc/; a git installed by Homebrew, in the prefix it's linked
// into, and also its keg's), else /etc/gitconfig. go-git looks in /etc
// alone, so on Windows it would miss core.autocrlf, which Git for Windows
// sets there.
func systemConfigs() []string {
	if os.Getenv("GIT_CONFIG_NOSYSTEM") != "" {
		return nil
	}
	if file := os.Getenv("GIT_CONFIG_SYSTEM"); file != "" {
		return []string{file}
	}
	var out []string
	if git, err := exec.LookPath("git"); err == nil {
		beside := func(git string) {
			bin := filepath.Dir(git)
			out = append(out, filepath.Join(bin, "..", "etc", "gitconfig"), filepath.Join(bin, "..", "..", "etc", "gitconfig"))
		}
		beside(git)
		if real, err := filepath.EvalSymlinks(git); err == nil && real != git {
			beside(real)
		}
	}
	if data := os.Getenv("ProgramData"); data != "" {
		out = append(out, filepath.Join(data, "Git", "config"))
	}
	return append(out, "/etc/gitconfig")
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

// workTree is the folder's work tree as a tree (HEAD's, with what differs
// from it made in o: what's staged, what's changed on disk, what was taken
// out of the index), and its untracked files, which git doesn't list in a
// diff but the tab shows as added.
func (r *repository) workTree(o *overlay, head *object.Commit) (*object.Tree, []string, error) {
	base, err := treeOf(head)
	if err != nil {
		return nil, nil, err
	}
	sc, err := r.scan(o, base, true)
	if err != nil {
		return nil, nil, err
	}
	h, err := editTree(o, base, sc.edits)
	if err != nil {
		return nil, nil, err
	}
	if h == emptyTreeHash {
		return &object.Tree{}, sc.untracked, nil
	}
	t, err := object.GetTree(o, h)
	if err != nil {
		return nil, nil, err
	}
	return t, sc.untracked, nil
}

// asCommitted is a file's content as git would commit it, without running
// anything: CRLFs as LFs as git would make them (see lfInRepository); and
// a file kept in Git LFS (its committed blob a pointer, was) as the
// pointer to its content, as git-lfs would make it.
func (r *repository) asCommitted(o *overlay, p string, was plumbing.Hash, data []byte) ([]byte, error) {
	if !was.IsZero() && !isPointer(data) {
		// A pointer is small: a blob that isn't isn't read to find out.
		if size, err := o.EncodedObjectSize(was); err == nil && size < 1024 {
			if old, err := o.EncodedObject(plumbing.BlobObject, was); err == nil {
				if rd, err := old.Reader(); err == nil {
					committed, _ := io.ReadAll(io.LimitReader(rd, 1024))
					rd.Close()
					if isPointer(committed) {
						sum := sha256.Sum256(data)
						return []byte("version https://git-lfs.github.com/spec/v1\noid sha256:" + hex.EncodeToString(sum[:]) + "\nsize " + strconv.Itoa(len(data)) + "\n"), nil
					}
				}
			}
		}
	}
	lf, err := r.lfInRepository(p, data)
	if err != nil {
		return nil, err
	}
	if lf {
		return bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n")), nil
	}
	return data, nil
}

// lfInRepository says file p's line ends are LFs in the repository, CRLFs
// made into LFs as git would commit it: by .gitattributes (text, eol; not
// -text, binary; text=auto only for what isn't binary), else by
// core.autocrlf, for what isn't binary.
func (r *repository) lfInRepository(p string, data []byte) (bool, error) {
	patterns, err := r.attributesFor(p)
	if err != nil {
		return false, err
	}
	attrs, _ := gitattributes.NewMatcher(patterns).Match(strings.Split(p, "/"), []string{"text", "eol", "binary"})
	text, eol, binary := attrs["text"], attrs["eol"], attrs["binary"]
	switch {
	case binary != nil && binary.IsSet(), text != nil && text.IsUnset():
		return false, nil
	case text != nil && text.IsValueSet() && text.Value() == "auto":
		return !filekind.IsBinary(data), nil
	case text != nil && text.IsSet():
		return true, nil
	case eol != nil && eol.IsValueSet():
		return !filekind.IsBinary(data), nil
	}
	return r.autocrlf && !filekind.IsBinary(data), nil
}

// attributesFor are the .gitattributes patterns bearing on file p: the git
// directory's info/attributes, then each folder's from the top down to
// p's, each file read once, from in the work tree (no link out, no FIFO).
func (r *repository) attributesFor(p string) ([]gitattributes.MatchAttribute, error) {
	var out []gitattributes.MatchAttribute
	if data, err := readIn(r.gitDir, filepath.Join("info", "attributes"), 1<<20); err == nil {
		ps, _ := gitattributes.ReadAttributes(bytes.NewReader(data), nil, true)
		out = append(out, ps...)
	}
	parts := strings.Split(p, "/")
	for depth := 0; depth < len(parts); depth++ {
		folder := strings.Join(parts[:depth], "/")
		ps, seen := r.attributes[folder]
		if !seen {
			data, err := readIn(r.work, filepath.FromSlash(path.Join(folder, ".gitattributes")), 1<<20)
			switch {
			case err == nil:
				ps, _ = gitattributes.ReadAttributes(bytes.NewReader(data), parts[:depth], depth == 0)
			case !errors.Is(err, os.ErrNotExist):
				return nil, fmt.Errorf("%s can't be read: %w", path.Join(folder, ".gitattributes"), err)
			}
			r.attributes[folder] = ps
		}
		out = append(out, ps...)
	}
	return out, nil
}

func isPointer(data []byte) bool {
	return bytes.HasPrefix(data, []byte("version https://git-lfs.github.com/spec/"))
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
	case filekind.IsBinary(old.data) || filekind.IsBinary(now.data):
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
