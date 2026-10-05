package prompt

import (
	"reflect"
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

// TestSelectionLinesListOneItemPerLine is the point of the whole exercise:
// a checklist's answer is a list, and it used to be joined onto one line.
func TestSelectionLinesListOneItemPerLine(t *testing.T) {
	got := selectionLines([]string{"alpha", "bravo"}, "n")
	want := []string{"Confirm selection:", "  - alpha", "  - bravo", "", questionLine + "n"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("selectionLines = %q, want %q", got, want)
	}

	// Nothing checked is still something to confirm, not a silent answer.
	empty := selectionLines(nil, "")
	if len(empty) < 2 || !strings.Contains(empty[1], noneSelected) {
		t.Errorf("an empty selection should say so: %q", empty)
	}
}
