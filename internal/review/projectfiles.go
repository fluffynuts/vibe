package review

import (
	"context"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/go-git/go-git/v5/plumbing/object"

	"vibe/internal/plainfile"
)

const (
	// maxProjectFile is the most of a project file that's read: they're
	// small, and the sandbox writes them.
	maxProjectFile = 1 << 20
	// maxProjectFiles is how many are read, a side at a time.
	maxProjectFiles = 2000
)

// ProjectFiles are the text of the files in a folder that name the packages
// a project uses (see projectFile), as the sandbox's agent can't be asked
// about them: at a branch, and as they are now.
type ProjectFiles struct {
	// Committed are the files at the branch asked for, by path from the
	// folder; none when no branch was.
	Committed map[string]string `json:"committed"`
	// Working are the files as the folder has them, untracked ones too.
	Working map[string]string `json:"working"`
	// Truncated says project files were left unread: too large, too many, or unreadable. A package
	// none of the files read name may be named in one of them.
	Truncated bool `json:"truncated,omitempty"`
}

// projectFile is a file that names packages: a project, or the .props and
// .targets that centralise their versions.
func projectFile(name string) bool {
	switch strings.ToLower(path.Ext(name)) {
	case ".csproj", ".fsproj", ".vbproj", ".props", ".targets":
		return true
	}
	return false
}

// notProjectFolder is a folder that holds build output or dependencies, not
// a project's own files.
func notProjectFolder(name string) bool {
	switch name {
	case ".git", "bin", "obj", "node_modules", ".vs":
		return true
	}
	return false
}

// ReadProjectFiles reads the project files in the folder dir, at branch ref
// (a branch, say; "" for none) and as they are now, as plain text: nothing
// is run, and links aren't followed.
func ReadProjectFiles(ctx context.Context, dir, ref string) (ProjectFiles, error) {
	var out ProjectFiles
	return out, isolated(ctx, "projectFiles", helperArgs{Dir: dir, Ref: ref}, &out)
}

func projectFilesOf(ctx context.Context, dir, ref string) (ProjectFiles, error) {
	out := ProjectFiles{Committed: map[string]string{}, Working: map[string]string{}}
	r, err := open(dir)
	if err != nil {
		return out, err
	}
	defer r.close()
	if ref != "" {
		if err := r.committedProjectFiles(ref, dir, &out); err != nil {
			return out, err
		}
	}
	err = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		switch {
		case d.IsDir():
			if p != dir && notProjectFolder(d.Name()) {
				return fs.SkipDir
			}
		case d.Type().IsRegular() && projectFile(d.Name()):
			if len(out.Working) >= maxProjectFiles {
				out.Truncated = true
				return nil
			}
			data, err := plainfile.Read(p, maxProjectFile)
			switch {
			case err == nil:
				rel, _ := filepath.Rel(dir, p)
				out.Working[filepath.ToSlash(rel)] = string(data)
			case !os.IsNotExist(err):
				// Too large, or not readable: said, so what isn't in the files read isn't taken for absent.
				out.Truncated = true
			}
		}
		return nil
	})
	return out, err
}

// committedProjectFiles reads into out.Committed the project files in the folder dir
// as commit ref has them.
func (r *repository) committedProjectFiles(ref, dir string, out *ProjectFiles) error {
	c, err := r.commit(ref)
	if err != nil {
		return err
	}
	tree, err := treeOf(c)
	if err != nil {
		return err
	}
	// r.top is where the repository really is: the folder is found against that, by where it really is.
	real, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return err
	}
	prefix := ""
	if rel, err := filepath.Rel(r.top, real); err == nil && rel != "." {
		prefix = filepath.ToSlash(rel) + "/"
	}
	files := tree.Files()
	defer files.Close()
	return files.ForEach(func(f *object.File) error {
		if !f.Mode.IsFile() || !strings.HasPrefix(f.Name, prefix) || !projectFile(f.Name) {
			return nil
		}
		rel := strings.TrimPrefix(f.Name, prefix)
		for _, part := range strings.Split(path.Dir(rel), "/") {
			if notProjectFolder(part) {
				return nil
			}
		}
		if f.Size > maxProjectFile || len(out.Committed) >= maxProjectFiles {
			out.Truncated = true
			return nil
		}
		reader, err := f.Reader()
		if err != nil {
			out.Truncated = true
			return nil
		}
		defer reader.Close()
		data, err := io.ReadAll(io.LimitReader(reader, maxProjectFile+1))
		if err != nil || len(data) > maxProjectFile {
			out.Truncated = true
			return nil
		}
		out.Committed[rel] = string(data)
		return nil
	})
}
