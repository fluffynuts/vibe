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
//
// The waits roughly double, from 2 ms to 640 ms, about 1.3 s in all: long enough
// for a virus scanner or an indexer that has the file open (some hold a new file
// for the better part of a second), short enough that a file that really can't be
// written (access denied is what Windows says for that too) is reported within
// about a second.
var inUseWaits = []time.Duration{
	2 * time.Millisecond, 5 * time.Millisecond, 10 * time.Millisecond, 20 * time.Millisecond, 40 * time.Millisecond,
	80 * time.Millisecond, 160 * time.Millisecond, 320 * time.Millisecond, 640 * time.Millisecond,
}

// longWait is how long a file is waited for before OnLongWait says so.
const longWait = 500 * time.Millisecond

// OnLongWait, when set, is told (once for each wait) that path has been in use
// for longer than longWait: so a pause that long isn't silent. It is called on
// the goroutine that waits, and should be set once, before anything runs.
var OnLongWait func(path string)

// What is done is a variable each, for the tests to stand in for.
var (
	inUse    = InUse
	pause    = time.Sleep
	rename   = os.Rename
	readFile = os.ReadFile
)

// retryInUse does op, which works on path, again after each wait in inUseWaits
// while it fails with a file being in use, and gives the last error if it still
// does.
func retryInUse(path string, op func() error) error {
	err := op()
	var waited time.Duration
	for _, wait := range inUseWaits {
		if err == nil || !inUse(err) {
			return err
		}
		if waited < longWait && waited+wait >= longWait && OnLongWait != nil {
			OnLongWait(path)
		}
		pause(wait)
		waited += wait
		err = op()
	}
	return err
}

// ReadFile is os.ReadFile for a file that is written whole (WriteWholeMode): it
// waits out the moment such a file is being replaced.
func ReadFile(path string) ([]byte, error) {
	var data []byte
	err := retryInUse(path, func() (err error) {
		data, err = readFile(path)
		return err
	})
	return data, err
}
