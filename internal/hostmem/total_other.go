//go:build !linux && !darwin && !windows

package hostmem

import (
	"errors"
	"runtime"
)

func total() (uint64, error) {
	return 0, errors.New("can't read the host's memory on " + runtime.GOOS)
}
