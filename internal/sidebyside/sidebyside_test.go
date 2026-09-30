package sidebyside

import (
	"strconv"
	"strings"
	"testing"
)

func TestRenderShowsOnlyChangedSectionsWithContext(t *testing.T) {
	var a, b []string
	for i := 1; i <= 30; i++ {
		a = append(a, "line "+itoa(i))
		b = append(b, "line "+itoa(i))
	}
	b[4] = "line 5 changed" // one change near the top
	b = append(b[:20], append([]string{"inserted"}, b[20:]...)...)
	out := Render("mine", "theirs", strings.Join(a, "\n"), strings.Join(b, "\n"), 80, false)

	for _, want := range []string{"mine", "theirs", "line 5 ", "≠", "line 5 changed", "> inserted", "line 2 ", "line 8 ", "⋯"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "line 12 ") || strings.Contains(out, "line 30") || strings.Contains(out, "line 1 ") {
		t.Errorf("output shows unchanged lines far from any change:\n%s", out)
	}
	if strings.Contains(out, "\x1b[") {
		t.Error("color was off, but escape codes were written")
	}
}

func TestRenderKeepsToTheWidth(t *testing.T) {
	long := strings.Repeat("x", 200)
	out := Render("a", "b", long, long+"y", 60, false)
	for _, l := range strings.Split(strings.TrimSuffix(out, "\n"), "\n") {
		if n := len([]rune(l)); n > 60 {
			t.Errorf("a line is %d columns wide, over 60: %q", n, l)
		}
	}
}

func TestRenderSaysWhenThereIsNothingToShow(t *testing.T) {
	if out := Render("a", "b", "same\n", "same\n", 80, false); !strings.Contains(out, "(no differences)") {
		t.Errorf("identical texts rendered as:\n%s", out)
	}
}

func itoa(i int) string { return strconv.Itoa(i) }
