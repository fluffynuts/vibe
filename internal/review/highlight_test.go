package review

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// joined is a line's runs put back together: always the line's text.
func joined(line []Token) string {
	var b strings.Builder
	for _, t := range line {
		b.WriteString(t.Text)
	}
	return b.String()
}

// classOf is the class of the run of line with text.
func classOf(line []Token, text string) string {
	for _, t := range line {
		if strings.TrimSpace(t.Text) == text {
			return t.Class
		}
	}
	return "(none)"
}

// A file is coloured whole, by its language (from its name), as runs per
// line that put back together are its lines: so a comment over several
// lines is a comment on each.
func TestAFileIsColouredWholeByItsLanguage(t *testing.T) {
	src := "package main\n\n/* one\n   two */\nfunc main() {\n\tx := \"hi\"\n}\n"
	h := Highlight("cmd/main.go", src)
	if h.Language != "Go" || h.Skipped != "" {
		t.Fatalf("language %q, skipped %q", h.Language, h.Skipped)
	}
	want := strings.Split(strings.TrimSuffix(src, "\n"), "\n")
	if len(h.Lines) != len(want) {
		t.Fatalf("%d lines, want %d", len(h.Lines), len(want))
	}
	for i, line := range h.Lines {
		if joined(line) != want[i] {
			t.Errorf("line %d: %q, want %q", i+1, joined(line), want[i])
		}
	}
	if c := classOf(h.Lines[0], "package"); !strings.HasPrefix(c, "k") {
		t.Errorf("package: %q, want a keyword", c)
	}
	if c := classOf(h.Lines[3], "two */"); !strings.HasPrefix(c, "c") {
		t.Errorf("the comment's second line: %q, want a comment", c)
	}
	if c := classOf(h.Lines[5], `"hi"`); !strings.HasPrefix(c, "s") {
		t.Errorf("the string: %q, want a string", c)
	}
}

func TestAFileOfNoKnownLanguageIsntColoured(t *testing.T) {
	if h := Highlight("notes.unknownext", "just words\n"); h.Lines != nil || h.Language != "" {
		t.Errorf("%+v", h)
	}
}

// A very long file isn't coloured, saying why: it would be slow.
func TestAFileTooLongIsntColoured(t *testing.T) {
	h := Highlight("big.go", strings.Repeat("x := 1\n", maxHighlightLines+1))
	if h.Lines != nil || !strings.Contains(h.Skipped, "too long") {
		t.Errorf("skipped %q, %d lines", h.Skipped, len(h.Lines))
	}
}

// The old side is read where the diff starts from: for the folder as it
// is, its last commit; for a branch, where it parted.
func TestTheOldSideIsReadWhereTheDiffStartsFrom(t *testing.T) {
	dir := repo(t, map[string]string{"a.go": "package old\n"})
	write(t, dir, "a.go", "package changed\n")
	text, err := OldTextOf(context.Background(), dir, Review{}, "a.go")
	if err != nil || text != "package old\n" {
		t.Errorf("working tree's old side: %q, %v", text, err)
	}
	git(t, dir, "checkout", "-q", "-b", "feat")
	git(t, dir, "commit", "-q", "-am", "changed")
	text, err = OldTextOf(context.Background(), dir, Review{Ref: "main"}, "a.go")
	if err != nil || text != "package old\n" {
		t.Errorf("against main: %q, %v", text, err)
	}
	text, err = OldTextOf(context.Background(), dir, Review{Commit: "HEAD"}, "a.go")
	if err != nil || text != "package old\n" {
		t.Errorf("a commit's: %q, %v", text, err)
	}
}

// Lines opened from a gap in a review of unpushed commits are the last
// commit's, as its diff is: not the folder's, which may have changed since.
func TestAnUnpushedReviewsLinesAreTheLastCommits(t *testing.T) {
	dir := cloned(t)
	write(t, dir, "a.txt", "one\ntwo\n")
	git(t, dir, "commit", "-q", "-am", "two")
	write(t, dir, "a.txt", "not committed\n")
	lines, err := FileLines(context.Background(), dir, Review{Ref: "origin/main", Unpushed: true}, "a.txt")
	if err != nil || strings.Join(lines, "|") != "one|two" {
		t.Errorf("lines: %q, %v", lines, err)
	}
}

// Nothing outside the folder is read for a review, whatever path it's
// asked for (a comment's file comes from the agent): not by .., not by an
// absolute path, not through a symbolic link.
func TestNothingOutsideTheFolderIsRead(t *testing.T) {
	outside := t.TempDir()
	write(t, outside, "secret.txt", "secret\n")
	dir := repo(t, map[string]string{"a.txt": "one\n"})
	if err := os.Symlink(filepath.Join(outside, "secret.txt"), filepath.Join(dir, "link.txt")); err != nil {
		t.Skip("can't make a symbolic link here:", err)
	}
	for _, path := range []string{"../" + filepath.Base(outside) + "/secret.txt", filepath.Join(outside, "secret.txt"), "link.txt"} {
		if text, err := NewTextOf(context.Background(), dir, Review{}, path); err == nil {
			t.Errorf("%s read: %q", path, text)
		}
		if _, err := OldTextOf(context.Background(), dir, Review{}, path); err == nil && path != "link.txt" {
			t.Errorf("%s read from git", path)
		}
	}
}
