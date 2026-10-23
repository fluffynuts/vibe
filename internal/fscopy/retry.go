package fscopy

import (
	"os"
	"time"
)

// Windows won't replace a file someone has open, nor let it be opened for the
// moment it is being replaced, so a file written whole (WriteWholeMode) that a
// reader happens to be reading can't be replaced just then, and a reader that
// comes just then can't read it. Neither is anything wrong with the file, and
// the next moment it works: the way to deal with it, since Windows gives nothing
// to wait on, is to try again after a short wait, as for a request to a server
// that said "busy". It is bounded, and only for that one error: anything else, or
// a file that stays in use, is said as it was.
var inUseWaits = []time.Duration{2 * time.Millisecond, 5 * time.Millisecond, 10 * time.Millisecond, 20 * time.Millisecond, 40 * time.Millisecond, 80 * time.Millisecond}

// What is done is a variable each, for the tests to stand in for.
var (
	inUse    = InUse
	pause    = time.Sleep
	rename   = os.Rename
	readFile = os.ReadFile
)

// retryInUse does op, again after each wait in inUseWaits while it fails with a
// file being in use, and gives the last error if it still does.
func retryInUse(op func() error) error {
	err := op()
	for _, wait := range inUseWaits {
		if err == nil || !inUse(err) {
			return err
		}
		pause(wait)
		err = op()
	}
	return err
}

// ReadFile is os.ReadFile for a file that is written whole (WriteWholeMode): it
// waits out the moment such a file is being replaced.
func ReadFile(path string) ([]byte, error) {
	var data []byte
	err := retryInUse(func() (err error) {
		data, err = readFile(path)
		return err
	})
	return data, err
}
