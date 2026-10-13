package main

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func repoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	// cmd/vibe -> cmd -> repo root
	return filepath.Dir(filepath.Dir(wd))
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
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
		if got := expandHome(tt.in); got != tt.want {
			t.Errorf("expandHome(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
