package fscopy

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// A file vibe writes may be read while it is being replaced, and a Windows file
// in use can't be read or replaced for that moment: the whole-file write and the
// reading that waits (WriteWholeMode, ReadFile) are how vibe reads and writes
// its files, so it stays one way. A file that is not vibe's to replace (the
// system's) or whose creation is the point is allowed to be read or written
// directly, with the reason.
var directFileAccess = map[string]string{
	"src/vibe/diagnose.go":             "reads /proc: the system's, never replaced by vibe",
	"internal/timezone/detect_unix.go": "reads /etc/timezone: the system's",
	"internal/review/limit_linux.go":   "reads /proc/self/status: the system's",
	"internal/kvmcheck/probe_linux.go": "reads /proc/cpuinfo: the system's",
	"internal/sbxtest/sbxtest.go":      "writes a fixture program that is run at once, for tests",
	"internal/memory/memory.go":        "creates a marker to see whether the sandbox sees the folder: that it is created in place is the check",
}

func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no go.mod above the test's folder")
		}
		dir = parent
	}
}

// directAccess lists the files, from the module's root, that call os.ReadFile
// or os.WriteFile outside tests.
func directAccess(t *testing.T) map[string][]string {
	t.Helper()
	root := moduleRoot(t)
	found := map[string][]string{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case "node_modules", ".git", "vendor":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		ast.Inspect(file, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if pkg, ok := sel.X.(*ast.Ident); ok && pkg.Name == "os" && (sel.Sel.Name == "ReadFile" || sel.Sel.Name == "WriteFile") {
				found[rel] = append(found[rel], "os."+sel.Sel.Name)
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return found
}

func TestFilesAreReadAndWrittenWholeThroughFscopyNotDirectly(t *testing.T) {
	found := directAccess(t)
	var offenders []string
	for file, calls := range found {
		if file == "internal/fscopy/retry.go" || file == "internal/fscopy/fscopy.go" {
			continue
		}
		if _, allowed := directFileAccess[file]; !allowed {
			offenders = append(offenders, file+": "+strings.Join(calls, ", "))
		}
	}
	sort.Strings(offenders)
	if len(offenders) > 0 {
		t.Errorf("these use os.ReadFile or os.WriteFile: use fscopy.ReadFile and fscopy.WriteWholeMode (they wait out a file being replaced, and never leave one half-written), or, for a file that isn't vibe's to replace, give the reason in directFileAccess:\n  %s", strings.Join(offenders, "\n  "))
	}
}

func TestEveryAllowedDirectFileAccessIsStillUsed(t *testing.T) {
	found := directAccess(t)
	for file, reason := range directFileAccess {
		if len(found[file]) == 0 {
			t.Errorf("%s (%s) no longer reads or writes a file directly: take it off directFileAccess", file, reason)
		}
	}
}
