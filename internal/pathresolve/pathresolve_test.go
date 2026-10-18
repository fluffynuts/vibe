package pathresolve

import (
	"path/filepath"
	"runtime"
	"testing"
)

func TestDeriveName(t *testing.T) {
	cases := map[string]string{
		"/home/user/code/company/foo-browser": "foo-browser",
		"/home/user/code/Yumbi":               "yumbi",
		"/home/user/code/My Cool App!!":       "my-cool-app",
		"/home/user/code/---":                 "sandbox",
		"/home/user/code/a":                   "a0",
		"/home/user/code/9lives":              "9lives",
	}
	for path, want := range cases {
		if got := DeriveName(path); got != want {
			t.Errorf("DeriveName(%q) = %q, want %q", path, got, want)
		}
	}
}

func TestWithin(t *testing.T) {
	dir := filepath.Join(string(filepath.Separator)+"home", "u")
	for path, want := range map[string]bool{
		dir:                                true,
		filepath.Join(dir, "code"):         true,
		filepath.Join(dir, "..cache"):      true,
		filepath.Join(dir, "a", "..", "b"): true,
		filepath.Join(dir, ".."):           false,
		filepath.Join(dir, "..", "other"):  false,
		filepath.Join(string(filepath.Separator), "elsewhere"): false,
	} {
		if got := Within(dir, path); got != want {
			t.Errorf("Within(%q, %q) = %v, want %v", dir, path, got, want)
		}
	}
}

func TestTilde(t *testing.T) {
	home := filepath.Join(string(filepath.Separator), "home", "me")
	cases := map[string]string{
		home:                                "~",
		filepath.Join(home, "code", "vibe"): filepath.Join("~", "code", "vibe"),
		filepath.Join(home, "..cache"):      filepath.Join("~", "..cache"),
		filepath.Join(home+"x", "code"):     filepath.Join(home+"x", "code"),
		filepath.Join(string(filepath.Separator), "srv"): filepath.Join(string(filepath.Separator), "srv"),
	}
	for in, want := range cases {
		if got := tilde(in, home); got != want {
			t.Errorf("tilde(%q) = %q, want %q", in, got, want)
		}
	}
}

// Windows PowerShell passes "~\project" to vibe unexpanded, and vibe shows
// folders that way itself, so it has to expand both spellings.
func TestExpandHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	// A backslash is only a separator on Windows; elsewhere it is part of
	// the name, and stays that way.
	backslashed := filepath.Join(home, `code\project`)
	if runtime.GOOS == "windows" {
		backslashed = filepath.Join(home, "code", "project")
	}
	for _, tt := range []struct{ in, want string }{
		{"", ""},
		{"~", home},
		{"~/code/project", filepath.Join(home, "code", "project")},
		{`~\code\project`, backslashed},
		{"~other/project", "~other/project"},
		{"/abs/path", "/abs/path"},
	} {
		if got := ExpandHome(tt.in); got != tt.want {
			t.Errorf("ExpandHome(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
