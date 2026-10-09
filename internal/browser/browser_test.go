package browser

import (
	"errors"
	"testing"
)

func env(vars map[string]string) func(string) string {
	return func(k string) string { return vars[k] }
}

func found(string) (string, error)   { return "/usr/bin/xdg-open", nil }
func missing(string) (string, error) { return "", errors.New("not found") }

func TestCommandPerPlatform(t *testing.T) {
	const url = "http://localhost:5350/#t"
	cases := []struct {
		goos     string
		vars     map[string]string
		look     func(string) (string, error)
		wantName string
		wantOK   bool
	}{
		{"darwin", nil, missing, "open", true},
		{"windows", nil, missing, "rundll32", true},
		{"linux", map[string]string{"DISPLAY": ":0"}, found, "xdg-open", true},
		{"linux", map[string]string{"WAYLAND_DISPLAY": "wayland-0"}, found, "xdg-open", true},
		{"linux", nil, found, "", false},
		{"linux", map[string]string{"DISPLAY": ":0"}, missing, "", false},
		{"linux", map[string]string{"DISPLAY": ":0", "SSH_CONNECTION": "1.2.3.4 1 5.6.7.8 22"}, found, "", false},
		{"darwin", map[string]string{"SSH_TTY": "/dev/pts/1"}, found, "", false},
	}
	for _, c := range cases {
		name, args, ok := command(c.goos, env(c.vars), c.look, url)
		if ok != c.wantOK || name != c.wantName {
			t.Errorf("%s %v: got %q ok=%v, want %q ok=%v", c.goos, c.vars, name, ok, c.wantName, c.wantOK)
		}
		if ok && args[len(args)-1] != url {
			t.Errorf("%s: url is not the last argument: %v", c.goos, args)
		}
	}
}
