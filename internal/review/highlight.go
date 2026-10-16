package review

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
)

// Past these, a file isn't coloured: it would take too long, for little.
const (
	maxHighlightBytes = 1 << 20
	maxHighlightLines = 10000
)

// Token is a run of a line's text, and what it is: Class names it as
// Chroma's HTML formatter does ("k" a keyword, "s" a string, "c" a comment,
// and so on; "" plain text).
type Token struct {
	Class string `json:"c,omitempty"`
	Text  string `json:"t"`
}

// Highlighted is a file coloured: its lines as runs, by its Language; no
// Lines when it isn't (its language unknown, or Skipped, saying why).
type Highlighted struct {
	Language string    `json:"language,omitempty"`
	Lines    [][]Token `json:"lines,omitempty"`
	Skipped  string    `json:"skipped,omitempty"`
}

// Highlight colours text, the whole of the file at path (its name says its
// language; else its content may), so what spans lines (a comment, a
// string) is coloured on each of them.
func Highlight(path, text string) Highlighted {
	lines := strings.Count(text, "\n")
	if len(text) > maxHighlightBytes || lines > maxHighlightLines {
		return Highlighted{Skipped: fmt.Sprintf("too long to colour (%d lines)", lines)}
	}
	lexer := lexers.Match(filepath.Base(path))
	if lexer == nil {
		lexer = lexers.Analyse(text)
	}
	if lexer == nil || lexer.Config().Name == "plaintext" {
		return Highlighted{}
	}
	lexer = chroma.Coalesce(lexer)
	it, err := lexer.Tokenise(nil, text)
	if err != nil {
		return Highlighted{Skipped: "couldn't be coloured: " + err.Error()}
	}
	out := Highlighted{Language: lexer.Config().Name, Lines: [][]Token{{}}}
	for tok := it(); tok != chroma.EOF; tok = it() {
		class := chroma.StandardTypes[tok.Type]
		for i, part := range strings.Split(tok.Value, "\n") {
			if i > 0 {
				out.Lines = append(out.Lines, []Token{})
			}
			if part != "" {
				last := len(out.Lines) - 1
				out.Lines[last] = append(out.Lines[last], Token{Class: class, Text: part})
			}
		}
	}
	// The file's last newline ends its last line: it doesn't start another.
	if n := len(out.Lines); n > 1 && len(out.Lines[n-1]) == 0 && strings.HasSuffix(text, "\n") {
		out.Lines = out.Lines[:n-1]
	}
	return out
}

// OldTextOf is path's text where review rv's diff starts from: the commit
// before rv's, where its branch parted from its ref, or the folder's last
// commit.
func OldTextOf(ctx context.Context, dir string, rv Review, path string) (string, error) {
	clean, err := inFolder(path)
	if err != nil {
		return "", err
	}
	base := "HEAD"
	switch {
	case rv.Commit != "":
		base = rv.Commit + "^"
	case rv.Ref != "":
		parted, err := run(ctx, dir, "merge-base", rv.Ref, "HEAD")
		if err != nil {
			return "", err
		}
		base = strings.TrimSpace(parted)
	}
	return run(ctx, dir, "show", base+":"+filepath.ToSlash(clean))
}
