// Package sbxtest stands in for sbx in tests.
package sbxtest

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// OnPath puts an sbx on PATH, ahead of any other, for the rest of the test:
// a shell script whose body is script. It returns the folder the script is
// in, for anything else the script reads or writes. A shell script can't
// stand in on Windows, so there the test is skipped.
func OnPath(t testing.TB, script string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in sbx is a shell script")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "sbx"), []byte("#!/bin/sh\n"+script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return dir
}
