package filekind

import (
	"strings"
	"testing"
)

func TestIsBinaryLooksForANulInTheFirst8000Bytes(t *testing.T) {
	for name, c := range map[string]struct {
		data string
		want bool
	}{
		"text":            {"hello\nworld\n", false},
		"empty":           {"", false},
		"a NUL":           {"PNG\x00\x01", true},
		"a NUL past 8000": {strings.Repeat("a", 8000) + "\x00", false},
	} {
		if got := IsBinary([]byte(c.data)); got != c.want {
			t.Errorf("%s: %v, want %v", name, got, c.want)
		}
	}
}
