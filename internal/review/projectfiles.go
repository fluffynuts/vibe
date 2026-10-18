package review

import (
	"context"
	"errors"
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
		if err := r.committedProjectFiles(ref, dir, out.Committed); err != nil {
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
		case d.Type().IsRegular() && projectFile(d.Name()) && len(out.Working) < maxProjectFiles:
			data, err := plainfile.Read(p, maxProjectFile)
			if err == nil {
				rel, _ := filepath.Rel(dir, p)
				out.Working[filepath.ToSlash(rel)] = string(data)
			} else if !errors.As(err, &plainfile.TooLarge{}) && !os.IsNotExist(err) {
				return nil
			}
		}
		return nil
	})
	return out, err
}

// committedProjectFiles reads into into the project files in the folder dir
// as commit ref has them.
func (r *repository) committedProjectFiles(ref, dir string, into map[string]string) error {
	c, err := r.commit(ref)
	if err != nil {
		return err
	}
	tree, err := treeOf(c)
	if err != nil {
		return err
	}
	prefix := ""
	if rel, err := filepath.Rel(r.top, dir); err == nil && rel != "." {
		prefix = filepath.ToSlash(rel) + "/"
	}
	files := tree.Files()
	defer files.Close()
	return files.ForEach(func(f *object.File) error {
		if !f.Mode.IsFile() || !strings.HasPrefix(f.Name, prefix) || !projectFile(f.Name) || f.Size > maxProjectFile || len(into) >= maxProjectFiles {
			return nil
		}
		rel := strings.TrimPrefix(f.Name, prefix)
		for _, part := range strings.Split(path.Dir(rel), "/") {
			if notProjectFolder(part) {
				return nil
			}
		}
		reader, err := f.Reader()
		if err != nil {
			return nil
		}
		defer reader.Close()
		data, err := io.ReadAll(io.LimitReader(reader, maxProjectFile+1))
		if err != nil || len(data) > maxProjectFile {
			return nil
		}
		into[rel] = string(data)
		return nil
	})
}
