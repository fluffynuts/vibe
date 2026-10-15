//go:build !windows

package main

import "syscall"

// onOneFilesystem says a and b are on one filesystem, so a rename between
// them moves nothing but a name: across filesystems, it fails.
func onOneFilesystem(a, b string) bool {
	var sa, sb syscall.Stat_t
	if syscall.Stat(a, &sa) != nil || syscall.Stat(b, &sb) != nil {
		return false
	}
	return sa.Dev == sb.Dev
}
