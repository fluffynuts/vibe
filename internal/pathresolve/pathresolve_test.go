package pathresolve

import (
	"path/filepath"
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
