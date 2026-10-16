package review

import (
	"container/heap"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/format/index"
	"github.com/go-git/go-git/v5/plumbing/object"
)

// emptyTree is git's tree with nothing in it: what a repository with no
// commits yet is compared with.
const emptyTree = "4b825dc642cb6eb9a060e54bf8d69288fbee4904"

// maxUntracked is the most of an untracked file shown: past it, the file
// is listed without its lines.
const maxUntracked = 1 << 20

// Diffing is how a diff is shown: IgnoreSpace leaves out what changed only
// in whitespace (an untracked file is all new, so it's shown whole).
type Diffing struct {
	IgnoreSpace bool
}

// open is the repository dir is in, or why there's no diff to review.
func open(dir string) (*repository, error) {
	r, err := openRepo(dir)
	switch {
	case errors.Is(err, errNotRepository):
		return nil, fmt.Errorf("%s isn't a git repository, so there's no diff to review", dir)
	case err != nil:
		return nil, fmt.Errorf("%s can't be read as a git repository, so there's no diff to review: %w", dir, err)
	}
	return r, nil
}

// WorkingTree is the diff of the folder dir as it is: everything not yet
// committed, staged or not, and untracked files (those git doesn't ignore)
// as added.
func WorkingTree(ctx context.Context, dir string, how ...Diffing) ([]File, error) {
	r, err := open(dir)
	if err != nil {
		return nil, err
	}
	head, err := r.head()
	if err != nil {
		return nil, err
	}
	return r.diffWorkTree(ctx, head, how)
}

// Against is the diff of the folder dir as it is from where it parted from
// ref (a branch, say): this branch's changes, committed or not, without
// those ref has gained since.
func Against(ctx context.Context, dir, ref string, how ...Diffing) ([]File, error) {
	r, err := open(dir)
	if err != nil {
		return nil, err
	}
	other, err := r.commit(ref)
	if err != nil {
		return nil, err
	}
	head, err := r.head()
	if err != nil || head == nil {
		return nil, fmt.Errorf("the folder has no commits to compare with %s", ref)
	}
	parted, err := mergeBase(other, head)
	if err != nil {
		return nil, fmt.Errorf("%s and the folder's HEAD have no history in common: %w", ref, err)
	}
	return r.diffWorkTree(ctx, parted, how)
}

// diffWorkTree is the work tree's diff from commit base (none: from
// nothing), untracked files with it, in path order.
func (r *repository) diffWorkTree(ctx context.Context, base *object.Commit, how []Diffing) ([]File, error) {
	head, err := r.head()
	if err != nil {
		return nil, err
	}
	o := newOverlay(r.git.Storer)
	now, untracked, err := r.workTree(o, head)
	if err != nil {
		return nil, err
	}
	from, err := treeOf(base)
	if err != nil {
		return nil, err
	}
	files, err := diffTrees(ctx, from, now, how, o.large)
	if err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(r.top)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	for _, p := range untracked {
		// As git, only files and links: not a FIFO, a socket, a device.
		if info, err := root.Lstat(filepath.FromSlash(p)); err == nil && !info.Mode().IsRegular() && info.Mode()&os.ModeSymlink == 0 {
			continue
		}
		files = append(files, untrackedFile(root, p))
	}
	sort.SliceStable(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files, nil
}

// Commit is the diff of what commit ref changed: a merge commit's, what it
// brought in to its first parent.
func Commit(ctx context.Context, dir, ref string, how ...Diffing) ([]File, error) {
	r, err := open(dir)
	if err != nil {
		return nil, err
	}
	c, err := r.commit(ref)
	if err != nil {
		return nil, err
	}
	var parent *object.Commit
	if c.NumParents() > 0 {
		if parent, err = c.Parent(0); err != nil {
			return nil, err
		}
	}
	return r.diffCommits(ctx, parent, c, how)
}

// Unpushed is the diff of the commits on HEAD that upstream hasn't: what
// they changed, from where the two parted, without anything uncommitted.
func Unpushed(ctx context.Context, dir, upstream string, how ...Diffing) ([]File, error) {
	r, err := open(dir)
	if err != nil {
		return nil, err
	}
	up, err := r.commit(upstream)
	if err != nil {
		return nil, err
	}
	head, err := r.head()
	if err != nil || head == nil {
		return nil, fmt.Errorf("the folder has no commits")
	}
	parted, err := mergeBase(up, head)
	if err != nil {
		return nil, fmt.Errorf("%s and the folder's HEAD have no history in common: %w", upstream, err)
	}
	return r.diffCommits(ctx, parted, head, how)
}

func (r *repository) diffCommits(ctx context.Context, from, to *object.Commit, how []Diffing) ([]File, error) {
	a, err := treeOf(from)
	if err != nil {
		return nil, err
	}
	b, err := treeOf(to)
	if err != nil {
		return nil, err
	}
	files, err := diffTrees(ctx, a, b, how, nil)
	if err != nil {
		return nil, err
	}
	sort.SliceStable(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files, nil
}

// untrackedFile is an untracked file, as git would show it added: a link
// as where it points, never what's there (it may point out of the folder,
// at a host file, or at /dev/zero); only a plain file's content is read,
// from in the folder (root).
func untrackedFile(root *os.Root, path string) File {
	f := untrackedOf(root, path)
	f.Fingerprint = fingerprint(f)
	return f
}

func untrackedOf(root *os.Root, path string) File {
	f := File{Path: path, Status: Added, Hunks: []Hunk{}}
	native := filepath.FromSlash(path)
	info, err := root.Lstat(native)
	if err != nil {
		f.Unreadable = err.Error()
		return f
	}
	// Its size and time stand for its content until it's read.
	f.stamp = fmt.Sprintf("%d %d", info.Size(), info.ModTime().UnixNano())
	if info.Mode()&os.ModeSymlink != 0 {
		target, err := root.Readlink(native)
		if err != nil {
			f.Unreadable = err.Error()
			return f
		}
		f.stamp = "link " + target
		f.Lines = 1
		f.Hunks = []Hunk{{OldStart: 0, NewStart: 1, Lines: []Line{{Kind: Addition, New: 1, Text: target}}}}
		return f
	}
	if info.Size() > maxUntracked {
		f.TooLarge = true
		return f
	}
	body, err := readIn(root, native, maxUntracked)
	if err != nil {
		f.Unreadable = err.Error()
		return f
	}
	sum := sha256.Sum256(body)
	f.stamp = hex.EncodeToString(sum[:])
	if isBinary(body) {
		f.Binary = true
		return f
	}
	if len(body) == 0 {
		return f
	}
	h := Hunk{OldStart: 0, NewStart: 1}
	for i, line := range splitLines(string(body)) {
		h.Lines = append(h.Lines, Line{Kind: Addition, New: i + 1, Text: line})
	}
	f.Hunks = []Hunk{h}
	f.Lines = len(h.Lines)
	return f
}

// Root is the top of the repository folder dir is in: what its paths are
// relative to. It's dir or a folder above it, whatever the repository's
// config says.
func Root(ctx context.Context, dir string) (string, error) {
	r, err := open(dir)
	if err != nil {
		return "", err
	}
	return r.top, nil
}

// StartOptions are the reviews the folder can be started on, as it is.
type StartOptions struct {
	// Uncommitted says there are changes not yet committed.
	Uncommitted bool `json:"uncommitted"`
	// DefaultBranch is the repository's main line to review this branch
	// against: "" when it can't be told, or the folder is on it.
	DefaultBranch string `json:"defaultBranch,omitempty"`
	// Upstream is the branch this one pushes to, when it has commits not
	// pushed there yet: Unpushed of them.
	Upstream string `json:"upstream,omitempty"`
	Unpushed int    `json:"unpushed,omitempty"`
}

// Options are the reviews the folder dir can be started on.
func Options(ctx context.Context, dir string) (StartOptions, error) {
	var o StartOptions
	r, err := open(dir)
	if err != nil {
		return o, err
	}
	files, err := WorkingTree(ctx, dir)
	if err != nil {
		return o, err
	}
	o.Uncommitted = len(files) > 0
	current := r.branch()
	if def := r.defaultBranch(); def != "" && def != current {
		o.DefaultBranch = def
	}
	if up, upstream := r.upstream(current); up != nil {
		head, err := r.head()
		if err != nil {
			return o, err
		}
		if ahead, _ := aheadBehind(head, up); ahead > 0 {
			o.Upstream, o.Unpushed = upstream, ahead
		}
	}
	return o, nil
}

// branch is the branch HEAD is on (one with no commits yet included), or
// "" when it's detached.
func (r *repository) branch() string {
	ref, err := r.git.Storer.Reference(plumbing.HEAD)
	if err != nil || ref.Type() != plumbing.SymbolicReference || !ref.Target().IsBranch() {
		return ""
	}
	return ref.Target().Short()
}

// defaultBranch is the repository's main line, as origin says (its HEAD),
// else main or master, whichever is there; "" when none is. The local
// branch's name when there is one, else the remote's.
func (r *repository) defaultBranch() string {
	exists := func(name plumbing.ReferenceName) bool {
		_, err := r.git.Reference(name, true)
		return err == nil
	}
	if ref, err := r.git.Storer.Reference("refs/remotes/origin/HEAD"); err == nil && ref.Type() == plumbing.SymbolicReference {
		remote := ref.Target().Short()
		if local := strings.TrimPrefix(remote, "origin/"); exists(plumbing.NewBranchReferenceName(local)) {
			return local
		}
		return remote
	}
	for _, name := range []string{"main", "master"} {
		if exists(plumbing.NewBranchReferenceName(name)) {
			return name
		}
	}
	return ""
}

// upstream is the commit branch pushes to, and its name; nil when it has
// none.
func (r *repository) upstream(branch string) (*object.Commit, string) {
	if branch == "" {
		return nil, ""
	}
	cfg, err := r.git.Config()
	if err != nil {
		return nil, ""
	}
	b, ok := cfg.Branches[branch]
	if !ok || b.Merge == "" {
		return nil, ""
	}
	name := plumbing.NewRemoteReferenceName(b.Remote, b.Merge.Short())
	short := b.Remote + "/" + b.Merge.Short()
	if b.Remote == "." {
		name, short = b.Merge, b.Merge.Short()
	}
	ref, err := r.git.Reference(name, true)
	if err != nil {
		return nil, ""
	}
	c, err := r.git.CommitObject(ref.Hash())
	if err != nil {
		return nil, ""
	}
	return c, short
}

// aheadBehind is how many commits a has that b hasn't, and b that a
// hasn't: git's way, walking back from both newest first until all that's
// left is history they share.
func aheadBehind(a, b *object.Commit) (ahead, behind int) {
	if a == nil || b == nil {
		return 0, 0
	}
	const fromA, fromB = 1, 2
	flags := map[plumbing.Hash]int{}
	q := &byTime{}
	mark := func(c *object.Commit, f int) {
		if flags[c.Hash]|f == flags[c.Hash] {
			return
		}
		flags[c.Hash] |= f
		heap.Push(q, c)
	}
	mark(a, fromA)
	mark(b, fromB)
	for q.Len() > 0 {
		stale := true
		for _, c := range *q {
			if flags[c.Hash] != fromA|fromB {
				stale = false
				break
			}
		}
		if stale {
			break
		}
		c := heap.Pop(q).(*object.Commit)
		f := flags[c.Hash]
		parents := c.Parents()
		parents.ForEach(func(p *object.Commit) error {
			mark(p, f)
			return nil
		})
		parents.Close()
	}
	for _, f := range flags {
		switch f {
		case fromA:
			ahead++
		case fromB:
			behind++
		}
	}
	return ahead, behind
}

// byTime is commits, newest first.
type byTime []*object.Commit

func (q byTime) Len() int           { return len(q) }
func (q byTime) Less(i, j int) bool { return q[i].Committer.When.After(q[j].Committer.When) }
func (q byTime) Swap(i, j int)      { q[i], q[j] = q[j], q[i] }
func (q *byTime) Push(x any)        { *q = append(*q, x.(*object.Commit)) }
func (q *byTime) Pop() any {
	old := *q
	c := old[len(old)-1]
	*q = old[:len(old)-1]
	return c
}

// FolderStatus is what the companion page's header shows of the folder's
// git status, as the infobar in the sandbox does.
type FolderStatus struct {
	Branch     string
	Staged     int
	Modified   int
	Untracked  int
	Conflicted int
	Ahead      int
	Behind     int
	Stashed    int
}

// StatusOf is the git status of the folder dir; an error when it's in no
// repository (or one that can't be read).
func StatusOf(ctx context.Context, dir string) (*FolderStatus, error) {
	r, err := openRepo(dir)
	if err != nil {
		return nil, err
	}
	wt, err := r.git.Worktree()
	if err != nil {
		return nil, err
	}
	wt.Excludes = append(wt.Excludes, r.ignored()...)
	status, err := wt.Status()
	if err != nil {
		return nil, err
	}
	s := &FolderStatus{Branch: r.branch()}
	head, err := r.head()
	if err != nil {
		return nil, err
	}
	if s.Branch == "" {
		s.Branch = "detached"
		if head != nil {
			s.Branch = "@" + head.Hash.String()[:7]
		}
	}
	idx, err := r.git.Storer.Index()
	if err != nil {
		return nil, err
	}
	for path, f := range status {
		switch {
		case f.Worktree == gogit.Untracked:
			s.Untracked++
			continue
		case f.Staging == gogit.UpdatedButUnmerged || f.Worktree == gogit.UpdatedButUnmerged:
			s.Conflicted++
			continue
		}
		if f.Staging != gogit.Unmodified {
			s.Staged++
		}
		if f.Worktree != gogit.Unmodified && r.changedOnDisk(path, idx) {
			s.Modified++
		}
	}
	if up, _ := r.upstream(r.branch()); up != nil {
		s.Ahead, s.Behind = aheadBehind(head, up)
	}
	s.Stashed = r.stashes()
	return s, nil
}

// changedOnDisk says file p differs from the index as git sees it: go-git
// compares the bytes, and a checkout's CRLFs (or a Git LFS file's content)
// are no change to git.
func (r *repository) changedOnDisk(p string, idx *index.Index) bool {
	e, err := idx.Entry(p)
	if err != nil {
		return true
	}
	root, err := os.OpenRoot(r.top)
	if err != nil {
		return true
	}
	defer root.Close()
	info, err := root.Lstat(filepath.FromSlash(p))
	if err != nil || !info.Mode().IsRegular() || info.Size() > maxFileBytes {
		return true
	}
	data, err := readIn(root, filepath.FromSlash(p), maxFileBytes)
	if err != nil {
		return true
	}
	was := &object.TreeEntry{Name: p, Mode: e.Mode, Hash: e.Hash}
	data = r.asCommitted(newOverlay(r.git.Storer), p, was, data)
	return plumbing.ComputeHash(plumbing.BlobObject, data) != e.Hash
}

// stashes is how many stashes there are: the lines of the stash's log.
func (r *repository) stashes() int {
	f, err := r.git.Storer.Reference("refs/stash")
	if err != nil || f == nil {
		return 0
	}
	n := 0
	for _, dir := range r.gitDirs {
		if data, err := readPlain(filepath.Join(dir, "logs", "refs", "stash"), 1<<20); err == nil {
			n = strings.Count(string(data), "\n")
		}
	}
	return n
}

// TextOf is path's lines as review rv has it (see NewTextOf).
func TextOf(ctx context.Context, dir string, rv Review, path string) ([]string, error) {
	text, err := NewTextOf(ctx, dir, rv, path)
	if err != nil {
		return nil, err
	}
	return splitLines(text), nil
}

// splitLines is text's lines, without the newline ending the last, nor
// the carriage returns ending a CRLF file's: the one place lines are made,
// so they read the same from the diff, the file and a comment.
func splitLines(text string) []string {
	lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	for i, l := range lines {
		lines[i] = strings.TrimSuffix(l, "\r")
	}
	return lines
}

// Moves are rv's comments on the new side whose code (as it read when
// they were made) is no longer at their lines but is elsewhere in the file:
// where to. A file that can't be read moves nothing: its comments stay, and
// the page marks any whose lines read differently.
func Moves(ctx context.Context, dir string, rv Review) []Move {
	var out []Move
	files := map[string][]string{}
	for _, t := range rv.Threads {
		if t.File == "" || t.Side == "old" || t.Code == "" {
			continue
		}
		lines, seen := files[t.File]
		if !seen {
			lines, _ = TextOf(ctx, dir, rv, t.File)
			files[t.File] = lines
		}
		if lines == nil {
			continue
		}
		end := t.EndLine
		if end < t.Line {
			end = t.Line
		}
		if t.Line >= 1 && end <= len(lines) && strings.Join(lines[t.Line-1:end], "\n") == t.Code {
			continue
		}
		if at, ok := Relocate(t.Code, lines, t.Line); ok && at != t.Line {
			out = append(out, Move{Thread: t.ID, Line: at})
		}
	}
	return out
}

// fingerprint stands for f's change: a digest of what it is and its stamp
// (the full ids of the blobs compared, and their modes; an untracked
// file's content), not its hunks, which change with how the diff is shown
// (see Diffing).
func fingerprint(f File) string {
	data, _ := json.Marshal(struct {
		Path, OldPath, Status, Stamp string
		Binary, TooLarge             bool
	}{f.Path, f.OldPath, f.Status, f.stamp, f.Binary, f.TooLarge})
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:12])
}
