// Package prompt provides small interactive terminal prompts for vibe's
// command line — an arrow-key list picker in the style of inquirer.
package prompt

import (
	"fmt"
	"io"
	"strconv"
	"strings"
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
func Select(rw *Terminal, labels []string, def int) (choice int, ok bool) {
	if len(labels) == 0 {
		return 0, false
	}
	f, err := openFrame(rw)
	if err != nil {
		return 0, false
	}
	defer f.close()

	sel := def
	if sel < 0 || sel >= len(labels) {
		sel = 0
	}
	f.draw(selectLines(labels, sel))

	for {
		key, ok := rw.readByte()
		if !ok {
			f.clear()
			return 0, false
		}
		switch key {
		case 3, 'q', 'Q': // Ctrl-C, q
			f.clear()
			return 0, false
		case '\r', '\n':
			f.clear()
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
				f.clear()
				return 0, false
			default:
				continue
			}
		default:
			continue
		}
		f.draw(selectLines(labels, sel))
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
func MultiSelect(rw *Terminal, labels []string, checked []bool) (selected []int, ok bool) {
	return multiSelect(rw, labels, checked, true)
}

// MultiSelectNoConfirm is MultiSelect without the confirmation step: enter
// answers straight away. It suits a checklist whose answer is acted on at
// once and whose items say plainly what each one does — a list of upgrades
// to install, say — where asking again only costs the user a keypress.
func MultiSelectNoConfirm(rw *Terminal, labels []string, checked []bool) (selected []int, ok bool) {
	return multiSelect(rw, labels, checked, false)
}

func multiSelect(rw *Terminal, labels []string, checked []bool, confirm bool) (selected []int, ok bool) {
	if len(labels) == 0 {
		return nil, false
	}
	f, err := openFrame(rw)
	if err != nil {
		return nil, false
	}
	defer f.close()

	marks := make([]bool, len(labels))
	copy(marks, checked)

	cursor := 0
	f.draw(checklistLines(labels, marks, cursor))

	for {
		key, ok := rw.readByte()
		if !ok {
			f.clear()
			return nil, false
		}
		switch key {
		case 3, 'q', 'Q': // Ctrl-C, q
			f.clear()
			return nil, false
		case '\r', '\n':
			var idxs []int
			var picked []string
			for i, m := range marks {
				if m {
					idxs = append(idxs, i)
					picked = append(picked, labels[i])
				}
			}
			if !confirm {
				f.clear()
				writeSelectionRecord(rw, picked)
				return idxs, true
			}
			switch confirmSelection(f, rw, picked) {
			case confirmYes:
				writeSelectionRecord(rw, picked)
				return idxs, true
			case confirmCancel:
				return nil, false
			}
			// Back to the list, exactly as it was left — cursor included.
			f.draw(checklistLines(labels, marks, cursor))
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
				f.clear()
				return nil, false
			default:
				continue
			}
		default:
			continue
		}
		f.draw(checklistLines(labels, marks, cursor))
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

// selectionLines is the confirmation a checklist shows once enter is
// pressed: the checked labels, one per line, a blank line, then the
// question with whatever has been typed in answer so far.
func selectionLines(picked []string, typed string) []string {
	lines := []string{"Confirm selection:"}
	if len(picked) == 0 {
		lines = append(lines, "  "+noneSelected)
	}
	for _, p := range picked {
		lines = append(lines, "  - "+p)
	}
	return append(lines, "", questionLine+typed)
}

// questionLine is the confirmation's last line.
const questionLine = "Continue? [Y/n] "

// typedAnswerLimit caps how much of an answer is kept and echoed. No answer
// this prompt accepts is near it.
const typedAnswerLimit = 16

// confirmSelection shows what is checked and asks whether to go ahead,
// drawing in place of the checklist in f. It reads a whole line rather than
// a single keypress, which is what "[Y/n]" invites and, more to the point,
// is what consumes the enter that follows a typed "y": on a single-key read
// that enter would be left in the terminal's input queue for the caller's
// *next* question to read as a blank line, silently answering it with its
// default.
//
// Enter alone takes the default, yes; "n" goes back to the list with
// everything still checked; Esc and Ctrl-C abandon the prompt (ok=false
// from MultiSelect). Anything else is asked again rather than guessed at.
// It wipes what it drew before returning.
func confirmSelection(f *frame, rw *Terminal, picked []string) confirmAnswer {
	var typed []byte
	f.draw(selectionLines(picked, ""))
	defer f.clear()

	for {
		key, ok := rw.readByte()
		if !ok {
			return confirmCancel
		}
		c := key
		switch {
		case c == 3: // Ctrl-C
			return confirmCancel
		case c == 0x1b:
			// An arrow key here is a stray keypress: only a bare Escape
			// cancels.
			if readEscape(rw) == escBare {
				return confirmCancel
			}
			continue
		case c == '\r' || c == '\n':
			switch strings.ToLower(strings.TrimSpace(string(typed))) {
			case "", "y", "yes":
				return confirmYes
			case "n", "no":
				return confirmBack
			}
			typed = typed[:0]
		case c == 127 || c == 8: // backspace, delete
			if len(typed) == 0 {
				continue
			}
			typed = typed[:len(typed)-1]
		case c >= ' ' && c < 127 && len(typed) < typedAnswerLimit:
			// Raw mode means nothing is echoed for us: the redraw does it.
			typed = append(typed, c)
		default:
			continue
		}
		f.draw(selectionLines(picked, string(typed)))
	}
}

// writeSelectionRecord leaves the confirmed answer in the scrollback, one
// item per line, so what was chosen is still legible after the prompt has
// cleared itself away.
func writeSelectionRecord(w io.Writer, picked []string) {
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
func Reorder(rw *Terminal, labels []string) (ordered []string, ok bool) {
	if len(labels) == 0 {
		return nil, false
	}
	f, err := openFrame(rw)
	if err != nil {
		return nil, false
	}
	defer f.close()

	order := make([]string, len(labels))
	copy(order, labels)
	cursor := 0
	f.draw(reorderLines(order, cursor))

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

	for {
		key, ok := rw.readByte()
		if !ok {
			f.clear()
			return nil, false
		}
		switch key {
		case 3, 'q', 'Q': // Ctrl-C, q
			f.clear()
			return nil, false
		case '\r', '\n':
			f.clear()
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
				f.clear()
				return nil, false
			default:
				continue
			}
		default:
			continue
		}
		f.draw(reorderLines(order, cursor))
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
const escapeWait = 30 // milliseconds

// readEscape reads what follows an already-consumed ESC byte: a bare
// Escape keypress, a plain arrow ("ESC [ A/B"), or an arrow held with a
// modifier key ("ESC [ 1 ; <modifier> A/B", xterm's scheme for Shift,
// Alt, Ctrl and combinations of them on a cursor key).
func readEscape(rw *Terminal) escKey {
	b1, ok := rw.pollByte()
	if !ok || b1 != '[' {
		return escBare
	}
	var params []byte
	var final byte
	for {
		b, ok := rw.pollByte()
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

// listLines renders labels as a list with the one at cursor highlighted —
// prefix(i) goes before label i, a checkbox say — then hint.
func listLines(labels []string, cursor int, prefix func(i int) string, hint string) []string {
	lines := make([]string, 0, len(labels)+1)
	for i, label := range labels {
		if i == cursor {
			lines = append(lines, "\x1b[36m❯ "+prefix(i)+label+"\x1b[0m")
		} else {
			lines = append(lines, "  "+prefix(i)+label)
		}
	}
	return append(lines, "\x1b[2m"+hint+"\x1b[0m")
}

func noPrefix(int) string { return "" }

func selectLines(labels []string, sel int) []string {
	return listLines(labels, sel, noPrefix, "(↑/↓ to move, enter to select, q to quit)")
}

func checklistLines(labels []string, marks []bool, cursor int) []string {
	box := func(i int) string {
		if marks[i] {
			return "[x] "
		}
		return "[ ] "
	}
	return listLines(labels, cursor, box, "(↑/↓ to move, space to toggle, enter when done, q to quit)")
}

func reorderLines(labels []string, cursor int) []string {
	return listLines(labels, cursor, noPrefix, "(↑/↓ to select, shift+↑/↓ to move the selected item, enter to confirm, q to quit)")
}

// readMasked is ReadMasked's loop, echoing to echo: kept apart from raw
// mode so it can be fed keys under test.
func readMasked(rw *Terminal, echo io.Writer) (string, bool) {
	var line []byte
	erase := func() {
		// Back up over the last character: its bytes, as UTF-8, but one *.
		i := len(line) - 1
		for i > 0 && line[i]&0xC0 == 0x80 {
			i--
		}
		line = line[:i]
		io.WriteString(echo, "\b \b")
	}
	for {
		b, ok := rw.readByte()
		if !ok {
			return "", false
		}
		switch {
		case b == '\r' || b == '\n':
			io.WriteString(echo, "\r\n")
			return string(line), true
		case b == 3: // Ctrl-C
			io.WriteString(echo, "\r\n")
			return "", false
		case b == 0x1b:
			// A key with an escape sequence, or a bracketed paste's
			// markers, is skipped; Esc on its own gives up.
			if readEscape(rw) == escBare {
				io.WriteString(echo, "\r\n")
				return "", false
			}
		case b == 0x7f || b == 0x08: // Backspace
			if len(line) > 0 {
				erase()
			}
		case b == 0x15: // Ctrl-U
			for len(line) > 0 {
				erase()
			}
		case b < 0x20:
			// Any other control character (a tab, say) isn't part of a
			// token.
		default:
			line = append(line, b)
			if b&0xC0 != 0x80 {
				io.WriteString(echo, "*")
			}
		}
	}
}
