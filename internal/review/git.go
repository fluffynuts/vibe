package review

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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
// config says: no colour, no external diff tool, a/ and b/ prefixes, and
// renames found.
var diffFlags = []string{"--no-color", "--no-ext-diff", "--src-prefix=a/", "--dst-prefix=b/", "-M"}

// WorkingTree is the diff of the folder dir as it is: everything not yet
// committed, staged or not, and untracked files (those git doesn't ignore)
// as added.
func WorkingTree(ctx context.Context, dir string) ([]File, error) {
	if err := isRepository(ctx, dir); err != nil {
		return nil, err
	}
	base := "HEAD"
	if _, err := run(ctx, dir, "rev-parse", "--verify", "--quiet", "HEAD"); err != nil {
		base = emptyTree
	}
	return withDetails(ctx, dir, "", base)(diffFrom(ctx, dir, base))
}

// Against is the diff of the folder dir as it is from where it parted from
// ref (a branch, say): this branch's changes, committed or not, without
// those ref has gained since.
func Against(ctx context.Context, dir, ref string) ([]File, error) {
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
	return withDetails(ctx, dir, "", parted)(diffFrom(ctx, dir, parted))
}

// Commit is the diff of what commit ref changed.
func Commit(ctx context.Context, dir, ref string) ([]File, error) {
	if err := isRepository(ctx, dir); err != nil {
		return nil, err
	}
	if err := isCommit(ctx, dir, ref); err != nil {
		return nil, err
	}
	out, err := run(ctx, dir, append([]string{"show", "--format="}, append(diffFlags, ref)...)...)
	if err != nil {
		return nil, err
	}
	return withDetails(ctx, dir, ref, ref+"^")(Parse(out))
}

// diffFrom is the working tree's diff from base, untracked files with it,
// in path order.
func diffFrom(ctx context.Context, dir, base string) ([]File, error) {
	out, err := run(ctx, dir, append([]string{"diff"}, append(diffFlags, base)...)...)
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

// untracked is an untracked file, as git would show it added.
func untracked(dir, path string) File {
	f := File{Path: path, Status: Added}
	info, err := os.Stat(filepath.Join(dir, path))
	if err != nil || info.Size() > maxUntracked {
		f.TooLarge = err == nil
		return f
	}
	body, err := os.ReadFile(filepath.Join(dir, path))
	if err != nil {
		return f
	}
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
	for i, line := range strings.Split(text, "\n") {
		h.Lines = append(h.Lines, Line{Kind: Addition, New: i + 1, Text: strings.TrimSuffix(line, "\r")})
	}
	f.Hunks = []Hunk{h}
	return f
}

func isRepository(ctx context.Context, dir string) error {
	if _, err := exec.LookPath("git"); err != nil {
		return fmt.Errorf("git isn't installed on this machine, and the Review tab needs it to work out the diff")
	}
	if _, err := run(ctx, dir, "rev-parse", "--git-dir"); err != nil {
		return fmt.Errorf("%s isn't a git repository, so there's no diff to review", dir)
	}
	return nil
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

// run runs git in dir, with paths left as they are (not \-quoted), and
// gives what it wrote; failing, its error says what git said.
func run(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-c", "core.quotepath=false"}, args...)...)
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
func Unpushed(ctx context.Context, dir, upstream string) ([]File, error) {
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
	out, err := run(ctx, dir, append([]string{"diff"}, append(diffFlags, strings.TrimSpace(base), "HEAD")...)...)
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

// linesOnDisk is how many lines the file at path has; 0 if it can't be
// read.
func linesOnDisk(path string) int {
	f, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer f.Close()
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
	return strings.Split(strings.TrimSuffix(text, "\n"), "\n"), nil
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

// fingerprint stands for f's change: a digest of what it is and its hunks.
func fingerprint(f File) string {
	data, _ := json.Marshal(struct {
		Path, OldPath, Status string
		Binary, TooLarge      bool
		Hunks                 []Hunk
	}{f.Path, f.OldPath, f.Status, f.Binary, f.TooLarge, f.Hunks})
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:12])
}
