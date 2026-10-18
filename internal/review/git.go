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

	"github.com/go-git/go-git/v5/plumbing"
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

// workingTree is the diff of the folder dir as it is: everything not yet
// committed, staged or not, and untracked files (those git doesn't ignore)
// as added.
func workingTree(ctx context.Context, dir string, how ...Diffing) ([]File, error) {
	r, err := open(dir)
	if err != nil {
		return nil, err
	}
	defer r.close()
	head, err := r.head()
	if err != nil {
		return nil, err
	}
	return r.diffWorkTree(ctx, head, how)
}

// against is the diff of the folder dir as it is from where it parted from
// ref (a branch, say): this branch's changes, committed or not, without
// those ref has gained since.
func against(ctx context.Context, dir, ref string, how ...Diffing) ([]File, error) {
	r, err := open(dir)
	if err != nil {
		return nil, err
	}
	defer r.close()
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
	for _, p := range untracked {
		files = append(files, untrackedFile(r.work, p))
	}
	sort.SliceStable(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files, nil
}

// commit is the diff of what commit ref changed: a merge commit's, what it
// brought in to its first parent.
func commit(ctx context.Context, dir, ref string, how ...Diffing) ([]File, error) {
	r, err := open(dir)
	if err != nil {
		return nil, err
	}
	defer r.close()
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

// unpushed is the diff of the commits on HEAD that upstream hasn't: what
// they changed, from where the two parted, without anything uncommitted.
func unpushed(ctx context.Context, dir, upstream string, how ...Diffing) ([]File, error) {
	r, err := open(dir)
	if err != nil {
		return nil, err
	}
	defer r.close()
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

// root is the top of the repository folder dir is in: what its paths are
// relative to. It's dir or a folder above it, whatever the repository's
// config says.
func root(ctx context.Context, dir string) (string, error) {
	r, err := open(dir)
	if err != nil {
		return "", err
	}
	defer r.close()
	return r.top, nil
}

// StartOptions are the reviews the folder can be started on, as it is.
type StartOptions struct {
	// Uncommitted says there are changes not yet committed.
	Uncommitted bool `json:"uncommitted"`
	// DefaultBranch is the repository's main line to review this branch
	// against: the local branch, else origin's; "" when it can't be told
	// (NoDefaultBranch says why), when it has no commits yet
	// (EmptyDefaultBranch names it), or when the folder is on it.
	DefaultBranch      string `json:"defaultBranch,omitempty"`
	NoDefaultBranch    string `json:"noDefaultBranch,omitempty"`
	EmptyDefaultBranch string `json:"emptyDefaultBranch,omitempty"`
	// Upstream is the branch this one pushes to, when it has commits not
	// pushed there yet: Unpushed of them.
	Upstream string `json:"upstream,omitempty"`
	Unpushed int    `json:"unpushed,omitempty"`
}

// optionsOf are the reviews the folder dir can be started on.
func optionsOf(ctx context.Context, dir string) (StartOptions, error) {
	var o StartOptions
	r, err := open(dir)
	if err != nil {
		return o, err
	}
	defer r.close()
	files, err := workingTree(ctx, dir)
	if err != nil {
		return o, err
	}
	o.Uncommitted = len(files) > 0
	current := r.branch()
	if def := r.defaultBranch(); def.Name == "" {
		o.NoDefaultBranch = def.Why
	} else if def.Name != current {
		switch {
		case r.hasRef(plumbing.NewBranchReferenceName(def.Name)):
			o.DefaultBranch = def.Name
		case !def.Set && r.hasRef(plumbing.NewRemoteReferenceName("origin", def.Name)):
			o.DefaultBranch = "origin/" + def.Name
		default:
			o.EmptyDefaultBranch = def.Name
		}
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

// DefaultBranchKey is the git config key a repository's default branch
// can be set under, for when origin can't say which it is.
const DefaultBranchKey = "vibe.defaultBranch"

// Why the default branch can't be told. It's only ever what origin's HEAD
// names, else what was set for the repository: a guess (main, master) that's
// wrong is worse than none.
const (
	noOrigin     = "Can't tell the default branch: there's no origin remote."
	noOriginHEAD = "Can't tell the default branch: origin/HEAD isn't set. `git remote set-head origin --auto` sets it."
)

// Mainline is the repository's default branch, as far as it can be told.
type Mainline struct {
	// Name is the branch, without "origin/"; "" when it can't be told, Why
	// saying why.
	Name string `json:"name,omitempty"`
	// Set says Name is the one set for the repository (DefaultBranchKey),
	// origin not saying.
	Set bool   `json:"set,omitempty"`
	Why string `json:"why,omitempty"`
	// Branches are the repository's local branches, when Name can't be
	// told: what it might be set to.
	Branches []string `json:"branches,omitempty"`
}

// defaultBranch is the repository's main line: the branch origin's HEAD
// names, else the one set for the repository.
func (r *repository) defaultBranch() Mainline {
	why := noOrigin
	if _, err := r.git.Remote("origin"); err == nil {
		why = noOriginHEAD
		if ref, err := r.git.Storer.Reference("refs/remotes/origin/HEAD"); err == nil && ref.Type() == plumbing.SymbolicReference {
			if name, ok := strings.CutPrefix(ref.Target().Short(), "origin/"); ok && name != "" {
				return Mainline{Name: name}
			}
		}
	}
	if set := r.setDefaultBranch(); set != "" {
		// The sandbox can write the repository's config: what's there is
		// used only as a branch's name, and only once it's a valid one.
		if plumbing.NewBranchReferenceName(set).Validate() != nil || strings.HasPrefix(set, "-") {
			why = fmt.Sprintf("Can't tell the default branch: %s is set to %q, which isn't a branch name.", DefaultBranchKey, set)
		} else {
			return Mainline{Name: set, Set: true}
		}
	}
	return Mainline{Why: why, Branches: r.localBranches()}
}

func (r *repository) setDefaultBranch() string {
	cfg, err := r.git.Config()
	if err != nil {
		return ""
	}
	section, key, _ := strings.Cut(DefaultBranchKey, ".")
	return cfg.Raw.Section(section).Option(key)
}

func (r *repository) localBranches() []string {
	refs, err := r.git.Branches()
	if err != nil {
		return nil
	}
	var names []string
	refs.ForEach(func(ref *plumbing.Reference) error {
		names = append(names, ref.Name().Short())
		return nil
	})
	sort.Strings(names)
	return names
}

func (r *repository) hasRef(name plumbing.ReferenceName) bool {
	_, err := r.git.Reference(name, true)
	return err == nil
}

// originOf is the URL the repository dir is in has for its origin remote:
// "" when it has none.
func originOf(dir string) (string, error) {
	r, err := open(dir)
	if err != nil {
		return "", err
	}
	defer r.close()
	remote, err := r.git.Remote("origin")
	if err != nil {
		return "", nil
	}
	if urls := remote.Config().URLs; len(urls) > 0 {
		return urls[0], nil
	}
	return "", nil
}

func defaultBranchOf(dir string) (Mainline, error) {
	r, err := open(dir)
	if err != nil {
		return Mainline{}, err
	}
	defer r.close()
	return r.defaultBranch(), nil
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

// maxWalk bounds walking history for aheadBehind: past it, the counts so
// far stand (a branch thousands of commits from its upstream is far
// enough to say so, and the walk is the sandbox's to make long).
var maxWalk = 20000

// aheadBehind is how many commits a has that b hasn't, and b that a
// hasn't: git's way, walking back from both newest first until all that's
// left is history they share (or maxWalk commits have been walked).
func aheadBehind(a, b *object.Commit) (ahead, behind int) {
	if a == nil || b == nil {
		return 0, 0
	}
	const fromA, fromB = 1, 2
	flags := map[plumbing.Hash]int{}
	q := &byTime{}
	// fresh counts the commits waiting that only one side has reached:
	// none left, only shared history is.
	fresh := 0
	mark := func(c *object.Commit, f int) {
		was := flags[c.Hash]
		if was|f == was {
			return
		}
		flags[c.Hash] = was | f
		if was != 0 && was != fromA|fromB {
			fresh--
		}
		if was|f != fromA|fromB {
			fresh++
		}
		heap.Push(q, c)
	}
	mark(a, fromA)
	mark(b, fromB)
	for walked := 0; q.Len() > 0 && fresh > 0 && walked < maxWalk; walked++ {
		c := heap.Pop(q).(*object.Commit)
		f := flags[c.Hash]
		if f != fromA|fromB {
			fresh--
		}
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

// statusOf is the git status of the folder dir; an error when it's in no
// repository (or one that can't be read).
func statusOf(ctx context.Context, dir string) (*FolderStatus, error) {
	r, err := openRepo(dir)
	if err != nil {
		return nil, err
	}
	defer r.close()
	head, err := r.head()
	if err != nil {
		return nil, err
	}
	base, err := treeOf(head)
	if err != nil {
		return nil, err
	}
	sc, err := r.scan(newOverlay(r.git.Storer), base, false)
	if err != nil {
		return nil, err
	}
	s := &FolderStatus{Branch: r.branch(), Staged: sc.staged, Modified: sc.changed, Untracked: len(sc.untracked), Conflicted: sc.conflicted}
	if s.Branch == "" {
		s.Branch = "detached"
		if head != nil {
			s.Branch = "@" + head.Hash.String()[:7]
		}
	}
	if up, _ := r.upstream(r.branch()); up != nil {
		s.Ahead, s.Behind = aheadBehind(head, up)
	}
	s.Stashed = r.stashes()
	return s, nil
}

// stashes is how many stashes there are: the lines of the stash's log.
func (r *repository) stashes() int {
	if _, err := r.git.Storer.Reference("refs/stash"); err != nil {
		return 0
	}
	data, err := readIn(r.common, filepath.Join("logs", "refs", "stash"), 1<<20)
	if err != nil {
		return 0
	}
	return strings.Count(string(data), "\n")
}

// textOf is path's lines as review rv has it (see NewTextOf).
func textOf(ctx context.Context, dir string, rv Review, path string) ([]string, error) {
	text, err := newTextOf(ctx, dir, rv, path)
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

// moves are rv's comments on the new side whose code (as it read when
// they were made) is no longer at their lines but is elsewhere in the file:
// where to. A file that can't be read moves nothing: its comments stay, and
// the page marks any whose lines read differently.
func moves(ctx context.Context, dir string, rv Review) []Move {
	var out []Move
	files := map[string][]string{}
	for _, t := range rv.Threads {
		if t.File == "" || t.Side == "old" || t.Code == "" {
			continue
		}
		lines, seen := files[t.File]
		if !seen {
			lines, _ = textOf(ctx, dir, rv, t.File)
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
