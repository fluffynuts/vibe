package prompt

import (
	"strings"
	"testing"
)

func TestHasShiftModifier(t *testing.T) {
	cases := []struct {
		params string
		want   bool
	}{
		{"", false},    // plain arrow: "ESC [ A", no parameters at all
		{"1", false},   // malformed/incomplete: no modifier field
		{"1;2", true},  // Shift alone
		{"1;3", false}, // Alt alone
		{"1;4", true},  // Shift+Alt
		{"1;5", false}, // Ctrl alone
		{"1;6", true},  // Shift+Ctrl
		{"1;9", false}, // Meta alone
		{"1;10", true}, // Shift+Meta
		{"1;x", false}, // unparseable modifier field
	}
	for _, c := range cases {
		if got := hasShiftModifier([]byte(c.params)); got != c.want {
			t.Errorf("hasShiftModifier(%q) = %v, want %v", c.params, got, c.want)
		}
	}
}

// TestSelectionBlockLineCount pins the one thing that silently breaks the
// confirmation: clear wipes exactly the number of lines it is told, so a
// block that reports fewer lines than it prints leaves residue on screen and
// one that reports more eats the caller's own output above it.
func TestSelectionBlockLineCount(t *testing.T) {
	for _, picked := range [][]string{nil, {"one"}, {"one", "two"}, {"a", "b", "c", "d"}} {
		text, lines := selectionBlock(picked)
		printed := strings.Count(text, "\r\n") + 1 // the question has no break
		if printed != lines {
			t.Errorf("selectionBlock(%v) prints %d lines but reports %d:\n%q", picked, printed, lines, text)
		}
		if !strings.HasSuffix(text, questionLine) {
			t.Errorf("selectionBlock(%v) should end on the question, got %q", picked, text)
		}
	}
}

// TestSelectionBlockListsOneItemPerLine is the point of the whole exercise:
// a checklist's answer is a list, and it used to be joined onto one line.
func TestSelectionBlockListsOneItemPerLine(t *testing.T) {
	text, _ := selectionBlock([]string{"alpha", "bravo"})
	for _, want := range []string{"Confirm selection:\r\n", "  - alpha\r\n", "  - bravo\r\n"} {
		if !strings.Contains(text, want) {
			t.Errorf("block %q does not contain %q", text, want)
		}
	}
	if strings.Contains(text, "alpha, bravo") {
		t.Errorf("the items were joined onto one line: %q", text)
	}

	// Nothing checked is still something to confirm, not a silent answer.
	empty, _ := selectionBlock(nil)
	if !strings.Contains(empty, noneSelected) {
		t.Errorf("an empty selection should say so: %q", empty)
	}
}
