package prompt

import (
	"strings"
	"testing"
)

func testFrame(width *int) (*frame, *strings.Builder) {
	var out strings.Builder
	return &frame{w: &out, width: func() int { return *width }}, &out
}

func TestTruncateKeepsLinesInsideTheTerminal(t *testing.T) {
	cases := []struct {
		in   string
		max  int
		want string
	}{
		{"short", 10, "short"},
		{"exactly10!", 10, "exactly10!"},
		{"a bit too long", 10, "a bit too…\x1b[0m"},
		// Escapes take up no room, and are kept whole.
		{"\x1b[36m❯ highlighted\x1b[0m", 13, "\x1b[36m❯ highlighted\x1b[0m"},
		{"\x1b[36m❯ highlighted\x1b[0m", 6, "\x1b[36m❯ hig…\x1b[0m"},
		{"anything", 0, "anything"}, // width unknown
	}
	for _, c := range cases {
		got := truncate(c.in, c.max)
		if got != c.want {
			t.Errorf("truncate(%q, %d) = %q, want %q", c.in, c.max, got, c.want)
		}
		if c.max > 0 && visibleWidth(got) > c.max {
			t.Errorf("truncate(%q, %d) is %d wide", c.in, c.max, visibleWidth(got))
		}
	}
}

func TestFrameRedrawsFromItsTop(t *testing.T) {
	width := 80
	f, out := testFrame(&width)
	f.draw([]string{"one", "two", "hint"})
	if got := out.String(); got != "one\r\ntwo\r\nhint" {
		t.Fatalf("first draw = %q", got)
	}
	out.Reset()
	f.draw([]string{"uno", "dos", "hint"})
	// Two rows up from the last line is the first; erase to the end of the
	// screen, whatever is there.
	if got, want := out.String(), "\r\x1b[2A\x1b[Juno\r\ndos\r\nhint"; got != want {
		t.Errorf("redraw = %q, want %q", got, want)
	}
	out.Reset()
	f.clear()
	if got, want := out.String(), "\r\x1b[2A\x1b[J"; got != want {
		t.Errorf("clear = %q, want %q", got, want)
	}
	out.Reset()
	f.clear()
	if out.String() != "" {
		t.Errorf("clearing what is already cleared wrote %q", out.String())
	}
}

// The resize bug: narrow a window that reflows, and lines that fitted now
// take two rows each. Going up one row per line stopped short of the top,
// and every redraw left the last one behind.
func TestFrameFindsItsTopAfterTheTerminalNarrows(t *testing.T) {
	width := 40
	f, out := testFrame(&width)
	long := strings.Repeat("x", 30)
	f.draw([]string{long, long, "hint"})

	width = 20 // each 30-wide line is now two rows
	out.Reset()
	f.draw([]string{long, long, "hint"})
	got := out.String()
	if !strings.HasPrefix(got, "\r\x1b[4A\x1b[J") {
		t.Errorf("redraw after narrowing should go up 4 rows (2+2+1, less the one it is on): %q", got)
	}
	// And what is drawn now fits the new width.
	for _, line := range strings.Split(strings.TrimPrefix(got, "\r\x1b[4A\x1b[J"), "\r\n") {
		if w := visibleWidth(line); w >= width {
			t.Errorf("line %q is %d wide on a %d-column terminal", line, w, width)
		}
	}
}

func TestAbortErasesTheActivePrompt(t *testing.T) {
	if Abort() {
		t.Fatal("Abort with no prompt showing should say so")
	}
	width := 80
	f, out := testFrame(&width)
	restored := 0
	f.restore = func() { restored++ }
	screenMu.Lock()
	active = f
	screenMu.Unlock()

	f.draw([]string{"a", "b"})
	out.Reset()
	if !Abort() {
		t.Fatal("Abort should have found the prompt")
	}
	if got, want := out.String(), "\r\x1b[1A\x1b[J"; got != want {
		t.Errorf("Abort wrote %q, want %q", got, want)
	}
	// The prompt's goroutine may still be running: it must not draw over
	// whatever is printed next, nor restore the terminal a second time.
	out.Reset()
	f.draw([]string{"a", "b"})
	f.close()
	if out.String() != "" || restored != 1 {
		t.Errorf("after Abort: drew %q, restored %d times", out.String(), restored)
	}
}
