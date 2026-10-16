package review

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/filemode"
	"github.com/go-git/go-git/v5/plumbing/format/gitignore"
	"github.com/go-git/go-git/v5/plumbing/format/index"
	"github.com/go-git/go-git/v5/plumbing/object"
)

// scanned is the folder compared as git status compares it: HEAD with the
// index (what's staged), the index with the work tree (what's changed), and
// what's on disk that isn't tracked or ignored.
type scanned struct {
	// edits make HEAD's tree the work tree's (see workTree), when asked for.
	edits                       map[string]edit
	untracked                   []string
	staged, changed, conflicted int
}

// scan compares the folder as git status does, without git: so nothing the
// repository's config names runs, and the work tree is read only here,
// bounded: through an os.Root, a file over maxFileBytes not read, nothing
// opened that would wait (a FIFO). withEdits gathers what workTree needs.
func (r *repository) scan(o *overlay, base *object.Tree, withEdits bool) (*scanned, error) {
	idx, err := r.git.Storer.Index()
	if err != nil {
		return nil, fmt.Errorf("the index can't be read: %w", err)
	}
	head := map[string]object.TreeEntry{}
	if base != nil && len(base.Entries) > 0 {
		walker := object.NewTreeWalker(base, true, nil)
		defer walker.Close()
		for {
			name, entry, err := walker.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				return nil, err
			}
			if entry.Mode != filemode.Dir {
				head[name] = entry
			}
		}
	}
	// A file written no earlier than the index was can't be told unchanged
	// by its time (git's "racy" entries): it's read.
	var indexed time.Time
	if info, err := r.gitDir.Stat("index"); err == nil {
		indexed = info.ModTime()
	}
	sc := &scanned{edits: map[string]edit{}}
	tracked := map[string]bool{}
	conflicted := map[string]bool{}
	for _, e := range idx.Entries {
		tracked[e.Name] = true
		// An entry merged is stage 0 (go-git's index.Merged says 1, the
		// ancestor's stage in a conflict, which is what 1 is).
		if e.Stage != 0 {
			conflicted[e.Name] = true
			continue
		}
		if e.Mode == filemode.Submodule {
			continue
		}
		was, inHead := head[e.Name]
		staged := !inHead || was.Hash != e.Hash || was.Mode != e.Mode
		same, ed, err := r.onDisk(o, e, indexed)
		if err != nil {
			return nil, fmt.Errorf("%s can't be read: %w", e.Name, err)
		}
		if staged {
			sc.staged++
		}
		if !same {
			sc.changed++
		}
		switch {
		case !withEdits:
		case !same:
			sc.edits[e.Name] = ed
		case staged:
			sc.edits[e.Name] = edit{hash: e.Hash, mode: e.Mode}
		}
	}
	for p := range head {
		if !tracked[p] {
			// Taken out of the index (git rm --cached): a staged deletion.
			sc.staged++
			if withEdits {
				sc.edits[p] = edit{gone: true}
			}
		}
	}
	sc.conflicted = len(conflicted)
	if sc.untracked, err = r.untrackedPaths(tracked); err != nil {
		return nil, err
	}
	return sc, nil
}

// onDisk compares index entry e with its file on disk: same, as git sees
// it; else the edit making the tree's entry what's on disk. A file whose
// size and time are what the index recorded, from before the index was
// written (indexed), isn't read (as git doesn't); one over maxFileBytes
// isn't either: it stands as changed.
func (r *repository) onDisk(o *overlay, e *index.Entry, indexed time.Time) (bool, edit, error) {
	if e.SkipWorktree {
		return true, edit{}, nil
	}
	name := filepath.FromSlash(e.Name)
	info, err := r.work.Lstat(name)
	if errors.Is(err, os.ErrNotExist) || errors.Is(err, os.ErrInvalid) || isNotDir(err) {
		return false, edit{gone: true}, nil
	}
	if err != nil {
		return false, edit{}, err
	}
	switch {
	case info.Mode()&os.ModeSymlink != 0:
		target, err := r.work.Readlink(name)
		if err != nil {
			return false, edit{}, err
		}
		data := []byte(filepath.ToSlash(target))
		if plumbing.ComputeHash(plumbing.BlobObject, data) == e.Hash && e.Mode == filemode.Symlink {
			return true, edit{}, nil
		}
		h, err := o.blob(data)
		return false, edit{hash: h, mode: filemode.Symlink}, err
	case info.Mode().IsRegular():
		mode := r.modeOf(info, e.Mode)
		if mode == e.Mode && uint32(info.Size()) == e.Size && info.ModTime().Equal(e.ModifiedAt) && info.ModTime().Before(indexed) {
			return true, edit{}, nil
		}
		if info.Size() > maxFileBytes {
			h, err := o.blob([]byte(fmt.Sprintf("%d bytes, %d", info.Size(), info.ModTime().UnixNano())))
			o.large[h] = true
			return false, edit{hash: h, mode: mode}, err
		}
		data, err := readIn(r.work, name, maxFileBytes)
		if err != nil {
			return false, edit{}, err
		}
		if data, err = r.asCommitted(o, e.Name, e.Hash, data); err != nil {
			return false, edit{}, err
		}
		if mode == e.Mode && plumbing.ComputeHash(plumbing.BlobObject, data) == e.Hash {
			return true, edit{}, nil
		}
		h, err := o.blob(data)
		return false, edit{hash: h, mode: mode}, err
	}
	// A folder, a FIFO: not the file the index has.
	return false, edit{gone: true}, nil
}

// isNotDir is a path through something that isn't a folder (a file where a
// folder was): the file the index has isn't there.
func isNotDir(err error) bool {
	return err != nil && strings.Contains(err.Error(), "not a directory")
}

// modeOf is a file's mode as git would record it: executable or not, by
// its bit, unless core.fileMode is false (or there's no such bit: Windows),
// when what the index has stands.
func (r *repository) modeOf(info os.FileInfo, indexed filemode.FileMode) filemode.FileMode {
	if !r.fileMode || os.PathSeparator == '\\' {
		if indexed == filemode.Executable {
			return filemode.Executable
		}
		return filemode.Regular
	}
	if info.Mode().Perm()&0o111 != 0 {
		return filemode.Executable
	}
	return filemode.Regular
}

// untrackedPaths are the files and links on disk that aren't tracked, nor
// ignored (by .gitignore files, info/exclude and the excludes files git
// reads), in path order; a folder that's ignored isn't gone into.
func (r *repository) untrackedPaths(tracked map[string]bool) ([]string, error) {
	base, err := r.excludes()
	if err != nil {
		return nil, err
	}
	var out []string
	var walk func(folder string, parts []string, patterns []gitignore.Pattern) error
	walk = func(folder string, parts []string, patterns []gitignore.Pattern) error {
		own, err := ignoreFile(r.work, path.Join(folder, ".gitignore"), parts)
		if err != nil {
			return err
		}
		patterns = append(patterns[:len(patterns):len(patterns)], own...)
		ignored := gitignore.NewMatcher(patterns)
		d, err := r.work.Open(native(folder))
		if err != nil {
			return err
		}
		entries, err := d.ReadDir(-1)
		d.Close()
		if err != nil {
			return err
		}
		sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
		for _, e := range entries {
			name := e.Name()
			if name == ".git" {
				continue
			}
			p := path.Join(folder, name)
			at := append(parts[:len(parts):len(parts)], name)
			if e.IsDir() {
				if tracked[p] || ignored.Match(at, true) {
					// A tracked folder is a submodule: its own repository.
					continue
				}
				if _, err := r.work.Lstat(filepath.Join(native(p), ".git")); err == nil {
					// Another repository, not tracked: git leaves it out.
					continue
				}
				if err := walk(p, at, patterns); err != nil {
					return err
				}
				continue
			}
			if tracked[p] || ignored.Match(at, false) {
				continue
			}
			if e.Type().IsRegular() || e.Type()&os.ModeSymlink != 0 {
				out = append(out, p)
			}
		}
		return nil
	}
	if err := walk("", nil, base); err != nil {
		return nil, err
	}
	return out, nil
}

// ignoreFile is the patterns of the ignore file at name in root, for the
// folder parts; none when there's none. One that can't be read (a FIFO,
// say) is said to be.
func ignoreFile(root *os.Root, name string, parts []string) ([]gitignore.Pattern, error) {
	data, err := readIn(root, native(name), 1<<20)
	if errors.Is(err, os.ErrNotExist) || isNotDir(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("%s can't be read: %w", name, err)
	}
	return parseIgnore(data, parts), nil
}

func parseIgnore(data []byte, parts []string) []gitignore.Pattern {
	var out []gitignore.Pattern
	lines := bufio.NewScanner(bytes.NewReader(data))
	for lines.Scan() {
		line := strings.TrimRight(lines.Text(), "\r")
		if line != "" && !strings.HasPrefix(line, "#") {
			out = append(out, gitignore.ParsePattern(line, parts))
		}
	}
	return out
}

// excludes are the patterns of the excludes git reads besides .gitignore
// files: the user's (core.excludesfile, else git's default, ~/.config/git/
// ignore), then info/exclude. The host's own files are read as git would;
// an excludes file a sandbox's repository names is read only from in the
// folder.
func (r *repository) excludes() ([]gitignore.Pattern, error) {
	var out []gitignore.Pattern
	global := ""
	// The user's own config alone: go-git's ConfigScoped(GlobalScope) has
	// the repository's merged into it.
	if cfg, err := config.LoadConfig(config.GlobalScope); err == nil {
		global = cfg.Raw.Section("core").Option("excludesfile")
	}
	if global == "" {
		if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
			global = filepath.Join(xdg, "git", "ignore")
		} else if home, err := os.UserHomeDir(); err == nil {
			global = filepath.Join(home, ".config", "git", "ignore")
		}
	}
	if data, err := hostRead(expandHome(global)); err == nil {
		out = append(out, parseIgnore(data, nil)...)
	}
	if cfg, err := r.git.ConfigScoped(config.LocalScope); err == nil {
		if own := expandHome(cfg.Raw.Section("core").Option("excludesfile")); own != "" {
			switch {
			case r.trusted:
				if data, err := hostRead(own); err == nil {
					out = append(out, parseIgnore(data, nil)...)
				}
			case !filepath.IsAbs(own) || inFolderTree(own, r.top):
				rel := own
				if filepath.IsAbs(own) {
					rel, _ = filepath.Rel(r.top, own)
				}
				ps, err := ignoreFile(r.work, filepath.ToSlash(rel), nil)
				if err != nil {
					return nil, err
				}
				out = append(out, ps...)
			}
		}
	}
	ps, err := ignoreFile(r.common, "info/exclude", nil)
	if err != nil {
		return nil, err
	}
	return append(out, ps...), nil
}

// hostRead reads a file of the host's own (a config or excludes file:
// never one the sandbox can write), up to 1 MB.
func hostRead(name string) ([]byte, error) {
	if name == "" {
		return nil, os.ErrNotExist
	}
	f, err := os.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if info, err := f.Stat(); err != nil || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s isn't a plain file", name)
	}
	return io.ReadAll(io.LimitReader(f, 1<<20))
}

func expandHome(name string) string {
	if rest, ok := strings.CutPrefix(name, "~/"); ok {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, rest)
		}
	}
	return name
}
