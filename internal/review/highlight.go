package review

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
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
	return tokenise(lexer, text)
}

// tokenise is text as runs per line, by what Chroma's lexer makes of it: so what spans lines (a comment, a string) is coloured on
// each of them.
func tokenise(lexer chroma.Lexer, text string) Highlighted {
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
	// The text's last newline ends its last line: it doesn't start another.
	if n := len(out.Lines); n > 1 && len(out.Lines[n-1]) == 0 && strings.HasSuffix(text, "\n") {
		out.Lines = out.Lines[:n-1]
	}
	return out
}

// A block in the conversation is not a file: it's text the agent wrote, a few lines as a rule, so a lower limit than a file's.
const (
	maxBlockBytes = 200 << 10
	maxBlockLines = 5000
)

// aTag is what a fence's language can be: a word (c++, f#, objective-c, vb.net), not a path or a pattern, since it is given to
// Chroma's matching of file names.
var aTag = regexp.MustCompile(`^[A-Za-z0-9_+#.-]{1,40}$`)

// lexerForTag is the language a fence's tag names, or nil: by name, alias or file extension, as Chroma has it, and never by
// guessing from the text, which could take prose for code. Plain text is no language.
func lexerForTag(tag string) chroma.Lexer {
	if !aTag.MatchString(tag) {
		return nil
	}
	lexer := lexers.Get(tag)
	if lexer == nil || lexer.Config().Name == "plaintext" {
		return nil
	}
	return lexer
}

// blockTooLong says why text is past what a block is coloured to, or "".
func blockTooLong(text string) string {
	lines := strings.Count(text, "\n")
	if len(text) > maxBlockBytes || lines > maxBlockLines {
		return fmt.Sprintf("too long to colour here (%d lines; a block over %d lines or %d KB isn't coloured)", lines, maxBlockLines, maxBlockBytes>>10)
	}
	return ""
}

// HighlightBlock colours text, the contents of a fenced block, by the language its tag names. Nothing comes back (no lines, nothing
// said) for a tag that names none: that is a block that is plain, not one that wasn't coloured.
func HighlightBlock(tag, text string) Highlighted {
	lexer := lexerForTag(tag)
	if lexer == nil {
		return Highlighted{}
	}
	if why := blockTooLong(text); why != "" {
		return Highlighted{Skipped: why}
	}
	return tokenise(lexer, text)
}

// colour is path's text, from review rv's old side or its new one,
// coloured.
func colour(ctx context.Context, dir string, rv Review, path string, old bool) (Highlighted, error) {
	read := newTextOf
	if old {
		read = oldTextOf
	}
	text, err := read(ctx, dir, rv, path)
	if err != nil {
		return Highlighted{}, err
	}
	return Highlight(path, text), nil
}

// oldTextOf is path's text where review rv's diff starts from: the commit
// before rv's, where its branch parted from its ref, or the folder's last
// commit.
func oldTextOf(ctx context.Context, dir string, rv Review, path string) (string, error) {
	clean, err := inFolder(path)
	if err != nil {
		return "", err
	}
	repo, err := open(dir)
	if err != nil {
		return "", err
	}
	defer repo.close()
	head, err := repo.head()
	if err != nil {
		return "", err
	}
	base := head
	switch {
	case rv.Commit != "":
		c, err := repo.commit(rv.Commit)
		if err != nil {
			return "", err
		}
		if c.NumParents() == 0 {
			return "", fmt.Errorf("commit %s has no parent: %q is new in it", rv.Commit, path)
		}
		if base, err = c.Parent(0); err != nil {
			return "", err
		}
	case rv.Ref != "":
		other, err := repo.commit(rv.Ref)
		if err != nil {
			return "", err
		}
		if head == nil {
			return "", fmt.Errorf("the folder has no commits")
		}
		if base, err = mergeBase(other, head); err != nil {
			return "", err
		}
	}
	if base == nil {
		return "", fmt.Errorf("the folder has no commits: %q is new", path)
	}
	return blobText(base, filepath.ToSlash(clean))
}
