package review

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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
	text, err := oldTextOf(context.Background(), dir, Review{}, "a.go")
	if err != nil || text != "package old\n" {
		t.Errorf("working tree's old side: %q, %v", text, err)
	}
	git(t, dir, "checkout", "-q", "-b", "feat")
	git(t, dir, "commit", "-q", "-am", "changed")
	text, err = oldTextOf(context.Background(), dir, Review{Ref: "main"}, "a.go")
	if err != nil || text != "package old\n" {
		t.Errorf("against main: %q, %v", text, err)
	}
	text, err = oldTextOf(context.Background(), dir, Review{Commit: "HEAD"}, "a.go")
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
		if text, err := newTextOf(context.Background(), dir, Review{}, path); err == nil {
			t.Errorf("%s read: %q", path, text)
		}
		if _, err := oldTextOf(context.Background(), dir, Review{}, path); err == nil && path != "link.txt" {
			t.Errorf("%s read from git", path)
		}
	}
}

// A file's colouring can be made never to end: the lexer Chroma has for
// JSONata loops for good on a comment that isn't closed. It's coloured in
// the helper, which is ended when the caller's time is up.
func TestColouringThatNeverEndsIsStopped(t *testing.T) {
	dir := repo(t, map[string]string{"a.txt": "a\n"})
	write(t, dir, "x.jsonata", "/*")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := Colour(ctx, dir, Review{}, "x.jsonata", false)
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Error("a colouring that never ended was given as done")
		}
	case <-time.After(15 * time.Second):
		t.Fatal("colouring went on past its time")
	}
}

func TestAFileIsColouredFromEitherSide(t *testing.T) {
	dir := repo(t, map[string]string{"a.go": "package old\n"})
	write(t, dir, "a.go", "package new\n")
	for old, want := range map[bool]string{true: "package old", false: "package new"} {
		h, err := Colour(context.Background(), dir, Review{}, "a.go", old)
		if err != nil {
			t.Fatal(err)
		}
		if h.Language != "Go" || len(h.Lines) != 1 || joined(h.Lines[0]) != want {
			t.Errorf("old=%v: %+v", old, h)
		}
	}
}

// A block the agent wrote in a conversation has no file name: its language is the tag on its fence, and that alone. Never guessed
// from the text, which could colour prose as code.
func TestABlockIsColouredByItsLanguageTag(t *testing.T) {
	src := "const x = 'hi' // greeting\n/* one\n   two */\n"
	h := HighlightBlock("js", src)
	if h.Language != "JavaScript" || h.Skipped != "" || len(h.Lines) != 3 {
		t.Fatalf("language %q, skipped %q, %d lines", h.Language, h.Skipped, len(h.Lines))
	}
	for i, want := range strings.Split(strings.TrimSuffix(src, "\n"), "\n") {
		if got := joined(h.Lines[i]); got != want {
			t.Errorf("line %d runs put back together: %q, want %q", i+1, got, want)
		}
	}
	if c := classOf(h.Lines[0], "// greeting"); !strings.HasPrefix(c, "c") {
		t.Errorf("a comment is class %q", c)
	}
	if c := classOf(h.Lines[2], "two */"); !strings.HasPrefix(c, "c") {
		t.Errorf("a comment over lines is a comment on each: %q", c)
	}
}

func TestTheTagsAnAgentUsesFindTheirLanguage(t *testing.T) {
	for tag, language := range map[string]string{
		"js": "JavaScript", "javascript": "JavaScript", "JS": "JavaScript", "csharp": "C#", "cs": "C#", "CSharp": "C#", "go": "Go",
		"golang": "Go", "python": "Python", "py": "Python", "json": "JSON", "yaml": "YAML", "yml": "YAML", "sh": "Bash", "bash": "Bash",
		"sql": "SQL", "diff": "Diff", "html": "HTML", "css": "CSS", "ts": "TypeScript", "typescript": "TypeScript", "rust": "Rust",
	} {
		if h := HighlightBlock(tag, "x\n"); h.Language != language {
			t.Errorf("the tag %q gave %q, want %q", tag, h.Language, language)
		}
	}
}

// What isn't a language: nothing to colour, and nothing to say about it (it isn't a block that was too big to colour).
func TestABlockWithNoLanguageOfItsOwnIsLeftPlain(t *testing.T) {
	code := "package main\n\nfunc main() { println(\"hi\") }\n"
	for _, tag := range []string{"", "text", "txt", "plain", "plaintext", "TEXT", "klingon", "markdownish", "no-highlight"} {
		if h := HighlightBlock(tag, code); h.Lines != nil || h.Language != "" || h.Skipped != "" {
			t.Errorf("tag %q: %+v", tag, h)
		}
	}
}

// A tag is a word, and is not given to the lexers' file name matching as one: nothing odd reaches them.
func TestATagThatIsntAPlainWordIsLeftPlain(t *testing.T) {
	for _, tag := range []string{"../etc/passwd", "a/b", "[", "*", "x\x00y", strings.Repeat("a", 200), "go;rm", "go\n", " go"} {
		if h := HighlightBlock(tag, "x\n"); h.Lines != nil || h.Language != "" || h.Skipped != "" {
			t.Errorf("tag %q: %+v", tag, h)
		}
	}
	for _, tag := range []string{"c++", "c#", "f#", "objective-c", "shell_session", "vb.net"} {
		HighlightBlock(tag, "x\n") // must not panic; whether the language is known is chroma's
	}
}

// A very long block isn't coloured, saying why: it would be slow, and the conversation is not where to read a long file. The
// limit is lower than a file's.
func TestABlockTooLongIsntColoured(t *testing.T) {
	for what, text := range map[string]string{
		"too many lines": strings.Repeat("x := 1\n", maxBlockLines+1),
		"too many bytes": strings.Repeat("x := 1 // "+strings.Repeat("a", 90)+"\n", maxBlockBytes/100+1),
	} {
		h := HighlightBlock("go", text)
		if h.Lines != nil || !strings.Contains(h.Skipped, "too long") {
			t.Errorf("%s: skipped %q, %d lines", what, h.Skipped, len(h.Lines))
		}
	}
	if maxBlockLines >= maxHighlightLines || maxBlockBytes >= maxHighlightBytes {
		t.Error("a conversation block's limit isn't below a file's")
	}
	if h := HighlightBlock("go", strings.Repeat("x := 1\n", maxBlockLines)); h.Skipped != "" || h.Lines == nil {
		t.Errorf("at the limit: skipped %q", h.Skipped)
	}
}

// Text that no language is asked for isn't counted: nothing is said of a size that would never have been coloured.
func TestABlockOfNoLanguageIsntToldItWasTooLong(t *testing.T) {
	if h := HighlightBlock("text", strings.Repeat("x\n", maxBlockLines*2)); h.Skipped != "" {
		t.Errorf("skipped %q", h.Skipped)
	}
}

// What a block's colouring does can be made never to end (see TestColouringThatNeverEndsIsStopped), and the text is the
// sandbox's to write: it is coloured in the helper, which is ended when the caller's time is up.
func TestColouringABlockThatNeverEndsIsStopped(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := ColourBlock(ctx, "jsonata", "/*")
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Error("a colouring that never ended was given as done")
		}
	case <-time.After(15 * time.Second):
		t.Fatal("colouring went on past its time")
	}
}

func TestABlockIsColouredInTheHelperAsInProcess(t *testing.T) {
	got, err := ColourBlock(context.Background(), "go", "package main\n")
	want := HighlightBlock("go", "package main\n")
	if err != nil || got.Language != "Go" || len(got.Lines) != len(want.Lines) {
		t.Errorf("%+v, %v", got, err)
	}
}

// The cheap parts (the tag, the size) are answered without a helper at all: the text is not even sent to one.
func TestABlockThatNeedsNoColouringStartsNoHelper(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // a helper started now would be ended at once, with an error
	for what, args := range map[string][2]string{
		"no language": {"klingon", "x\n"},
		"plain":       {"text", "x\n"},
		"too long":    {"go", strings.Repeat("x := 1\n", maxBlockLines+1)},
	} {
		if _, err := ColourBlock(ctx, args[0], args[1]); err != nil {
			t.Errorf("%s: %v", what, err)
		}
	}
}
