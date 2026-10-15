//go:build windows

package main

import (
	"path/filepath"
	"strings"
)

// onOneFilesystem says a and b are on one volume, so a rename between them
// moves nothing but a name: across volumes, it fails.
func onOneFilesystem(a, b string) bool {
	return strings.EqualFold(filepath.VolumeName(a), filepath.VolumeName(b))
}
