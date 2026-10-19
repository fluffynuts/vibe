package prompt

import (
	"fmt"
	"strings"
)

// filterRows is how many of a Filter's matches show at once; the rest
// scroll into view as the cursor reaches them.
const filterRows = 10

// Filter is Select for a list too long to show whole: typing narrows the
// list to the labels containing what's typed (ignoring case), backspace
// widens it again, Up/Down move through what matches, enter picks the
// highlighted one, and Esc/Ctrl-C cancel (ok=false). Letters are typed
// rather than taken as commands, so j, k and q are not keys here. def is
// the index highlighted first.
func Filter(rw *Terminal, labels []string, def int) (choice int, ok bool) {
	if len(labels) == 0 {
		return 0, false
	}
	f, err := openFrame(rw)
	if err != nil {
		return 0, false
	}
	defer f.close()

	if def < 0 || def >= len(labels) {
		def = 0
	}
	typed := ""
	matches := filterMatches(labels, typed)
	cursor := def
	top := scrollTop(cursor, len(matches))
	f.draw(filterLines(labels, matches, cursor, top, typed))

	for {
		key, ok := rw.readByte()
		if !ok {
			f.clear()
			return 0, false
		}
		refilter := false
		switch {
		case key == 3: // Ctrl-C
			f.clear()
			return 0, false
		case key == '\r' || key == '\n':
			if len(matches) == 0 {
				continue
			}
			f.clear()
			picked := matches[cursor]
			fmt.Fprintf(rw, "  \x1b[32m✔\x1b[0m %s\r\n", labels[picked])
			return picked, true
		case key == 0x7f || key == 0x08: // backspace
			if typed == "" {
				continue
			}
			typed = typed[:len(typed)-1]
			refilter = true
		case key == 0x15: // Ctrl-U
			typed = ""
			refilter = true
		case key == 0x1b:
			switch readEscape(rw) {
			case escUp:
				if cursor > 0 {
					cursor--
				}
			case escDown:
				if cursor < len(matches)-1 {
					cursor++
				}
			case escBare:
				f.clear()
				return 0, false
			default:
				continue
			}
		case key >= 0x20 && key < 0x7f:
			typed += string(key)
			refilter = true
		default:
			continue
		}
		if refilter {
			// Stay on the label that was highlighted while it still
			// matches; otherwise start from the best match.
			was := -1
			if cursor < len(matches) {
				was = matches[cursor]
			}
			matches = filterMatches(labels, typed)
			cursor = 0
			for i, m := range matches {
				if m == was {
					cursor = i
				}
			}
		}
		top = scrollTop(cursor, len(matches))
		f.draw(filterLines(labels, matches, cursor, top, typed))
	}
}

// filterMatches returns the indices of the labels containing typed,
// ignoring case: all of them when nothing is typed.
func filterMatches(labels []string, typed string) []int {
	needle := strings.ToLower(typed)
	var out []int
	for i, label := range labels {
		if strings.Contains(strings.ToLower(label), needle) {
			out = append(out, i)
		}
	}
	return out
}

// scrollTop returns the first of n matches to show with the cursor in the
// middle of the filterRows on screen, so the list is seen to scroll under it.
// Near either end it can't be centred (there's nothing to show above the
// first, or below the last), and the highlight moves toward the edge instead:
// the hint that there's no more.
func scrollTop(cursor, n int) int {
	return max(0, min(cursor-filterRows/2, n-filterRows))
}

// windowLines is the filterRows of labels on screen from top, the one at
// cursor highlighted, with how many more there are above and below.
func windowLines(labels []string, cursor, top int) []string {
	var lines []string
	end := min(top+filterRows, len(labels))
	if top > 0 {
		lines = append(lines, fmt.Sprintf("\x1b[2m  ↑ %d more\x1b[0m", top))
	}
	list := listLines(labels[top:end], cursor-top, noPrefix, "")
	lines = append(lines, list[:len(list)-1]...)
	if end < len(labels) {
		lines = append(lines, fmt.Sprintf("\x1b[2m  ↓ %d more\x1b[0m", len(labels)-end))
	}
	return lines
}

// filterLines is a Filter's display: what's been typed, the matches on
// screen with how many more there are either side, then the keys.
func filterLines(labels []string, matches []int, cursor, top int, typed string) []string {
	lines := []string{"filter: " + typed}
	if len(matches) == 0 {
		lines = append(lines, "\x1b[2m  (no matches)\x1b[0m")
	}
	shown := make([]string, 0, len(matches))
	for _, m := range matches {
		shown = append(shown, labels[m])
	}
	if len(matches) > 0 {
		lines = append(lines, windowLines(shown, cursor, top)...)
	}
	return append(lines, "\x1b[2m(type to filter, ↑/↓ to move, enter to select, esc to quit)\x1b[0m")
}
