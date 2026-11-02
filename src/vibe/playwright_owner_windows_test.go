//go:build windows

package main

import "os"

// ownedBy1000: there are no uids to say.
func ownedBy1000(os.FileInfo) bool { return false }
