// Package prompt provides small interactive terminal prompts for vibe's
// command line — an arrow-key list picker in the style of inquirer.
package prompt

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
	"golang.org/x/term"
)

// Select renders labels as an arrow-key-navigable list on rw — which must be
// both readable and writable and refer to a real terminal, since it is put
// into raw mode for the duration of the call — and returns the chosen
// index. def is the index highlighted first. Up/Down (and k/j) move the
// selection, enter confirms, q/Esc/Ctrl-C cancel (ok=false).
//
// The caller is expected to have already printed the question above the
// list; Select only draws the options and collapses them to the chosen
// answer once the user is done.
func Select(rw *os.File, labels []string, def int) (choice int, ok bool) {
	if len(labels) == 0 {
		return 0, false
	}
	fd := int(rw.Fd())
	state, err := term.MakeRaw(fd)
	if err != nil {
		return 0, false
	}
	defer term.Restore(fd, state)

	sel := def
	if sel < 0 || sel >= len(labels) {
		sel = 0
	}
	lines := len(labels) + 1
	draw(rw, labels, sel)

	buf := make([]byte, 1)
	for {
		n, err := rw.Read(buf)
		if err != nil || n == 0 {
			clear(rw, lines)
			return 0, false
		}
		switch buf[0] {
		case 3, 'q', 'Q': // Ctrl-C, q
			clear(rw, lines)
			return 0, false
		case '\r', '\n':
			clear(rw, lines)
			fmt.Fprintf(rw, "  \x1b[32m✔\x1b[0m %s\r\n", labels[sel])
			return sel, true
		case 'k':
			if sel > 0 {
				sel--
			}
		case 'j':
			if sel < len(labels)-1 {
				sel++
			}
		case 0x1b:
			switch readEscape(rw) {
			case escUp:
				if sel > 0 {
					sel--
				}
			case escDown:
				if sel < len(labels)-1 {
					sel++
				}
			case escBare:
				clear(rw, lines)
				return 0, false
			default:
				continue
			}
		default:
			continue
		}
		up(rw, lines)
		draw(rw, labels, sel)
	}
}

// MultiSelect renders labels as an arrow-key-navigable checkbox list on rw
// (same terminal requirements as Select). checked is the initial checked
// state, matched index-for-index with labels (a shorter or nil slice is
// treated as all-unchecked). Up/Down (and k/j) move the cursor, Space
// toggles the item under it, q/Esc/Ctrl-C cancel (ok=false).
//
// Enter does not answer straight away: it shows what is checked, one item
// per line, and asks whether to go ahead. Enter again (the default) returns
// the checked indices in ascending order; "n" goes back to the list with
// everything still checked, so a near-miss costs one keypress rather than
// the whole selection. A checklist's answer is a list, and the single line
// it used to collapse to was unreadable the moment more than one short
// label was checked.
func MultiSelect(rw *os.File, labels []string, checked []bool) (selected []int, ok bool) {
	if len(labels) == 0 {
		return nil, false
	}
	fd := int(rw.Fd())
	state, err := term.MakeRaw(fd)
	if err != nil {
		return nil, false
	}
	defer term.Restore(fd, state)

	marks := make([]bool, len(labels))
	copy(marks, checked)

	cursor := 0
	lines := len(labels) + 1
	drawChecklist(rw, labels, marks, cursor)

	buf := make([]byte, 1)
	for {
		n, err := rw.Read(buf)
		if err != nil || n == 0 {
			clear(rw, lines)
			return nil, false
		}
		switch buf[0] {
		case 3, 'q', 'Q': // Ctrl-C, q
			clear(rw, lines)
			return nil, false
		case '\r', '\n':
			clear(rw, lines)
			var idxs []int
			var picked []string
			for i, m := range marks {
				if m {
					idxs = append(idxs, i)
					picked = append(picked, labels[i])
				}
			}
			switch confirmSelection(rw, picked) {
			case confirmYes:
				writeSelectionRecord(rw, picked)
				return idxs, true
			case confirmCancel:
				return nil, false
			}
			// Back to the list, exactly as it was left — cursor included.
			drawChecklist(rw, labels, marks, cursor)
			continue
		case ' ':
			marks[cursor] = !marks[cursor]
		case 'k':
			if cursor > 0 {
				cursor--
			}
		case 'j':
			if cursor < len(labels)-1 {
				cursor++
			}
		case 0x1b:
			switch readEscape(rw) {
			case escUp:
				if cursor > 0 {
					cursor--
				}
			case escDown:
				if cursor < len(labels)-1 {
					cursor++
				}
			case escBare:
				clear(rw, lines)
				return nil, false
			default:
				continue
			}
		default:
			continue
		}
		up(rw, lines)
		drawChecklist(rw, labels, marks, cursor)
	}
}

// confirmAnswer is what the user did with a checklist's confirmation step.
type confirmAnswer int

const (
	confirmYes confirmAnswer = iota
	confirmBack
	confirmCancel
)

// noneSelected is how an empty selection is described, so "you checked
// nothing" is something the user confirms rather than something that
// happens silently on a mis-hit enter.
const noneSelected = "(nothing selected)"

// selectionBlock renders the confirmation a checklist shows once enter is
// pressed: the checked labels, one per line, then the question. It returns
// the text — raw mode, so every break is CRLF — together with the number of
// terminal lines it occupies, which is what clear needs to wipe it again.
// Both come from here so the two cannot drift apart.
func selectionBlock(picked []string) (text string, lines int) {
	var b strings.Builder
	b.WriteString("Confirm selection:\r\n")
	items := len(picked)
	if items == 0 {
		fmt.Fprintf(&b, "  %s\r\n", noneSelected)
		items = 1
	}
	for _, p := range picked {
		fmt.Fprintf(&b, "  - %s\r\n", p)
	}
	b.WriteString("\r\n")
	b.WriteString(questionLine)
	// Header, one line per item, a blank line, and the question — which is
	// left without a break, for the answer to land on.
	return b.String(), items + 3
}

// questionLine is the confirmation's last line, printed by selectionBlock
// and reprinted by confirmSelection when it has to ask again.
const questionLine = "Continue? [Y/n] "

// typedAnswerLimit caps how much of an answer is kept and echoed. The
// question sits on the last line of the block, and clear only knows how
// many lines that block is: let an answer run long enough to wrap and the
// wiping would miss a line. No answer this prompt accepts is near it.
const typedAnswerLimit = 16

// confirmSelection shows what is checked and asks whether to go ahead. It
// reads a whole line rather than a single keypress, which is what "[Y/n]"
// invites and, more to the point, is what consumes the enter that follows a
// typed "y": on a single-key read that enter would be left in the
// terminal's input queue for the caller's *next* question to read as a
// blank line, silently answering it with its default.
//
// Enter alone takes the default, yes; "n" goes back to the list with
// everything still checked; Esc and Ctrl-C abandon the prompt (ok=false
// from MultiSelect). Anything else is asked again rather than guessed at.
// It wipes its own block before returning, leaving the terminal where it
// found it.
func confirmSelection(rw *os.File, picked []string) confirmAnswer {
	text, lines := selectionBlock(picked)
	fmt.Fprint(rw, text)
	defer clear(rw, lines)

	var typed []byte
	buf := make([]byte, 1)
	for {
		n, err := rw.Read(buf)
		if err != nil || n == 0 {
			return confirmCancel
		}
		c := buf[0]
		switch {
		case c == 3: // Ctrl-C
			return confirmCancel
		case c == 0x1b:
			// An arrow key here is a stray keypress: only a bare Escape
			// cancels.
			if readEscape(rw) == escBare {
				return confirmCancel
			}
		case c == '\r' || c == '\n':
			switch strings.ToLower(strings.TrimSpace(string(typed))) {
			case "", "y", "yes":
				return confirmYes
			case "n", "no":
				return confirmBack
			}
			typed = typed[:0]
			fmt.Fprint(rw, "\r\x1b[2K"+questionLine)
		case c == 127 || c == 8: // backspace, delete
			if len(typed) > 0 {
				typed = typed[:len(typed)-1]
				fmt.Fprint(rw, "\b \b")
			}
		case c >= ' ' && c < 127 && len(typed) < typedAnswerLimit:
			typed = append(typed, c)
			// Raw mode means nothing is echoed for us.
			fmt.Fprintf(rw, "%c", c)
		}
	}
}

// writeSelectionRecord leaves the confirmed answer in the scrollback, one
// item per line, so what was chosen is still legible after the prompt has
// cleared itself away.
func writeSelectionRecord(w *os.File, picked []string) {
	if len(picked) == 0 {
		fmt.Fprintf(w, "  \x1b[32m✔\x1b[0m %s\r\n", noneSelected)
		return
	}
	fmt.Fprint(w, "  \x1b[32m✔\x1b[0m selected:\r\n")
	for _, p := range picked {
		fmt.Fprintf(w, "    - %s\r\n", p)
	}
}

// Reorder lets the user rearrange labels on rw (same terminal requirements
// as Select). Up/Down (and j/k) move the selection over the list without
// changing the order; Shift+Up/Shift+Down (and J/K, for a terminal that
// doesn't pass the shift modifier through) move the selected item,
// swapping it with its neighbor and following it. Enter confirms the final
// order, q/Esc/Ctrl-C cancel (ok=false).
func Reorder(rw *os.File, labels []string) (ordered []string, ok bool) {
	if len(labels) == 0 {
		return nil, false
	}
	fd := int(rw.Fd())
	state, err := term.MakeRaw(fd)
	if err != nil {
		return nil, false
	}
	defer term.Restore(fd, state)

	order := make([]string, len(labels))
	copy(order, labels)
	cursor := 0
	lines := len(order) + 1
	drawReorder(rw, order, cursor)

	selectUp := func() {
		if cursor > 0 {
			cursor--
		}
	}
	selectDown := func() {
		if cursor < len(order)-1 {
			cursor++
		}
	}
	moveUp := func() {
		if cursor > 0 {
			order[cursor-1], order[cursor] = order[cursor], order[cursor-1]
			cursor--
		}
	}
	moveDown := func() {
		if cursor < len(order)-1 {
			order[cursor+1], order[cursor] = order[cursor], order[cursor+1]
			cursor++
		}
	}

	buf := make([]byte, 1)
	for {
		n, err := rw.Read(buf)
		if err != nil || n == 0 {
			clear(rw, lines)
			return nil, false
		}
		switch buf[0] {
		case 3, 'q', 'Q': // Ctrl-C, q
			clear(rw, lines)
			return nil, false
		case '\r', '\n':
			clear(rw, lines)
			fmt.Fprintf(rw, "  \x1b[32m✔\x1b[0m %s\r\n", strings.Join(order, ", "))
			return order, true
		case 'k':
			selectUp()
		case 'j':
			selectDown()
		case 'K':
			moveUp()
		case 'J':
			moveDown()
		case 0x1b:
			switch readEscape(rw) {
			case escUp:
				selectUp()
			case escDown:
				selectDown()
			case escShiftUp:
				moveUp()
			case escShiftDown:
				moveDown()
			case escBare:
				clear(rw, lines)
				return nil, false
			default:
				continue
			}
		default:
			continue
		}
		up(rw, lines)
		drawReorder(rw, order, cursor)
	}
}

type escKey int

const (
	escOther escKey = iota
	escUp
	escDown
	escShiftUp
	escShiftDown
	escBare
)

// escapeWait is how long readEscape waits for each byte of the rest of an
// arrow-key sequence before giving up and treating the ESC as a bare
// Escape keypress. Real terminals emit the whole sequence in one burst, so
// this only ever matters for a genuine standalone Escape.
//
// os.File's own read deadline isn't used here: on at least one platform
// this was tested on, a raw-mode tty fd's Read never woke up on its
// deadline, hanging indefinitely on a lone Escape. Polling the fd directly
// sidesteps that.
const escapeWait = 30 // milliseconds

// readEscape reads what follows an already-consumed ESC byte: a bare
// Escape keypress, a plain arrow ("ESC [ A/B"), or an arrow held with a
// modifier key ("ESC [ 1 ; <modifier> A/B", xterm's scheme for Shift,
// Alt, Ctrl and combinations of them on a cursor key).
func readEscape(rw *os.File) escKey {
	b1, ok := pollByte(rw)
	if !ok || b1 != '[' {
		return escBare
	}
	var params []byte
	var final byte
	for {
		b, ok := pollByte(rw)
		if !ok {
			return escBare
		}
		if (b >= '0' && b <= '9') || b == ';' {
			params = append(params, b)
			continue
		}
		final = b
		break
	}
	shift := hasShiftModifier(params)
	switch final {
	case 'A':
		if shift {
			return escShiftUp
		}
		return escUp
	case 'B':
		if shift {
			return escShiftDown
		}
		return escDown
	default:
		return escOther
	}
}

// hasShiftModifier reports whether a CSI sequence's parameter bytes encode
// the Shift modifier, alone or combined with another (xterm's "1;<mod>"
// scheme, where <mod>-1 is a bitmask with bit 0 set for Shift).
func hasShiftModifier(params []byte) bool {
	parts := strings.Split(string(params), ";")
	if len(parts) < 2 {
		return false
	}
	mod, err := strconv.Atoi(parts[1])
	if err != nil || mod < 2 {
		return false
	}
	return (mod-1)&1 == 1
}

// pollByte waits up to escapeWait for rw to become readable and reads a
// single byte from it. ok is false on timeout or read error.
func pollByte(rw *os.File) (b byte, ok bool) {
	pfd := []unix.PollFd{{Fd: int32(rw.Fd()), Events: unix.POLLIN}}
	if n, err := unix.Poll(pfd, escapeWait); err != nil || n == 0 {
		return 0, false
	}
	buf := make([]byte, 1)
	if n, err := rw.Read(buf); err != nil || n == 0 {
		return 0, false
	}
	return buf[0], true
}

func draw(w *os.File, labels []string, sel int) {
	for i, label := range labels {
		fmt.Fprint(w, "\x1b[2K\r")
		if i == sel {
			fmt.Fprintf(w, "\x1b[36m❯ %s\x1b[0m\r\n", label)
		} else {
			fmt.Fprintf(w, "  %s\r\n", label)
		}
	}
	fmt.Fprint(w, "\x1b[2K\r\x1b[2m(↑/↓ to move, enter to select, q to quit)\x1b[0m")
}

func drawChecklist(w *os.File, labels []string, marks []bool, cursor int) {
	for i, label := range labels {
		fmt.Fprint(w, "\x1b[2K\r")
		box := "[ ]"
		if marks[i] {
			box = "[x]"
		}
		if i == cursor {
			fmt.Fprintf(w, "\x1b[36m❯ %s %s\x1b[0m\r\n", box, label)
		} else {
			fmt.Fprintf(w, "  %s %s\r\n", box, label)
		}
	}
	fmt.Fprint(w, "\x1b[2K\r\x1b[2m(↑/↓ to move, space to toggle, enter when done, q to quit)\x1b[0m")
}

func drawReorder(w *os.File, labels []string, cursor int) {
	for i, label := range labels {
		fmt.Fprint(w, "\x1b[2K\r")
		if i == cursor {
			fmt.Fprintf(w, "\x1b[36m❯ %s\x1b[0m\r\n", label)
		} else {
			fmt.Fprintf(w, "  %s\r\n", label)
		}
	}
	fmt.Fprint(w, "\x1b[2K\r\x1b[2m(↑/↓ to select, shift+↑/↓ to move the selected item, enter to confirm, q to quit)\x1b[0m")
}

// up moves the cursor back to the first drawn line, ready to redraw in
// place. lines is the total number of lines draw prints, including the
// trailing hint line.
func up(w *os.File, lines int) {
	fmt.Fprintf(w, "\r\x1b[%dA", lines-1)
}

// clear wipes every line draw printed and leaves the cursor at column 0 of
// what was the first line.
func clear(w *os.File, lines int) {
	fmt.Fprintf(w, "\r\x1b[%dA", lines-1)
	for i := 0; i < lines; i++ {
		fmt.Fprint(w, "\x1b[2K")
		if i < lines-1 {
			fmt.Fprint(w, "\r\n")
		}
	}
	fmt.Fprint(w, "\r")
}
