package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"vibe/internal/prompt"
)

// openTerminal and openTerminalInput are how vibe reaches the terminal to
// ask a question on. They are variables so the tests can take the terminal
// away: whether one is there otherwise depends on how the tests were run — a
// Windows CI runner has a console, and a question asked on it waits forever.
var (
	openTerminal      = prompt.Open
	openTerminalInput = prompt.OpenInput
)

// ttyReader opens whatever this process can ask a question on: the
// terminal itself (/dev/tty, or the Windows console), so a redirected stdin
// can't silently answer for the user. The returned close function must be
// called once the answer has been read; ok is false when there is no
// terminal at all.
func ttyReader() (reader *bufio.Reader, closeFn func(), ok bool) {
	in, closeFn, ok := openTerminalInput()
	if !ok {
		return nil, closeFn, false
	}
	return bufio.NewReader(in), closeFn, true
}

// confirm asks a yes/no question on the terminal, defaulting to no. force
// answers yes without asking.
func confirm(force bool, question string) bool {
	return confirmDefault(force, false, question)
}

// confirmDefault asks a yes/no question on the terminal, answering def when
// the user just presses enter. force answers yes without asking.
func confirmDefault(force, def bool, question string) bool {
	if force {
		return true
	}
	reader, closeFn, ok := ttyReader()
	if !ok {
		note("no terminal to confirm on — re-run with -f")
		os.Exit(1)
	}
	defer closeFn()
	hint := "y/N"
	if def {
		hint = "Y/n"
	}
	fmt.Fprintf(os.Stderr, "vibe: %s [%s] ", question, hint)
	line, _ := reader.ReadString('\n')
	line = strings.ToLower(strings.TrimSpace(line))
	switch line {
	case "":
		return def
	case "y", "yes":
		return true
	default:
		return false
	}
}

// interactive reports whether there is a terminal to ask questions on.
func interactive() bool {
	_, closeFn, ok := ttyReader()
	closeFn()
	return ok
}

// ttyRW opens the same terminal ttyReader would, for reading keys and
// drawing — what an interactive list picker needs to put the terminal into
// raw mode and redraw itself in place.
func ttyRW() (tty *prompt.Terminal, closeFn func(), ok bool) {
	tty, ok = openTerminal()
	if !ok {
		return nil, func() {}, false
	}
	return tty, tty.Close, true
}

// chooseFrom asks the user to pick one of labels with an interactive,
// arrow-key-navigable list (inquirer-style). def is the 0-based index
// highlighted first. It returns the chosen index, and ok=false when the
// user quit or there is no terminal to ask on.
//
// Falling back to numbered/typed input when Select can't put the terminal
// into raw mode is deliberately not attempted: interactive() already gates
// every chooseFrom call site on there being a real terminal, and a terminal
// that can't go raw is rare enough (and unhelpful enough for scripting) not
// to be worth a second input mode.
func chooseFrom(question string, labels []string, def int) (int, bool) {
	return asking(question, func(tty *prompt.Terminal) (int, bool) {
		return prompt.Select(tty, labels, def)
	})
}

// filterFrom is chooseFrom for a list too long to show whole: it scrolls, and
// typing narrows it (see prompt.Filter).
func filterFrom(question string, labels []string, def int) (int, bool) {
	return asking(question, func(tty *prompt.Terminal) (int, bool) {
		return prompt.Filter(tty, labels, def)
	})
}

// asking prints question, then lets ask drive an interactive prompt on the
// terminal; ok is false when there is no terminal to ask on.
func asking[T any](question string, ask func(tty *prompt.Terminal) (T, bool)) (answer T, ok bool) {
	tty, closeFn, opened := ttyRW()
	if !opened {
		return answer, false
	}
	defer closeFn()
	note("%s", question)
	return ask(tty)
}

// checklistFrom asks the user to check zero or more of labels with an
// interactive checkbox list. checked is the initial checked state. It
// returns the checked indices, and ok=false when the user quit or there is
// no terminal to ask on.
func checklistFrom(question string, labels []string, checked []bool) ([]int, bool) {
	return asking(question, func(tty *prompt.Terminal) ([]int, bool) {
		return prompt.MultiSelect(tty, labels, checked)
	})
}

// checklistNoConfirmFrom is checklistFrom without the step confirming the
// selection: enter answers straight away.
func checklistNoConfirmFrom(question string, labels []string, checked []bool) ([]int, bool) {
	return asking(question, func(tty *prompt.Terminal) ([]int, bool) {
		return prompt.MultiSelectNoConfirm(tty, labels, checked)
	})
}

// reorderFrom asks the user to rearrange labels with an interactive,
// arrow-key-movable list. It returns the labels in their final order, and
// ok=false when the user quit or there is no terminal to ask on.
func reorderFrom(question string, labels []string) ([]string, bool) {
	return asking(question, func(tty *prompt.Terminal) ([]string, bool) {
		return prompt.Reorder(tty, labels)
	})
}

// statusLine is a single vibe: line on stderr that is shown while work runs
// and then replaced in place by its outcome. Off a terminal, where the line
// can't be rewritten, the outcome is simply printed on the next line.
type statusLine struct {
	tty bool
}

func newStatusLine() statusLine {
	return statusLine{tty: prompt.IsTerminal(os.Stderr)}
}

func (s statusLine) show(format string, a ...interface{}) {
	if s.tty {
		fmt.Fprintf(os.Stderr, "vibe: "+format, a...)
		return
	}
	note(format, a...)
}

func (s statusLine) done(format string, a ...interface{}) {
	if s.tty {
		fmt.Fprint(os.Stderr, "\r\x1b[2K")
	}
	note(format, a...)
}
