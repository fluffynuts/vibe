//go:build !windows

package sbxinstall

import "os"

// PersistentPath is the PATH a terminal opened now would start with —
// outside Windows, there's no telling from here what a shell's profile will
// set, so it's this process's own.
func PersistentPath() string {
	return os.Getenv("PATH")
}
