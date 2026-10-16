package review

import (
	"strings"
	"unicode"
)

// Relocate finds where code (a comment's lines, as they read when it was
// made) is in file (its lines) now, near line (where the comment is): the
// same text, else the same text but for spacing; of several copies, the
// one nearest line (the earlier, when two are as near). ok is false when
// it isn't there at all, or there's nothing to look for.
func Relocate(code string, file []string, line int) (at int, ok bool) {
	if code == "" {
		return 0, false
	}
	want := strings.Split(code, "\n")
	for _, same := range []func(a, b string) bool{
		func(a, b string) bool { return a == b },
		func(a, b string) bool { return unspaced(a) == unspaced(b) },
	} {
		best, found := 0, false
		for i := 0; i+len(want) <= len(file); i++ {
			match := true
			for k, w := range want {
				if !same(file[i+k], w) {
					match = false
					break
				}
			}
			if match && (!found || distance(i+1, line) < distance(best, line)) {
				best, found = i+1, true
			}
		}
		if found {
			return best, true
		}
	}
	return 0, false
}

func unspaced(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, s)
}

func distance(a, b int) int {
	if a > b {
		return a - b
	}
	return b - a
}
