package companion

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// workspaceWith is a workspace folder holding files (name -> text), and a
// folder outside it with a secret.
func workspaceWith(t *testing.T, files map[string]string) (workspace, outside string) {
	t.Helper()
	base := t.TempDir()
	workspace, outside = filepath.Join(base, "work"), filepath.Join(base, "elsewhere")
	for _, dir := range []string{workspace, outside} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(outside, "secret.md"), []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	for name, text := range files {
		path := filepath.Join(workspace, filepath.FromSlash(name))
		os.MkdirAll(filepath.Dir(path), 0o755)
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return workspace, outside
}

func refusedAs(t *testing.T, err error, kind string) {
	t.Helper()
	var refused fileRefused
	if !errors.As(err, &refused) || refused.Kind != kind {
		t.Errorf("err = %v, want a refusal of kind %q", err, kind)
	}
}

func TestAFileInTheWorkspaceIsRead(t *testing.T) {
	ws, _ := workspaceWith(t, map[string]string{"doc/plans/a.md": "# Plan\n"})
	got, err := readWorkspaceFile(ws, "doc/plans/a.md")
	if err != nil || got.Text != "# Plan\n" || got.Path != "doc/plans/a.md" {
		t.Errorf("got %+v, %v", got, err)
	}
}

func TestAnAbsolutePathInTheWorkspaceIsTakenAsRelativeToIt(t *testing.T) {
	ws, _ := workspaceWith(t, map[string]string{"a.md": "x"})
	got, err := readWorkspaceFile(ws, filepath.Join(ws, "a.md"))
	if err != nil || got.Path != "a.md" {
		t.Errorf("got %+v, %v", got, err)
	}
}

func TestADotSlashPathIsTheSameFile(t *testing.T) {
	ws, _ := workspaceWith(t, map[string]string{"a.md": "x"})
	if got, err := readWorkspaceFile(ws, "./sub/../a.md"); err != nil || got.Path != "a.md" {
		t.Errorf("got %+v, %v", got, err)
	}
}

func TestPathsOutsideTheWorkspaceAreRefusedAsOutside(t *testing.T) {
	ws, outside := workspaceWith(t, nil)
	for _, asked := range []string{
		filepath.Join(outside, "secret.md"),
		"../elsewhere/secret.md",
		"doc/../../elsewhere/secret.md",
		"~/secret.md",
		"/etc/passwd",
	} {
		_, err := readWorkspaceFile(ws, asked)
		if asked == "/etc/passwd" && runtime.GOOS == "windows" {
			continue
		}
		refusedAs(t, err, "outside")
		if err != nil && strings.Contains(err.Error(), "secret") && !strings.Contains(err.Error(), asked) {
			t.Errorf("%s: the message says something other than the path asked for: %v", asked, err)
		}
	}
}

func TestASymlinkOutOfTheWorkspaceIsRefusedAsOutsideButOneWithinIsFollowed(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on Windows")
	}
	ws, outside := workspaceWith(t, map[string]string{"real.md": "inside"})
	os.Symlink(filepath.Join(outside, "secret.md"), filepath.Join(ws, "leak.md"))
	os.Symlink(filepath.Join(outside), filepath.Join(ws, "leakdir"))
	os.Symlink("real.md", filepath.Join(ws, "alias.md"))
	_, err := readWorkspaceFile(ws, "leak.md")
	refusedAs(t, err, "outside")
	_, err = readWorkspaceFile(ws, "leakdir/secret.md")
	refusedAs(t, err, "outside")
	if got, err := readWorkspaceFile(ws, "alias.md"); err != nil || got.Text != "inside" {
		t.Errorf("a link within the workspace: %+v, %v", got, err)
	}
}

func TestAWorkspaceThatIsItselfALinkIsStillTheWorkspace(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on Windows")
	}
	ws, _ := workspaceWith(t, map[string]string{"a.md": "x"})
	link := filepath.Join(t.TempDir(), "link")
	os.Symlink(ws, link)
	if got, err := readWorkspaceFile(link, filepath.Join(ws, "a.md")); err != nil || got.Path != "a.md" {
		t.Errorf("by its real path: %+v, %v", got, err)
	}
	if got, err := readWorkspaceFile(link, filepath.Join(link, "a.md")); err != nil || got.Path != "a.md" {
		t.Errorf("by the link: %+v, %v", got, err)
	}
}

func TestWhatIsNotThereOrNotAFileIsSaidSo(t *testing.T) {
	ws, _ := workspaceWith(t, map[string]string{"dir/a.md": "x"})
	_, err := readWorkspaceFile(ws, "nope.md")
	refusedAs(t, err, "missing")
	_, err = readWorkspaceFile(ws, "dir")
	refusedAs(t, err, "notFile")
	_, err = readWorkspaceFile(ws, "")
	refusedAs(t, err, "bad")
	_, err = readWorkspaceFile(ws, "a\x00b")
	refusedAs(t, err, "bad")
	_, err = readWorkspaceFile(ws, strings.Repeat("a/", 600))
	refusedAs(t, err, "bad")
}

func TestAFileTooBigOrNotTextIsRefused(t *testing.T) {
	ws, _ := workspaceWith(t, map[string]string{
		"big.md":    strings.Repeat("a", maxViewedFile+1),
		"exact.md":  strings.Repeat("a", maxViewedFile),
		"bin.dat":   "a\x00b",
		"latin1.md": "caf\xe9",
	})
	_, err := readWorkspaceFile(ws, "big.md")
	refusedAs(t, err, "tooBig")
	if got, err := readWorkspaceFile(ws, "exact.md"); err != nil || len(got.Text) != maxViewedFile {
		t.Errorf("a file of exactly the most: %v", err)
	}
	_, err = readWorkspaceFile(ws, "bin.dat")
	refusedAs(t, err, "notText")
	_, err = readWorkspaceFile(ws, "latin1.md")
	refusedAs(t, err, "notText")
}

func viewServer(t *testing.T, ws string) *Server {
	s := newServer(&fakeSbx{})
	s.cfg.Workspace = ws
	return s
}

func TestTheFileCallNeedsTheTokenAndAWorkspace(t *testing.T) {
	ws, _ := workspaceWith(t, map[string]string{"a.md": "x"})
	if rec := do(t, viewServer(t, ws), req{path: "/api/workspace-file?path=a.md"}); rec.Code != http.StatusUnauthorized {
		t.Errorf("no token: %d %s", rec.Code, rec.Body)
	}
	if rec := do(t, newServer(&fakeSbx{}), req{path: "/api/workspace-file?path=a.md", headers: authed(nil)}); rec.Code != http.StatusNotFound {
		t.Errorf("no workspace: %d %s", rec.Code, rec.Body)
	}
}

func TestTheFileCallGivesTheTextAndSaysWhyNotOtherwise(t *testing.T) {
	ws, _ := workspaceWith(t, map[string]string{"a.md": "hello"})
	s := viewServer(t, ws)
	rec := do(t, s, req{path: "/api/workspace-file?path=a.md", headers: authed(nil)})
	var ok struct{ Path, Text string }
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &ok) != nil || ok.Text != "hello" || ok.Path != "a.md" {
		t.Errorf("%d %s", rec.Code, rec.Body)
	}
	for path, want := range map[string]struct {
		status int
		code   string
	}{"../x": {http.StatusForbidden, "outside"}, "nope": {http.StatusNotFound, "missing"}, "": {http.StatusBadRequest, "bad"}} {
		rec := do(t, s, req{path: "/api/workspace-file?path=" + path, headers: authed(nil)})
		var bad struct{ Error, Code string }
		json.Unmarshal(rec.Body.Bytes(), &bad)
		if rec.Code != want.status || bad.Code != want.code || bad.Error == "" {
			t.Errorf("%q: %d %s", path, rec.Code, rec.Body)
		}
	}
}

func TestTheStateSaysWhetherFilesCanBeViewed(t *testing.T) {
	ws, _ := workspaceWith(t, nil)
	for _, c := range []struct {
		s    *Server
		want bool
	}{{viewServer(t, ws), true}, {newServer(&fakeSbx{}), false}} {
		rec := do(t, c.s, req{path: "/api/state", headers: authed(nil)})
		var st map[string]interface{}
		json.Unmarshal(rec.Body.Bytes(), &st)
		if st["viewFiles"] != c.want {
			t.Errorf("viewFiles = %v, want %v", st["viewFiles"], c.want)
		}
	}
}

func TestWhichOfTheAskedPathsAreFilesToShowIsSaidWithoutTheirText(t *testing.T) {
	ws, outside := workspaceWith(t, map[string]string{"doc/a.md": "x", "bin.dat": "a\x00b", "dir/inner.md": "y"})
	got := viewableFiles(ws, []string{
		"doc/a.md", "./doc/a.md", "dir", "nope.md", "bin.dat", "safe.directory",
		filepath.Join(ws, "dir", "inner.md"), filepath.Join(outside, "secret.md"), "../elsewhere/secret.md", "~/x.md", "",
	})
	want := []string{"doc/a.md", "./doc/a.md", filepath.Join(ws, "dir", "inner.md")}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestAskingAboutManyPathsIsBounded(t *testing.T) {
	ws, _ := workspaceWith(t, map[string]string{"a.md": "x"})
	asked := make([]string, maxCheckedPaths+10)
	for i := range asked {
		asked[i] = "a.md"
	}
	if got := viewableFiles(ws, asked); len(got) != maxCheckedPaths {
		t.Errorf("%d answered, want only the first %d", len(got), maxCheckedPaths)
	}
}

func TestTheCheckCallNeedsTheTokenAndAWorkspaceAndAnswersForTheAskedPaths(t *testing.T) {
	ws, _ := workspaceWith(t, map[string]string{"a.md": "x"})
	if rec := do(t, viewServer(t, ws), req{path: "/api/workspace-files?path=a.md"}); rec.Code != http.StatusUnauthorized {
		t.Errorf("no token: %d", rec.Code)
	}
	if rec := do(t, newServer(&fakeSbx{}), req{path: "/api/workspace-files?path=a.md", headers: authed(nil)}); rec.Code != http.StatusNotFound {
		t.Errorf("no workspace: %d", rec.Code)
	}
	rec := do(t, viewServer(t, ws), req{path: "/api/workspace-files?path=a.md&path=b.md&path=..%2Fx", headers: authed(nil)})
	var got struct{ Files []string }
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &got) != nil || strings.Join(got.Files, "|") != "a.md" {
		t.Errorf("%d %s", rec.Code, rec.Body)
	}
}
