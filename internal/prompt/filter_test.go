package prompt

import (
	"reflect"
	"strings"
	"testing"
)

func TestFilterMatchesIgnoresCase(t *testing.T) {
	labels := []string{"Africa/Johannesburg", "Europe/London", "America/New_York"}
	if got := filterMatches(labels, "LON"); !reflect.DeepEqual(got, []int{1}) {
		t.Errorf("filterMatches(LON) = %v, want [1]", got)
	}
	if got := filterMatches(labels, ""); len(got) != len(labels) {
		t.Errorf("nothing typed should match everything, got %v", got)
	}
}

func TestScrollTopKeepsTheCursorOnScreen(t *testing.T) {
	cases := []struct{ cursor, top, n, want int }{
		{0, 0, 100, 0},
		{9, 0, 100, 0},
		{10, 0, 100, 1},  // one past the bottom scrolls by one
		{50, 0, 100, 41}, // a far jump puts it on the last row
		{5, 20, 100, 5},  // above the top scrolls up to it
		{99, 95, 100, 90},
		{3, 7, 5, 0}, // fewer matches than rows: all of them, from the top
	}
	for _, c := range cases {
		if got := scrollTop(c.cursor, c.top, c.n); got != c.want {
			t.Errorf("scrollTop(%d, %d, %d) = %d, want %d", c.cursor, c.top, c.n, got, c.want)
		}
	}
}

func TestFilterLinesShowOnlyAWindowOfTheMatches(t *testing.T) {
	var labels []string
	for i := 0; i < 30; i++ {
		labels = append(labels, string(rune('a'+i%26))+strings.Repeat("x", i/26))
	}
	matches := filterMatches(labels, "")
	lines := filterLines(labels, matches, 12, 5, "")
	// filter line, "↑ 5 more", 10 rows, "↓ 15 more", hint.
	if len(lines) != 14 {
		t.Fatalf("got %d lines, want 14: %q", len(lines), lines)
	}
	if !strings.Contains(lines[1], "↑ 5 more") || !strings.Contains(lines[12], "↓ 15 more") {
		t.Errorf("should say how many more there are either side: %q", lines)
	}
	if !strings.Contains(lines[2+12-5], "❯ "+labels[12]) {
		t.Errorf("the cursor's label should be highlighted: %q", lines)
	}
}

func TestFilterLinesSayWhenNothingMatches(t *testing.T) {
	lines := filterLines([]string{"a"}, nil, 0, 0, "zz")
	if lines[0] != "filter: zz" || !strings.Contains(strings.Join(lines, "\n"), "(no matches)") {
		t.Errorf("filterLines with no matches = %q", lines)
	}
}
