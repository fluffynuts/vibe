package review

import (
	"context"
	"strings"
	"testing"
)

func fileOf(text string) []string { return strings.Split(text, "\n") }

// A comment follows its code when lines above it come or go: to the same
// text where it is now, else the same text but for spacing; of several,
// the nearest; none, it stays.
func TestACommentFindsItsCodesNewPlace(t *testing.T) {
	file := fileOf("a\nb\n  x := 1\n  y := 2\nc\nx := 1\ny := 2\nd")
	for _, tc := range []struct {
		why  string
		code string
		line int
		want int
		ok   bool
	}{
		{"still there", "c", 5, 5, true},
		{"moved down", "b", 1, 2, true},
		{"of two copies, the nearer", "x := 1\ny := 2", 7, 6, true},
		{"of two copies, the nearer (the other way)", "  x := 1\n  y := 2", 2, 3, true},
		{"only its spacing changed", "x:=1", 1, 3, true},
		{"gone", "z", 4, 0, false},
		{"nothing to look for", "", 4, 0, false},
	} {
		got, ok := Relocate(tc.code, file, tc.line)
		if got != tc.want || ok != tc.ok {
			t.Errorf("%s: %d, %v; want %d, %v", tc.why, got, ok, tc.want, tc.ok)
		}
	}
}

// A comment follows its code in the file as the review has it: in the
// commit reviewed, or the last commit for unpushed ones; not the folder.
func TestCommentsFollowTheFileAsTheReviewHasIt(t *testing.T) {
	dir := cloned(t)
	write(t, dir, "a.txt", "new\none\n")
	git(t, dir, "commit", "-q", "-am", "new line above")
	write(t, dir, "a.txt", "not\ncommitted\nyet\none\n")
	th := Thread{ID: 1, File: "a.txt", Side: "new", Line: 1, EndLine: 1, Code: "one"}
	for _, rv := range []Review{{Commit: "HEAD", Threads: []Thread{th}}, {Ref: "origin/main", Unpushed: true, Threads: []Thread{th}}} {
		if got := Moves(context.Background(), dir, rv); len(got) != 1 || got[0].Line != 2 {
			t.Errorf("commit %q unpushed %v: %+v", rv.Commit, rv.Unpushed, got)
		}
	}
	if got := Moves(context.Background(), dir, Review{Threads: []Thread{th}}); len(got) != 1 || got[0].Line != 4 {
		t.Errorf("the working tree: %+v", got)
	}
}
