package prompt

import "fmt"

// MultiFilter is Filter for ticking several: typing narrows the list as it
// does there, Tab ticks the highlighted one (and moves down), Ctrl-A ticks
// everything shown (or unticks it, when it was all ticked), enter answers
// with every ticked one — shown or not — in ascending order. Esc clears what
// is typed, when anything is; with nothing typed it cancels (ok=false), as
// Ctrl-C ends vibe. Space is typed, as in Filter, so it is not the tick key.
// Nothing is ticked to begin with, and ticking nothing is an answer.
func MultiFilter(rw *Terminal, labels []string) (selected []int, ok bool) {
	if len(labels) == 0 {
		return nil, false
	}
	f, err := openFrame(rw)
	if err != nil {
		return nil, false
	}
	defer f.close()

	m := newMultiFilterState(labels)
	f.draw(m.lines())
	for {
		key, ok := rw.readByte()
		if !ok {
			f.clear()
			return nil, false
		}
		switch {
		case key == 3: // Ctrl-C, on a Terminal with no onInterrupt
			f.clear()
			return nil, false
		case key == '\r' || key == '\n':
			f.clear()
			picked := m.ticked()
			for _, i := range picked {
				fmt.Fprintf(rw, "  \x1b[32m✔\x1b[0m %s\r\n", labels[i])
			}
			return picked, true
		case key == 0x7f || key == 0x08:
			m.backspace()
		case key == 0x15:
			m.clearTyped()
		case key == '\t':
			m.tickHighlighted()
		case key == 0x01:
			m.tickShown()
		case key == 0x1b:
			switch readEscape(rw) {
			case escUp:
				m.move(-1)
			case escDown:
				m.move(1)
			case escBare:
				if !m.clearTyped() {
					f.clear()
					return nil, false
				}
			default:
				continue
			}
		case key >= 0x20 && key < 0x7f:
			m.typeKey(key)
		default:
			continue
		}
		f.draw(m.lines())
	}
}

// multiFilterState is what a MultiFilter has: kept apart from the terminal
// so it can be driven by tests.
type multiFilterState struct {
	labels  []string
	marks   []bool
	typed   string
	matches []int
	cursor  int
}

func newMultiFilterState(labels []string) *multiFilterState {
	m := &multiFilterState{labels: labels, marks: make([]bool, len(labels))}
	m.matches = filterMatches(labels, "")
	return m
}

func (m *multiFilterState) typeKey(key byte) {
	m.typed += string(key)
	m.refilter()
}

func (m *multiFilterState) backspace() {
	if m.typed == "" {
		return
	}
	m.typed = m.typed[:len(m.typed)-1]
	m.refilter()
}

// clearTyped says whether there was anything typed to clear.
func (m *multiFilterState) clearTyped() bool {
	if m.typed == "" {
		return false
	}
	m.typed = ""
	m.refilter()
	return true
}

// refilter keeps the highlight on the label that had it while it still
// matches; otherwise it starts from the first match.
func (m *multiFilterState) refilter() {
	was := -1
	if m.cursor < len(m.matches) {
		was = m.matches[m.cursor]
	}
	m.matches = filterMatches(m.labels, m.typed)
	m.cursor = 0
	for i, match := range m.matches {
		if match == was {
			m.cursor = i
		}
	}
}

func (m *multiFilterState) move(by int) {
	if next := m.cursor + by; next >= 0 && next < len(m.matches) {
		m.cursor = next
	}
}

func (m *multiFilterState) tickHighlighted() {
	if len(m.matches) == 0 {
		return
	}
	i := m.matches[m.cursor]
	m.marks[i] = !m.marks[i]
	m.move(1)
}

// tickShown ticks everything the filter shows, or unticks it when it was all
// ticked already.
func (m *multiFilterState) tickShown() {
	all := true
	for _, i := range m.matches {
		all = all && m.marks[i]
	}
	for _, i := range m.matches {
		m.marks[i] = !all
	}
}

func (m *multiFilterState) ticked() []int {
	var out []int
	for i, on := range m.marks {
		if on {
			out = append(out, i)
		}
	}
	return out
}

func (m *multiFilterState) lines() []string {
	shown := make([]string, len(m.matches))
	for i, match := range m.matches {
		box := "[ ] "
		if m.marks[match] {
			box = "[x] "
		}
		shown[i] = box + m.labels[match]
	}
	lines := []string{fmt.Sprintf("filter: %s    (%d ticked)", m.typed, len(m.ticked()))}
	if len(m.matches) == 0 {
		lines = append(lines, "\x1b[2m  (no matches)\x1b[0m")
	} else {
		lines = append(lines, windowLines(shown, m.cursor, scrollTop(m.cursor, len(shown)))...)
	}
	escDoes := "quit"
	if m.typed != "" {
		escDoes = "clear the filter"
	}
	hint := "(type to filter, ↑/↓ to move, tab to tick, ctrl-a to tick all shown, enter when done, esc to " + escDoes + ")"
	return append(lines, "\x1b[2m"+hint+"\x1b[0m")
}
