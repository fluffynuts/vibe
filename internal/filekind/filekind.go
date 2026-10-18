// Package filekind tells what sort of file some content is.
package filekind

import "bytes"

// IsBinary guesses as git does whether data is binary rather than text: a
// NUL byte in its first 8000.
func IsBinary(data []byte) bool {
	return bytes.IndexByte(data[:min(len(data), 8000)], 0) >= 0
}
