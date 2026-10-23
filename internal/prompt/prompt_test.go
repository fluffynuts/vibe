package prompt

import (
	"os"
	"reflect"
	"regexp"
	"strconv"
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

func numbered(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = strconv.Itoa(i + 1)
	}
	return out
}

func plain(lines []string) string {
	return ansi.ReplaceAllString(strings.Join(lines, "\n"), "")
}

var (
	ansi      = regexp.MustCompile(`\x1b\[[0-9;]*m`)
	moreAbove = regexp.MustCompile(`↑ \d+ more`)
	moreBelow = regexp.MustCompile(`↓ \d+ more`)
)

// A menu that fits a screen is drawn whole, as it always was; the eleven
// agents sbx lists, and a dozen of anything, are no reason to start scrolling.
func TestAMenuOfADozenOrFewerIsDrawnWholeWithNoScrolling(t *testing.T) {
	for _, n := range []int{1, 4, 11, 12} {
		lines := selectLines(numbered(n), n/2)
		if len(lines) != n+1 {
			t.Errorf("%d entries drew %d lines, want %d (each, and the keys)", n, len(lines), n+1)
		}
		if strings.Contains(plain(lines), "more") {
			t.Errorf("%d entries scrolled:\n%s", n, plain(lines))
		}
	}
}

// Past that it scrolls, with the highlighted row kept in the middle and the
// rest counted either side, as the filter list does.
func TestALongerMenuScrollsWithTheHighlightInTheMiddle(t *testing.T) {
	lines := selectLines(numbered(30), 14) // the 15th
	if len(lines) != 13 {
		t.Fatalf("drew %d lines, want 13 (↑ more, 10 rows, ↓ more, keys):\n%s", len(lines), plain(lines))
	}
	got := plain(lines)
	for _, want := range []string{"↑ 9 more", "↓ 11 more", "❯ 15"} {
		if !strings.Contains(got, want) {
			t.Errorf("no %q in:\n%s", want, got)
		}
	}
	if !strings.Contains(plain(lines[6:7]), "❯ 15") {
		t.Errorf("the highlight isn't in the middle of the ten rows:\n%s", got)
	}
	if !strings.Contains(got, "q to quit") {
		t.Errorf("the keys aren't shown:\n%s", got)
	}
}

func TestAMenuHighlightMovesToTheEdgeNearTheEnds(t *testing.T) {
	first := plain(selectLines(numbered(30), 0))
	if moreAbove.MatchString(first) || !strings.Contains(first, "↓ 20 more") || !strings.Contains(first, "❯ 1") {
		t.Errorf("at the start:\n%s", first)
	}
	last := plain(selectLines(numbered(30), 29))
	if moreBelow.MatchString(last) || !strings.Contains(last, "↑ 20 more") || !strings.Contains(last, "❯ 30") {
		t.Errorf("at the end:\n%s", last)
	}
}

func TestEveryEntryOfALongMenuCanBeReachedAndHighlighted(t *testing.T) {
	for cursor := 0; cursor < 40; cursor++ {
		if !strings.Contains(plain(selectLines(numbered(40), cursor)), "❯ "+strconv.Itoa(cursor+1)+"\n") {
			t.Fatalf("entry %d isn't shown highlighted", cursor+1)
		}
	}
}

func TestCtrlCIsReportedAsItIsReadAndOtherKeysAreNot(t *testing.T) {
	interrupted := 0
	term := &Terminal{buf: []byte("a\x03b"), in: emptyFile(t), onInterrupt: func() { interrupted++ }}
	var got []byte
	for i := 0; i < 3; i++ {
		b, ok := term.readByte()
		if !ok {
			t.Fatal("ran out of keys")
		}
		got = append(got, b)
	}
	if string(got) != "a\x03b" || interrupted != 1 {
		t.Errorf("read %q, interrupted %d times; want the keys as typed and one interrupt", got, interrupted)
	}
}

func TestTheMaskedLineAndTheCountdownReportCtrlCThroughReadByte(t *testing.T) {
	// This runs the masked line and the countdown's key read. Select, MultiSelect,
	// Reorder and Filter need a real terminal (raw mode) and aren't run here; they
	// read their keys with the same readByte, which is why one handler there ends
	// the process for all of them.
	interrupted := 0
	term := &Terminal{buf: []byte("ab\x03"), in: emptyFile(t), onInterrupt: func() { interrupted++ }}
	var echo strings.Builder
	if line, ok := readMasked(term, &echo); line != "" || ok || interrupted != 1 {
		t.Errorf("the masked line: %q, %v, interrupted %d times", line, ok, interrupted)
	}
	term = &Terminal{buf: []byte("\x03"), in: emptyFile(t), onInterrupt: func() { interrupted++ }}
	if b, ok := term.waitByte(1); !ok || b != 3 || interrupted != 2 {
		t.Errorf("waitByte: %d, %v, interrupted %d times", b, ok, interrupted)
	}
}

func TestInterruptingWipesTheTerminalRestoresItAndExitsWithTheInterruptStatus(t *testing.T) {
	out, err := os.CreateTemp(t.TempDir(), "out")
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()
	restored, exited := 0, -1
	was := exit
	exit = func(code int) { exited = code }
	defer func() { exit = was }()
	term := &Terminal{out: out, restoreRaw: func() { restored++ }}
	term.exitOnInterrupt()
	data, _ := os.ReadFile(out.Name())
	if restored != 1 || exited != 130 || string(data) != "\r\n" {
		t.Errorf("restored %d times, exit status %d, wrote %q", restored, exited, data)
	}
}

func TestATerminalMadeTheWayOpenMakesOneEndsTheProcessOnCtrlC(t *testing.T) {
	term := newTerminal(emptyFile(t), emptyFile(t), func() {})
	if term.onInterrupt == nil {
		t.Fatal("Ctrl-C would only cancel the question on a Terminal from Open")
	}
}
