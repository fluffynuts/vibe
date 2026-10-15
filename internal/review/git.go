package review

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
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
	return diffFrom(ctx, dir, base)
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
	return diffFrom(ctx, dir, strings.TrimSpace(base))
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
	return Parse(out)
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
