package pathresolve

import "testing"

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
