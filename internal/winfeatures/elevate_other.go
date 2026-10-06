//go:build !windows

package winfeatures

import "errors"

// Elevated is Windows-only: elsewhere there are no Windows features.
func Elevated() bool {
	return false
}

// RunElevated is Windows-only.
func RunElevated(file string, args []string) (int, error) {
	return 0, errors.ErrUnsupported
}
