package sbxfind

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

func touch(t *testing.T, path string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func onPath(p string) func(string) (string, error) {
	return func(string) (string, error) { return p, nil }
}

func notOnPath(string) (string, error) { return "", errors.New("not found") }

func TestFindPrefersTheSbxOnPath(t *testing.T) {
	dir := t.TempDir()
	onpath := touch(t, filepath.Join(dir, "a", "sbx"))
	known := touch(t, filepath.Join(dir, "b", "sbx"))

	f := Find(onPath(onpath), []string{known})

	if f.Path != onpath || !f.OnPath {
		t.Errorf("got %+v, want the sbx on PATH", f)
	}
	if len(f.Others) != 1 || f.Others[0] != known {
		t.Errorf("Others = %v, want [%s]", f.Others, known)
	}
}

func TestFindFallsBackToAKnownLocation(t *testing.T) {
	dir := t.TempDir()
	first := touch(t, filepath.Join(dir, "a", "sbx"))
	second := touch(t, filepath.Join(dir, "b", "sbx"))

	f := Find(notOnPath, []string{filepath.Join(dir, "missing", "sbx"), first, second})

	if f.Path != first || f.OnPath {
		t.Errorf("got %+v, want the first known sbx that exists, not on PATH", f)
	}
}

func TestFindFindsNothing(t *testing.T) {
	f := Find(notOnPath, []string{filepath.Join(t.TempDir(), "sbx")})
	if f.Path != "" {
		t.Errorf("found %q", f.Path)
	}
}

func TestFindDoesntCountALinkToTheSbxOnPathAsAnotherSbx(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("links need privileges on Windows")
	}
	dir := t.TempDir()
	real := touch(t, filepath.Join(dir, "app", "sbx"))
	link := filepath.Join(dir, "bin", "sbx")
	os.MkdirAll(filepath.Dir(link), 0o755)
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}

	f := Find(onPath(link), []string{real})

	if len(f.Others) != 0 {
		t.Errorf("Others = %v, want none: it's the same file", f.Others)
	}
}

// Each case is described as it would be on goos, whatever the tests run on:
// what to do about PATH is a shell's, and Windows' isn't bash.
func TestDescribe(t *testing.T) {
	cases := []struct {
		name string
		goos string
		f    Found
		want []string
	}{
		{"nothing found", "linux", Found{}, nil},
		{"on PATH alone", "linux", Found{Path: "/a/sbx", OnPath: true}, nil},
		{"not on PATH", "linux", Found{Path: "/a/bin/sbx"}, []string{"/a/bin/sbx", "isn't on your PATH", `export PATH="/a/bin:$PATH"`}},
		{"not on PATH, on Windows", "windows", Found{Path: `C:\a\bin\sbx.exe`}, []string{`C:\a\bin\sbx.exe`, "isn't on your PATH", `$env:Path = "C:\a\bin;$env:Path"`}},
		{"on PATH with another", "linux", Found{Path: "/a/sbx", OnPath: true, Others: []string{"/b/sbx"}}, []string{"/a/sbx", "/b/sbx", "check that the right sbx"}},
	}
	for _, c := range cases {
		got := c.f.describe(c.goos)
		if c.want == nil && got != "" {
			t.Errorf("%s: got %q, want nothing", c.name, got)
		}
		for _, w := range c.want {
			if !strings.Contains(got, w) {
				t.Errorf("%s: %q lacks %q", c.name, got, w)
			}
		}
	}
}

func TestKnownLocations(t *testing.T) {
	env := func(k string) string { return map[string]string{"LOCALAPPDATA": `L`, "ProgramFiles": `P`}[k] }
	if got := KnownLocations("linux", "/h", env); len(got) != 1 || got[0] != filepath.Join("/h", ".docker", "sbx", "bin", "sbx") {
		t.Errorf("linux: %v", got)
	}
	if got := KnownLocations("windows", "/h", env); len(got) != 2 || !strings.HasSuffix(got[0], "sbx.exe") || !strings.HasPrefix(got[0], "L") {
		t.Errorf("windows: %v", got)
	}
	if got := KnownLocations("darwin", "/h", env); len(got) != 4 {
		t.Errorf("darwin: %v", got)
	}
}

func TestLookWarnsOnlyOnce(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("PATH", t.TempDir())
	if runtime.GOOS == "windows" {
		t.Skip("the known locations on Windows aren't under HOME")
	}
	touch(t, filepath.Join(home, ".docker", "sbx", "bin", "sbx"))
	var warnings []string
	old := Warn
	Warn = func(m string) { warnings = append(warnings, m) }
	defer func() { Warn = old }()
	warnOnce = syncOnce()

	Look()
	Look()

	if len(warnings) != 1 {
		t.Errorf("warned %d times: %v", len(warnings), warnings)
	}
}

func syncOnce() (o sync.Once) { return }
