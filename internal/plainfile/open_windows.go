//go:build windows

package plainfile

import (
	"fmt"
	"os"
)

// open opens path to read, refusing a symbolic link there (Windows
// has no FIFOs a sandbox could leave): the caller then checks it's a plain
// file.
func open(path string) (*os.File, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("it's a link, which isn't followed")
	}
	afterLstat(path)
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	// What was checked is what was opened: a file swapped for a link in between would be followed.
	opened, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	if !os.SameFile(info, opened) {
		f.Close()
		return nil, fmt.Errorf("it changed while it was being opened, which isn't followed")
	}
	return f, nil
}

// afterLstat is what happens between checking the path and opening it: nothing, but for a test that swaps the file.
var afterLstat = func(string) {}
