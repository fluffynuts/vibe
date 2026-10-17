package timezone

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// fakeSandbox puts an `sbx` on PATH whose exec runs the command here, as
// root would (sudo -n is dropped), with /etc standing for the returned
// directory. Like a real sbx can, it says something of its own on stderr.
func fakeSandbox(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the fake sbx is a shell script")
	}
	root := t.TempDir()
	bin := t.TempDir()
	script := "#!/bin/sh\n[ \"$1\" = exec ] || exit 1\nshift\n[ \"$1\" = -i ] && shift\nshift 2\n" +
		"[ \"$1\" = sudo ] && shift 2\n" +
		"echo 'A new version of sbx is available' >&2\n" +
		"for a; do shift; set -- \"$@\" \"$(printf %s \"$a\" | sed \"s#^/etc#" + root + "#\")\"; done\n" +
		"exec \"$@\"\n"
	if err := os.WriteFile(filepath.Join(bin, "sbx"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	return root
}

// The file is read, then written back with TZ in it: what sbx says on
// stderr as it reads must never end up in it, where every shell in the
// sandbox would run it.
func TestExportTZWritesBackOnlyWhatTheFileHeld(t *testing.T) {
	root := fakeSandbox(t)
	file := filepath.Join(root, "sandbox-persistent.sh")
	if err := os.WriteFile(file, []byte("export FOO=1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := exportTZ("box", "Africa/Johannesburg"); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(file)
	got := string(data)
	if strings.Contains(got, "new version") || !strings.Contains(got, "export FOO=1") || !strings.Contains(got, "Africa/Johannesburg") {
		t.Errorf("the file became:\n%s", got)
	}
}
