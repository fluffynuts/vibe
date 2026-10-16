package review

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// emptyTree is git's tree with nothing in it: what a repository with no
// commits yet is compared with.
const emptyTree = "4b825dc642cb6eb9a060e54bf8d69288fbee4904"

// maxUntracked is the most of an untracked file shown: past it, the file
// is listed without its lines.
const maxUntracked = 1 << 20

// diffFlags make git's diff what Parse reads, whatever the user's git
// config says: no colour, no external diff tool or text conversion (which
// the repository's own config could make run anything), a/ and b/
// prefixes, and renames found.
var diffFlags = []string{"--no-color", "--no-ext-diff", "--no-textconv", "--src-prefix=a/", "--dst-prefix=b/", "-M"}

// Diffing is how a diff is shown: IgnoreSpace leaves out what changed only
// in whitespace (an untracked file is all new, so it's shown whole).
type Diffing struct {
	IgnoreSpace bool
}

// flags are diffFlags, for how.
func flags(how []Diffing) []string {
	out := append([]string{}, diffFlags...)
	for _, h := range how {
		if h.IgnoreSpace {
			out = append(out, "--ignore-all-space")
		}
	}
	return out
}

// WorkingTree is the diff of the folder dir as it is: everything not yet
// committed, staged or not, and untracked files (those git doesn't ignore)
// as added.
func WorkingTree(ctx context.Context, dir string, how ...Diffing) ([]File, error) {
	if err := isRepository(ctx, dir); err != nil {
		return nil, err
	}
	base := "HEAD"
	if _, err := run(ctx, dir, "rev-parse", "--verify", "--quiet", "HEAD"); err != nil {
		base = emptyTree
	}
	return withDetails(ctx, dir, "", base)(diffFrom(ctx, dir, base, how))
}

// Against is the diff of the folder dir as it is from where it parted from
// ref (a branch, say): this branch's changes, committed or not, without
// those ref has gained since.
func Against(ctx context.Context, dir, ref string, how ...Diffing) ([]File, error) {
	if err := isRepository(ctx, dir); err != nil {
		return nil, err
	}
	if err := isCommit(ctx, dir, ref); err != nil {
		return nil, err
	}
	base, err := run(ctx, dir, "merge-base", ref, "HEAD")
	if err != nil {
		return nil, fmt.Errorf("%s and the folder's HEAD have no history in common: %w", ref, err)
	}
	parted := strings.TrimSpace(base)
	return withDetails(ctx, dir, "", parted)(diffFrom(ctx, dir, parted, how))
}

// Commit is the diff of what commit ref changed.
func Commit(ctx context.Context, dir, ref string, how ...Diffing) ([]File, error) {
	if err := isRepository(ctx, dir); err != nil {
		return nil, err
	}
	if err := isCommit(ctx, dir, ref); err != nil {
		return nil, err
	}
	// A merge commit's change is what it brought in to its first parent,
	// which is what its old side is read from.
	out, err := run(ctx, dir, append([]string{"show", "--format=", "-m", "--first-parent"}, append(flags(how), ref)...)...)
	if err != nil {
		return nil, err
	}
	return withDetails(ctx, dir, ref, ref+"^")(Parse(out))
}

// diffFrom is the working tree's diff from base, untracked files with it,
// in path order.
func diffFrom(ctx context.Context, dir, base string, how []Diffing) ([]File, error) {
	out, err := run(ctx, dir, append([]string{"diff"}, append(flags(how), base)...)...)
	if err != nil {
		return nil, err
	}
	files, err := Parse(out)
	if err != nil {
		return nil, err
	}
	listed, err := run(ctx, dir, "ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return nil, err
	}
	for _, path := range strings.Split(listed, "\x00") {
		if path != "" {
			files = append(files, untracked(dir, path))
		}
	}
	sort.SliceStable(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files, nil
}

// untracked is an untracked file, as git would show it added: a link as
// where it points, never what's there (it may point out of the folder, at
// a host file, or at /dev/zero); only a plain file's content is read.
func untracked(dir, path string) File {
	f := File{Path: path, Status: Added}
	full := filepath.Join(dir, path)
	info, err := os.Lstat(full)
	if err != nil {
		f.Unreadable = err.Error()
		return f
	}
	// Its size and time stand for its content until it's read.
	f.stamp = fmt.Sprintf("%d %d", info.Size(), info.ModTime().UnixNano())
	if info.Mode()&os.ModeSymlink != 0 {
		target, err := os.Readlink(full)
		if err != nil {
			f.Unreadable = err.Error()
			return f
		}
		f.stamp = "link " + target
		f.Hunks = []Hunk{{OldStart: 0, NewStart: 1, Lines: []Line{{Kind: Addition, New: 1, Text: target}}}}
		return f
	}
	if info.Size() > maxUntracked {
		f.TooLarge = true
		return f
	}
	body, err := readPlain(full, maxUntracked)
	if err != nil {
		f.Unreadable = err.Error()
		return f
	}
	sum := sha256.Sum256(body)
	f.stamp = hex.EncodeToString(sum[:])
	// As git tells: a NUL early on means binary.
	if bytes.IndexByte(body[:min(len(body), 8000)], 0) >= 0 {
		f.Binary = true
		return f
	}
	text := strings.TrimSuffix(string(body), "\n")
	if text == "" {
		return f
	}
	h := Hunk{OldStart: 0, NewStart: 1}
	for i, line := range splitLines(text) {
		h.Lines = append(h.Lines, Line{Kind: Addition, New: i + 1, Text: line})
	}
	f.Hunks = []Hunk{h}
	return f
}

func isRepository(ctx context.Context, dir string) error {
	if _, err := exec.LookPath("git"); err != nil {
		return fmt.Errorf("git isn't installed on this machine, and the Review tab needs it to work out the diff")
	}
	if _, err := run(ctx, dir, "rev-parse", "--git-dir"); err != nil {
		if strings.Contains(err.Error(), "not a git repository") {
			return fmt.Errorf("%s isn't a git repository, so there's no diff to review", dir)
		}
		return fmt.Errorf("git can't use %s as a repository, so there's no diff to review: %w", dir, err)
	}
	return nil
}

// Root is the top of the repository folder dir is in: what git's paths are
// relative to.
func Root(ctx context.Context, dir string) (string, error) {
	if err := isRepository(ctx, dir); err != nil {
		return "", err
	}
	top, err := run(ctx, dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", err
	}
	return filepath.FromSlash(strings.TrimSpace(top)), nil
}

func isCommit(ctx context.Context, dir, ref string) error {
	if strings.HasPrefix(ref, "-") {
		return fmt.Errorf("%q isn't a branch or commit", ref)
	}
	if _, err := run(ctx, dir, "rev-parse", "--verify", "--quiet", ref+"^{commit}"); err != nil {
		return fmt.Errorf("there's no branch or commit %q in %s", ref, dir)
	}
	return nil
}

// hostSafe is how git is run on this machine in a folder the sandbox can
// write, .git/config with it: nothing the repository says to run runs. No
// file system monitor, no hooks, no optional index writes (which would run
// some); diffs take no text conversion or external tool (diffFlags); and
// filters, which can't be turned off by name in advance, are refused
// (ownFilters). Paths are left as they are, not \-quoted.
var hostSafe = []string{"--no-optional-locks", "-c", "core.quotepath=false", "-c", "core.fsmonitor=false", "-c", "core.hooksPath=" + os.DevNull}

// readsWorkingTree are the git commands that read the folder's files, and
// so run the repository's filters on them.
var readsWorkingTree = map[string]bool{"diff": true, "status": true}

// Git runs git in dir as the Review tab does (see hostSafe), for others on
// this machine that read the sandbox's folder.
func Git(ctx context.Context, dir string, args ...string) (string, error) {
	return run(ctx, dir, args...)
}

// run runs git in dir, safely (see hostSafe), and gives what it wrote;
// failing, its error says what git said.
func run(ctx context.Context, dir string, args ...string) (string, error) {
	if readsWorkingTree[args[0]] {
		if err := ownFilters(ctx, dir); err != nil {
			return "", err
		}
	}
	cmd := exec.CommandContext(ctx, "git", append(append([]string{}, hostSafe...), args...)...)
	cmd.Dir = dir
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	if err := cmd.Run(); err != nil {
		if msg := strings.TrimSpace(errOut.String()); msg != "" {
			return "", fmt.Errorf("git %s: %s", args[0], msg)
		}
		return "", fmt.Errorf("git %s: %w", args[0], err)
	}
	return out.String(), nil
}

// ownFilters refuses a repository whose own config (not the user's, which
// is theirs to trust, git-lfs's say) sets a filter: one runs a command on
// each file git reads from the folder, and the sandbox can write the config.
func ownFilters(ctx context.Context, dir string) error {
	cmd := exec.CommandContext(ctx, "git", "config", "--show-scope", "--get-regexp", `^filter\.`)
	cmd.Dir = dir
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	if err := cmd.Run(); err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) && exit.ExitCode() == 1 && errOut.Len() == 0 {
			return nil // none set
		}
		return fmt.Errorf("the repository's git config couldn't be checked for commands it would run (git 2.26 or later is needed): %s", strings.TrimSpace(errOut.String()+" "+err.Error()))
	}
	for _, line := range strings.Split(out.String(), "\n") {
		scope, setting, _ := strings.Cut(line, "\t")
		if scope != "local" && scope != "worktree" {
			continue
		}
		key, _, _ := strings.Cut(setting, " ")
		return fmt.Errorf("the repository's own git config sets %s, a command git would run on this machine for each file it reads: the Review tab won't run git there (take it out of .git/config, or set it in your own ~/.gitconfig if you trust it)", key)
	}
	return nil
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
	if err := isRepository(ctx, dir); err != nil {
		return o, err
	}
	status, err := run(ctx, dir, "status", "--porcelain", "--untracked-files=normal")
	if err != nil {
		return o, err
	}
	o.Uncommitted = strings.TrimSpace(status) != ""
	current, _ := run(ctx, dir, "symbolic-ref", "--quiet", "--short", "HEAD")
	current = strings.TrimSpace(current)
	if def := defaultBranch(ctx, dir); def != "" && def != current {
		o.DefaultBranch = def
	}
	if up, err := run(ctx, dir, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{upstream}"); err == nil {
		count, err := run(ctx, dir, "rev-list", "--count", "@{upstream}..HEAD")
		if err != nil {
			return o, err
		}
		if n, _ := strconv.Atoi(strings.TrimSpace(count)); n > 0 {
			o.Upstream, o.Unpushed = strings.TrimSpace(up), n
		}
	}
	return o, nil
}

// defaultBranch is the repository's main line, as origin says (its HEAD),
// else main or master, whichever is there; "" when none is. The local
// branch's name when there is one, else the remote's.
func defaultBranch(ctx context.Context, dir string) string {
	exists := func(ref string) bool {
		_, err := run(ctx, dir, "rev-parse", "--verify", "--quiet", ref+"^{commit}")
		return err == nil
	}
	if remote, err := run(ctx, dir, "symbolic-ref", "--quiet", "--short", "refs/remotes/origin/HEAD"); err == nil {
		remote = strings.TrimSpace(remote)
		if local := strings.TrimPrefix(remote, "origin/"); exists("refs/heads/" + local) {
			return local
		}
		return remote
	}
	for _, name := range []string{"main", "master"} {
		if exists("refs/heads/" + name) {
			return name
		}
	}
	return ""
}

// Unpushed is the diff of the commits on HEAD that upstream hasn't: what
// they changed, from where the two parted, without anything uncommitted.
func Unpushed(ctx context.Context, dir, upstream string, how ...Diffing) ([]File, error) {
	if err := isRepository(ctx, dir); err != nil {
		return nil, err
	}
	if err := isCommit(ctx, dir, upstream); err != nil {
		return nil, err
	}
	base, err := run(ctx, dir, "merge-base", upstream, "HEAD")
	if err != nil {
		return nil, fmt.Errorf("%s and the folder's HEAD have no history in common: %w", upstream, err)
	}
	out, err := run(ctx, dir, append([]string{"diff"}, append(flags(how), strings.TrimSpace(base), "HEAD")...)...)
	if err != nil {
		return nil, err
	}
	return withDetails(ctx, dir, "HEAD", strings.TrimSpace(base))(Parse(out))
}

// withDetails is a step after working out a diff: it fills in each file's
// fingerprint, and its line count, from newRev (the folder itself when "")
// for the files there, and from oldRev for the deleted ones.
func withDetails(ctx context.Context, dir, newRev, oldRev string) func([]File, error) ([]File, error) {
	return func(files []File, err error) ([]File, error) {
		if err != nil {
			return files, err
		}
		for i := range files {
			f := &files[i]
			f.Fingerprint = fingerprint(*f)
			if f.Binary {
				continue
			}
			switch {
			case f.Status == Deleted:
				f.Lines = linesAt(ctx, dir, oldRev, f.Path)
			case newRev == "":
				f.Lines = linesOnDisk(filepath.Join(dir, f.Path))
			default:
				f.Lines = linesAt(ctx, dir, newRev, f.Path)
			}
		}
		return files, nil
	}
}

// linesAt is how many lines path has at rev; 0 if it can't be read.
func linesAt(ctx context.Context, dir, rev, path string) int {
	out, err := run(ctx, dir, "show", rev+":"+path)
	if err != nil {
		return 0
	}
	return countLines(strings.NewReader(out))
}

// linesOnDisk is how many lines the file at path has, as git sees it: a
// link's one (where it points); 0 if it isn't a plain file, or can't be
// read.
func linesOnDisk(path string) int {
	if info, err := os.Lstat(path); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return 1
	}
	f, err := openNoFollow(path)
	if err != nil {
		return 0
	}
	defer f.Close()
	if info, err := f.Stat(); err != nil || !info.Mode().IsRegular() {
		return 0
	}
	return countLines(f)
}

// countLines counts r's lines: a last line without a newline counts too.
func countLines(r io.Reader) int {
	buf := make([]byte, 64*1024)
	n, last := 0, byte('\n')
	for {
		k, err := r.Read(buf)
		for _, b := range buf[:k] {
			if b == '\n' {
				n++
			}
		}
		if k > 0 {
			last = buf[k-1]
		}
		if err != nil {
			break
		}
	}
	if last != '\n' {
		n++
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
// (the blobs git compares, the modes; an untracked file's content), not its
// hunks, which change with how the diff is shown (see Diffing).
func fingerprint(f File) string {
	data, _ := json.Marshal(struct {
		Path, OldPath, Status, Stamp string
		Binary, TooLarge             bool
	}{f.Path, f.OldPath, f.Status, f.stamp, f.Binary, f.TooLarge})
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:12])
}
