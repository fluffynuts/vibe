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
	return os.Open(path)
}
