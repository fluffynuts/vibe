//go:build !windows

package sbxinstall

import (
	"errors"
	"os"
)

// PersistentPath is the PATH a terminal opened now would start with —
// outside Windows, there's no telling from here what a shell's profile will
// set, so it's this process's own.
func PersistentPath() string {
	return os.Getenv("PATH")
}

// AddToUserPath is Windows-only: elsewhere, PATH is set by whichever shell
// profile the user keeps, and there's no one right file to add it to.
func AddToUserPath(dir string) (added bool, err error) {
	return false, errors.ErrUnsupported
}
