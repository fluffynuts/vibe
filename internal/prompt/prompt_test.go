package prompt

import (
	"os"
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

func TestReadMasked(t *testing.T) {
	for _, tt := range []struct {
		name, keys string
		want       string
		ok         bool
		echo       string
	}{
		{"typed", "abc\r", "abc", true, "***\r\n"},
		{"pasted with its newline", "github_pat_x\n", "github_pat_x", true, "************\r\n"},
		{"backspace", "abd\x7fc\r", "abc", true, "***\b \b*\r\n"},
		{"backspace with nothing typed", "\x7fa\r", "a", true, "*\r\n"},
		{"ctrl-u", "abc\x15xy\r", "xy", true, "***\b \b\b \b\b \b**\r\n"},
		{"multi-byte character is one star", "é\x7fe\r", "e", true, "*\b \b*\r\n"},
		{"bracketed paste markers skipped", "\x1b[200~tok\x1b[201~\r", "tok", true, "***\r\n"},
		{"tab ignored", "a\tb\r", "ab", true, "**\r\n"},
		{"ctrl-c gives up", "ab\x03", "", false, "**\r\n"},
		{"esc gives up", "ab\x1b", "", false, "**\r\n"},
		{"input ends", "ab", "", false, "**"},
	} {
		// A Terminal with nothing to read but keys: a bare Esc ends with
		// the buffer, where pollByte would wait on the (absent) terminal,
		// so the test keys put it last.
		var echo strings.Builder
		got, ok := readMasked(&Terminal{buf: []byte(tt.keys), in: emptyFile(t)}, &echo)
		if got != tt.want || ok != tt.ok || echo.String() != tt.echo {
			t.Errorf("%s: got %q, %v, echo %q; want %q, %v, echo %q", tt.name, got, ok, echo.String(), tt.want, tt.ok, tt.echo)
		}
	}
}

// emptyFile is a file with nothing to read, standing in for the terminal
// once a test's keys are used up.
func emptyFile(t *testing.T) *os.File {
	t.Helper()
	f, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.Close() })
	return f
}
