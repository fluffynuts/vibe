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

// The highlighted row sits in the middle of the window wherever the list
// allows, so the list is seen to scroll under it; near either end it can't,
// and the highlight moves toward the edge instead, which says there's no more.
func TestScrollTopCentresTheCursorUnlessAnEndGetsInTheWay(t *testing.T) {
	cases := []struct{ cursor, n, want int }{
		{0, 100, 0},   // the first: nothing above to centre on
		{5, 100, 0},   // still the first rows
		{6, 100, 1},   // from here the highlight stays in the middle
		{50, 100, 45}, // a far jump centres on it
		{94, 100, 89},
		{95, 100, 90}, // the last window: the highlight moves down
		{99, 100, 90},
		{3, 5, 0},  // fewer matches than rows: all of them, from the top
		{7, 12, 2}, // the last window of a short list
		{0, 0, 0},  // nothing matched
	}
	for _, c := range cases {
		if got := scrollTop(c.cursor, c.n); got != c.want {
			t.Errorf("scrollTop(%d, %d) = %d, want %d", c.cursor, c.n, got, c.want)
		}
	}
}

func TestMovingTheCursorOnePlaceScrollsTheListOneRowUnderAStillHighlight(t *testing.T) {
	for cursor := 6; cursor < 90; cursor++ {
		before, after := scrollTop(cursor, 100), scrollTop(cursor+1, 100)
		if after-before != 1 {
			t.Fatalf("cursor %d to %d moved the window by %d, want 1", cursor, cursor+1, after-before)
		}
		if cursor-before != cursor+1-after {
			t.Fatalf("the highlight moved on screen at cursor %d", cursor)
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
