//go:build !windows

package main

import (
	"os"
	"syscall"
)

// ownedBy1000 says info is of something the sandbox's agent user (uid 1000) owns.
func ownedBy1000(info os.FileInfo) bool {
	st, ok := info.Sys().(*syscall.Stat_t)
	return ok && st.Uid == 1000
}
