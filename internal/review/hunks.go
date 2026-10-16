package review

import (
	"strings"
	"unicode"

	"github.com/sergi/go-diff/diffmatchpatch"
)

// contextLines is how many unchanged lines a hunk shows around a change, as
// git's diff does.
const contextLines = 3

// hunksOf is the diff of old to now as hunks: changed lines with up to
// three unchanged around them, hunks closer than that joined. With
// ignoreSpace, lines differing only in whitespace count as the same (and
// show as they are now).
func hunksOf(old, now []byte, ignoreSpace bool) []Hunk {
	a, b := linesOf(old), linesOf(now)
	ops := lineOps(a, b, ignoreSpace)
	var hunks []Hunk
	for i := 0; i < len(ops); {
		if ops[i].kind == Context {
			i++
			continue
		}
		start := max(0, i-contextLines)
		end := i
		for end < len(ops) {
			if ops[end].kind != Context {
				end++
				continue
			}
			run := end
			for run < len(ops) && ops[run].kind == Context {
				run++
			}
			if run == len(ops) || run-end > 2*contextLines {
				end = min(len(ops), end+contextLines)
				break
			}
			end = run
		}
		hunks = append(hunks, hunkOf(ops[start:end]))
		i = end
	}
	return hunks
}

// op is one line of the diff: its kind, its numbers on each side, its text.
type op struct {
	kind     string
	old, new int
	text     string
}

func hunkOf(ops []op) Hunk {
	h := Hunk{}
	for _, o := range ops {
		if h.OldStart == 0 && o.old > 0 {
			h.OldStart = o.old
		}
		if h.NewStart == 0 && o.new > 0 {
			h.NewStart = o.new
		}
		h.Lines = append(h.Lines, Line{Kind: o.kind, Old: o.old, New: o.new, Text: o.text})
	}
	return h
}

// linesOf is text's lines (see splitLines); none for no text.
func linesOf(text []byte) []string {
	if len(text) == 0 {
		return nil
	}
	return splitLines(string(text))
}

// lineOps is the diff of lines a to b, line by line.
func lineOps(a, b []string, ignoreSpace bool) []op {
	key := func(lines []string) string {
		var sb strings.Builder
		for _, l := range lines {
			if ignoreSpace {
				l = strings.Map(func(r rune) rune {
					if unicode.IsSpace(r) {
						return -1
					}
					return r
				}, l)
			}
			sb.WriteString(l)
			sb.WriteByte('\n')
		}
		return sb.String()
	}
	dmp := diffmatchpatch.New()
	ca, cb, _ := dmp.DiffLinesToRunes(key(a), key(b))
	diffs := dmp.DiffMainRunes(ca, cb, false)
	var ops []op
	i, j := 0, 0
	for _, d := range diffs {
		n := len([]rune(d.Text))
		for k := 0; k < n; k++ {
			switch d.Type {
			case diffmatchpatch.DiffEqual:
				ops = append(ops, op{kind: Context, old: i + 1, new: j + 1, text: b[j]})
				i++
				j++
			case diffmatchpatch.DiffDelete:
				ops = append(ops, op{kind: Deletion, old: i + 1, text: a[i]})
				i++
			case diffmatchpatch.DiffInsert:
				ops = append(ops, op{kind: Addition, new: j + 1, text: b[j]})
				j++
			}
		}
	}
	return ops
}
