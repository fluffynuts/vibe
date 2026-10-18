package review

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const oldRef = `<Project><ItemGroup><PackageReference Include="Corp.Lib" Version="1.0.0" /></ItemGroup></Project>`
const newRef = `<Project><ItemGroup><PackageReference Include="Corp.Lib" Version="2.0.0" /></ItemGroup></Project>`

func TestProjectFilesAreReadAtTheBranchAndAsTheyAreNow(t *testing.T) {
	dir := repo(t, map[string]string{"src/App/App.csproj": oldRef, "readme.md": "hi", "Directory.Packages.props": "<Project/>"})
	git(t, dir, "checkout", "-q", "-b", "feature")
	write(t, dir, "src/App/App.csproj", newRef)
	write(t, dir, "src/Lib/Lib.fsproj", "<Project/>")

	got, err := ReadProjectFiles(context.Background(), dir, "main")
	if err != nil {
		t.Fatal(err)
	}
	if got.Committed["src/App/App.csproj"] != oldRef || got.Working["src/App/App.csproj"] != newRef {
		t.Errorf("committed %v, working %v", got.Committed, got.Working)
	}
	if _, ok := got.Working["src/Lib/Lib.fsproj"]; !ok {
		t.Errorf("a new, untracked project file is in the working tree: %v", got.Working)
	}
	if _, ok := got.Committed["src/Lib/Lib.fsproj"]; ok {
		t.Errorf("it isn't on main: %v", got.Committed)
	}
	if _, ok := got.Committed["Directory.Packages.props"]; !ok {
		t.Errorf("central package versions are project files: %v", got.Committed)
	}
	for _, side := range []map[string]string{got.Committed, got.Working} {
		if _, ok := side["readme.md"]; ok {
			t.Error("only files that name packages are read")
		}
	}
}

func TestProjectFilesAreOnlyThoseInTheFolder(t *testing.T) {
	top := repo(t, map[string]string{"a/A.csproj": oldRef, "b/B.csproj": oldRef})
	got, err := ReadProjectFiles(context.Background(), filepath.Join(top, "a"), "main")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Committed) != 1 || len(got.Working) != 1 || got.Committed["A.csproj"] == "" || got.Working["A.csproj"] == "" {
		t.Errorf("committed %v, working %v: only the sandbox's own folder, paths from it", got.Committed, got.Working)
	}
}

func TestProjectFilesDoNotFollowLinksOrReadBuildOutput(t *testing.T) {
	dir := repo(t, map[string]string{"A.csproj": oldRef})
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "Secret.csproj"), []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "linked")); err != nil {
		t.Skipf("no symlinks here: %v", err)
	}
	if err := os.Symlink(filepath.Join(outside, "Secret.csproj"), filepath.Join(dir, "Sneaky.csproj")); err != nil {
		t.Fatal(err)
	}
	write(t, dir, "obj/Generated.csproj", "generated")
	write(t, dir, "node_modules/x/X.csproj", "dependency")
	got, err := ReadProjectFiles(context.Background(), dir, "main")
	if err != nil {
		t.Fatal(err)
	}
	for path, body := range got.Working {
		if strings.Contains(body, "secret") || path == "obj/Generated.csproj" || strings.HasPrefix(path, "node_modules") || strings.HasPrefix(path, "linked") {
			t.Errorf("read %s", path)
		}
	}
}

func TestProjectFilesWithNoBranchAreOnlyReadAsTheyAreNow(t *testing.T) {
	dir := repo(t, map[string]string{"A.csproj": oldRef})
	got, err := ReadProjectFiles(context.Background(), dir, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Committed) != 0 || got.Working["A.csproj"] != oldRef {
		t.Errorf("committed %v, working %v", got.Committed, got.Working)
	}
}

func TestAProjectFileTooLargeToBeOneIsLeftOut(t *testing.T) {
	dir := repo(t, map[string]string{"Big.csproj": strings.Repeat("x", maxProjectFile+1), "A.csproj": oldRef})
	got, err := ReadProjectFiles(context.Background(), dir, "main")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := got.Working["Big.csproj"]; ok {
		t.Error("read a file over the limit")
	}
	if _, ok := got.Committed["Big.csproj"]; ok {
		t.Error("read a committed file over the limit")
	}
	if got.Working["A.csproj"] == "" {
		t.Error("the others are read")
	}
}
