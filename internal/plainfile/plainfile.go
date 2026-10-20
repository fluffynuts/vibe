// Package plainfile reads files a sandbox may have written: what the agent
// drops in a folder shared with the host is the agent's to make, so it's
// read only as a plain file, up to a limit.
package plainfile

import (
	"fmt"
	"io"
)

// Read reads the plain file at path, up to limit bytes: not a link there,
// nor anything that isn't a file (a FIFO would wait forever).
func Read(path string, limit int64) ([]byte, error) {
	f, err := open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("it isn't a plain file")
	}
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, TooLarge{limit}
	}
	return data, nil
}

// TooLarge is a file over the limit it's read up to.
type TooLarge struct{ Limit int64 }

func (e TooLarge) Error() string { return fmt.Sprintf("it's too large (over %d bytes)", e.Limit) }
