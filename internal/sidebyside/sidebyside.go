// Package sidebyside renders the difference between two texts as two columns
// — the left text's lines beside the right's — showing only the sections
// that changed, with a few lines of context around each.
package sidebyside

import (
	"strings"
	"unicode/utf8"
)

// Context is how many unchanged lines are shown around each change.
const Context = 3

// maxCells caps the line-diff table (left lines × right lines). Past it the
// texts are too long to compare line by line here, and Render says so
// instead.
const maxCells = 4_000_000

const (
	red   = "\x1b[31m"
	green = "\x1b[32m"
	dim   = "\x1b[2m"
	reset = "\x1b[0m"
)

type op int

const (
	same op = iota
	removed
	added
)

type line struct {
	op          op
	left, right string
}

// Render returns left and right side by side in width columns: a header
// naming each side, then every changed section with Context lines around
// it. Lines only on the left are red and marked "<", lines only on the
// right green and marked ">", a changed line "≠" across one row, and
// unchanged ones dim; color is off when color is false.
func Render(leftName, rightName, left, right string, width int, color bool) string {
	if width < 40 {
		width = 40
	}
	col := (width - 3) / 2
	var b strings.Builder
	paint := func(c, s string) string {
		if !color || c == "" {
			return s
		}
		return c + s + reset
	}
	row := func(l, lc, sep, r, rc string) {
		b.WriteString(paint(lc, pad(fit(l, col), col)))
		b.WriteString(sep)
		b.WriteString(paint(rc, fit(r, col)))
		b.WriteString("\n")
	}

	row(leftName, "", " │ ", rightName, "")
	b.WriteString(strings.Repeat("─", col) + "─┼─" + strings.Repeat("─", col) + "\n")

	lines, ok := diff(split(left), split(right))
	if !ok {
		b.WriteString("(too long to show side by side)\n")
		return b.String()
	}
	hunks := hunksOf(lines)
	if len(hunks) == 0 {
		b.WriteString("(no differences)\n")
		return b.String()
	}
	for i, h := range hunks {
		if i > 0 {
			row("⋯", dim, "   ", "⋯", dim)
		}
		section := lines[h[0]:h[1]]
		for k := 0; k < len(section); {
			if section[k].op == same {
				row(section[k].left, dim, " │ ", section[k].right, dim)
				k++
				continue
			}
			// A run of changes: lines only on the left, then lines only on
			// the right. Each left line is shown beside its counterpart on
			// the right, so a changed line reads across one row.
			var gone, came []string
			for ; k < len(section) && section[k].op == removed; k++ {
				gone = append(gone, section[k].left)
			}
			for ; k < len(section) && section[k].op == added; k++ {
				came = append(came, section[k].right)
			}
			for n := 0; n < max(len(gone), len(came)); n++ {
				switch {
				case n < len(gone) && n < len(came):
					row(gone[n], red, " ≠ ", came[n], green)
				case n < len(gone):
					row(gone[n], red, " < ", "", "")
				default:
					row("", "", " > ", came[n], green)
				}
			}
		}
	}
	return b.String()
}

func split(s string) []string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.TrimSuffix(s, "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

// diff pairs up a and b line by line along their longest common
// subsequence. ok is false when the texts are too long to compare here.
func diff(a, b []string) (lines []line, ok bool) {
	n, m := len(a), len(b)
	if n*m > maxCells {
		return nil, false
	}
	// lcs[i][j] is the length of the longest common subsequence of a[i:]
	// and b[j:], in one flat slice.
	lcs := make([]int32, (n+1)*(m+1))
	at := func(i, j int) int { return i*(m+1) + j }
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if a[i] == b[j] {
				lcs[at(i, j)] = lcs[at(i+1, j+1)] + 1
			} else if lcs[at(i+1, j)] >= lcs[at(i, j+1)] {
				lcs[at(i, j)] = lcs[at(i+1, j)]
			} else {
				lcs[at(i, j)] = lcs[at(i, j+1)]
			}
		}
	}
	i, j := 0, 0
	for i < n && j < m {
		switch {
		case a[i] == b[j]:
			lines = append(lines, line{same, a[i], b[j]})
			i++
			j++
		case lcs[at(i+1, j)] >= lcs[at(i, j+1)]:
			lines = append(lines, line{op: removed, left: a[i]})
			i++
		default:
			lines = append(lines, line{op: added, right: b[j]})
			j++
		}
	}
	for ; i < n; i++ {
		lines = append(lines, line{op: removed, left: a[i]})
	}
	for ; j < m; j++ {
		lines = append(lines, line{op: added, right: b[j]})
	}
	return lines, true
}

// hunksOf returns [start, end) ranges of lines to show: every change, with
// Context unchanged lines either side, overlapping ranges joined.
func hunksOf(lines []line) [][2]int {
	var hunks [][2]int
	for i, l := range lines {
		if l.op == same {
			continue
		}
		start, end := max(0, i-Context), min(len(lines), i+Context+1)
		if n := len(hunks); n > 0 && start <= hunks[n-1][1] {
			hunks[n-1][1] = max(hunks[n-1][1], end)
		} else {
			hunks = append(hunks, [2]int{start, end})
		}
	}
	return hunks
}

// fit shortens s to at most width display columns, marking a cut with "…".
// Tabs become spaces, so columns line up.
func fit(s string, width int) string {
	s = strings.ReplaceAll(s, "\t", "    ")
	if utf8.RuneCountInString(s) <= width {
		return s
	}
	r := []rune(s)
	return string(r[:width-1]) + "…"
}

func pad(s string, width int) string {
	if n := utf8.RuneCountInString(s); n < width {
		return s + strings.Repeat(" ", width-n)
	}
	return s
}
