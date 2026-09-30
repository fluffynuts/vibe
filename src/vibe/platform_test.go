package main

import (
	"os"
	"strings"
	"testing"
)

func TestOnPath(t *testing.T) {
	tests := []struct {
		name, goos, pathList, dir string
		want                      bool
	}{
		{"unix match", "linux", "/usr/bin:/home/me/.local/bin", "/home/me/.local/bin", true},
		{"unix trailing slash", "linux", "/home/me/.local/bin/:/usr/bin", "/home/me/.local/bin", true},
		{"unix is case-sensitive", "linux", "/Home/Me/.local/bin", "/home/me/.local/bin", false},
		{"unix missing", "darwin", "/usr/bin:/bin", "/home/me/.local/bin", false},
		{"empty entries ignored", "linux", "::/usr/bin", "/home/me/.local/bin", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Written with ':' for readability; PATH uses this platform's
			// separator.
			pathList := strings.ReplaceAll(tt.pathList, ":", string(os.PathListSeparator))
			if got := onPath(tt.goos, pathList, tt.dir); got != tt.want {
				t.Errorf("onPath(%q, %q, %q) = %v, want %v", tt.goos, tt.pathList, tt.dir, got, tt.want)
			}
		})
	}
}

// The case-insensitive comparison only ever applies on Windows; this runs
// the check with a "windows" goos against paths this platform can clean.
func TestOnPathIgnoresCaseOnWindows(t *testing.T) {
	if !onPath("windows", "/Users/Me/.local/bin", "/users/me/.local/bin") {
		t.Error("a Windows PATH entry differing only in case should match")
	}
}

func TestNetstatListenersPicksTheListeningPort(t *testing.T) {
	out := "\r\n" +
		"Active Connections\r\n" +
		"\r\n" +
		"  Proto  Local Address          Foreign Address        State           PID\r\n" +
		"  TCP    0.0.0.0:5391           0.0.0.0:0              LISTENING       4242\r\n" +
		"  TCP    0.0.0.0:53910          0.0.0.0:0              LISTENING       1111\r\n" +
		"  TCP    127.0.0.1:60000        127.0.0.1:5391         ESTABLISHED     2222\r\n" +
		"  TCP    [::]:5391              [::]:0                 LISTENING       4242\r\n"
	got := netstatListeners(out, 5391)
	lines := strings.Split(strings.TrimSpace(got), "\n")
	if len(lines) != 2 {
		t.Fatalf("netstatListeners = %q, want the two listening :5391 lines", got)
	}
	for _, line := range lines {
		if !strings.HasSuffix(line, "4242") || strings.Contains(line, "\r") {
			t.Errorf("unexpected line %q", line)
		}
	}
}
