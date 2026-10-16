// Package instructions handles the user's standing instructions to a
// sandbox's agent: a universal file in ~/.vibe, and one per profile, which
// are merged (universal first) and loaded by the agent in every session.
//
// A guided profile also carries the kits' instructions — how to review with
// vibe-review, say — composed from the library. Those are kept apart, in KitFile, so
// re-composing (which rewrites them) never touches what the user wrote; they
// reach the agent through sbx, as reading for when a kit is relevant.
package instructions

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// File is the user's instructions, both in ~/.vibe (universal) and in a
// profile's directory (for that profile's sandboxes).
const File = "agent-instructions.md"

// KitFile is, in a guided profile's directory, the kits' instructions as
// composing writes them.
const KitFile = "kit-instructions.md"

// EnsureFile creates path, empty, if there's nothing there yet; anything
// that is there is left alone.
func EnsureFile(path string) (created bool, err error) {
	if _, err := os.Stat(path); err == nil {
		return false, nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return false, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		if errors.Is(err, fs.ErrExist) {
			return false, nil
		}
		return false, err
	}
	return true, f.Close()
}

// MigrateProfile brings a profile's directory up to date. A guided profile
// from before the kits' instructions had a file of their own keeps them in
// File: that moves to KitFile (only when there's no KitFile yet, so this is
// done once). Then a blank File is made for the user, if there's none. A
// hand-made profile's File was always its author's own, and stays put.
func MigrateProfile(profileDir string, guided bool) (moved bool, err error) {
	user := filepath.Join(profileDir, File)
	kit := filepath.Join(profileDir, KitFile)
	if guided {
		_, kitErr := os.Stat(kit)
		_, userErr := os.Stat(user)
		switch {
		case kitErr == nil || userErr != nil:
			// Done already, or nothing to move.
		case !errors.Is(kitErr, fs.ErrNotExist):
			return false, kitErr
		default:
			if err := os.Rename(user, kit); err != nil {
				return false, fmt.Errorf("moving the kits' instructions to %s: %w", KitFile, err)
			}
			moved = true
		}
	}
	if _, err := EnsureFile(user); err != nil {
		return moved, err
	}
	return moved, nil
}

// Merge is the user's instructions as the agent gets them: the universal
// ones, then those for the profile, each under a heading, and a file that is
// blank leaving its section out. Both blank merge to "".
func Merge(universal, project string) string {
	var sections []string
	for _, s := range []struct{ heading, text string }{
		{"Universal instructions", universal},
		{"Project-specific instructions", project},
	} {
		text := strings.TrimSpace(normalise(s.text))
		if text == "" {
			continue
		}
		sections = append(sections, s.heading+"\n"+strings.Repeat("-", len(s.heading))+"\n\n"+text+"\n")
	}
	return strings.Join(sections, "\n")
}

// normalise gives text Unix line endings, whatever it was edited with.
func normalise(text string) string {
	return strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n")
}

// The lines vibe's part of an agent's own instructions file is kept
// between: everything outside them is the agent's (Claude writes there when
// asked to remember something) or the user's, and is left alone.
const (
	BeginMarker = "<!-- vibe: instructions begin (written by vibe, which rewrites this part) -->"
	EndMarker   = "<!-- vibe: instructions end -->"
)

// WithBlock is content with vibe's marked part set to block: replaced where
// it is, added at the end where there is none, and removed (with the blank
// line it was set off by) when block is "".
func WithBlock(content, block string) string {
	content = normalise(content)
	start := strings.Index(content, BeginMarker)
	end := strings.Index(content, EndMarker)
	if start >= 0 && end > start {
		before := strings.TrimRight(content[:start], "\n")
		after := strings.TrimLeft(content[end+len(EndMarker):], "\n")
		content = joinParts(before, after)
	}
	block = strings.TrimSpace(block)
	if block == "" {
		return content
	}
	return joinParts(strings.TrimRight(content, "\n"), BeginMarker+"\n"+block+"\n"+EndMarker+"\n")
}

// joinParts joins two pieces of a file with a blank line, either of which
// may be empty, ending the whole with a newline when there is anything.
func joinParts(a, b string) string {
	a, b = strings.TrimRight(a, "\n"), strings.TrimRight(b, "\n")
	switch {
	case a == "" && b == "":
		return ""
	case a == "":
		return b + "\n"
	case b == "":
		return a + "\n"
	}
	return a + "\n\n" + b + "\n"
}

// Agent is where an agent keeps the instructions it loads in every session,
// relative to its home: Dir/Global, which vibe adds its marked part to.
// Imports says the agent follows "@path" lines there, so vibe's part is one
// line pointing at the merged file (Dir/MergedFile); otherwise it is the
// merged text itself.
//
// OneShot, when the agent has one, is how to ask it something once, with no
// tools and nothing kept: the prompt goes in on stdin, the answer comes out
// on stdout. It is what Optimise uses.
type Agent struct {
	Dir, Global string
	Imports     bool
	OneShot     []string
}

// MergedFile is, in an agent's Dir, the merged instructions vibe writes for
// an agent that follows imports.
const MergedFile = "vibe-instructions.md"

// Agents are the agents vibe knows how to give these instructions to; one
// not here doesn't get them (yet).
var Agents = map[string]Agent{
	"claude": {Dir: ".claude", Global: "CLAUDE.md", Imports: true, OneShot: []string{"claude", "-p", "--tools", ""}},
	"gemini": {Dir: ".gemini", Global: "GEMINI.md", Imports: true},
	"codex":  {Dir: ".codex", Global: "AGENTS.md"},
}

// Block is vibe's part of the agent's global file for merged instructions,
// with the agent's home at home: "" when there's nothing to give it.
func (a Agent) Block(home, merged string) string {
	if strings.TrimSpace(merged) == "" {
		return ""
	}
	if a.Imports {
		return "@" + path.Join(home, a.Dir, MergedFile)
	}
	return merged
}

// The tags the agent is asked to put an optimised document between. Not a
// fenced block: instructions often hold fenced examples of their own.
const (
	documentOpen  = "<document>"
	documentClose = "</document>"
)

// OptimisePrompt asks the agent for a tidier version of text, which is the
// universal file (universal) or the profile's; other is the other file, given
// as context only.
func OptimisePrompt(universal bool, text, other string) string {
	var b strings.Builder
	b.WriteString("You are tidying a file of standing instructions a user gives to an AI coding agent " +
		"(you, in another session) at the start of every session. Rewrite it so that nothing the user " +
		"asks for is lost, but:\n" +
		"- remove anything said more than once, keeping its clearest wording;\n" +
		"- remove anything you would do anyway under your own built-in instructions;\n")
	if universal {
		b.WriteString("- keep it general: these apply to every project.\n")
	} else {
		b.WriteString("- remove anything the user's universal instructions, below, already say: these are " +
			"loaded after them, for one project.\n")
	}
	b.WriteString("Keep the user's voice and markdown, and don't add anything they didn't ask for.\n\n" +
		"Reply with the new file only, between " + documentOpen + " and " + documentClose +
		" on lines of their own, and nothing else.\n\n")
	if strings.TrimSpace(other) != "" {
		if universal {
			b.WriteString("For context only (don't rewrite it), the project-specific instructions loaded after these:\n")
		} else {
			b.WriteString("For context only (don't rewrite it), the universal instructions loaded before these:\n")
		}
		b.WriteString(documentOpen + "\n" + strings.TrimSpace(normalise(other)) + "\n" + documentClose + "\n\n")
	}
	b.WriteString("The file to rewrite:\n" + documentOpen + "\n" + strings.TrimSpace(normalise(text)) + "\n" + documentClose + "\n")
	return b.String()
}

// ExtractDocument is the document in the agent's reply: what is between the
// one pair of document tags in it. A reply with none, or more than one, is
// an error rather than a guess.
func ExtractDocument(reply string) (string, error) {
	reply = normalise(reply)
	if strings.Count(reply, documentOpen) != 1 || strings.Count(reply, documentClose) != 1 {
		return "", fmt.Errorf("the agent's answer wasn't one document between %s tags — nothing changed", documentOpen)
	}
	start := strings.Index(reply, documentOpen) + len(documentOpen)
	end := strings.Index(reply, documentClose)
	if end < start {
		return "", fmt.Errorf("the agent's answer wasn't one document between %s tags — nothing changed", documentOpen)
	}
	doc := strings.TrimSpace(reply[start:end])
	if doc == "" {
		return "", errors.New("the agent's answer was an empty document — nothing changed")
	}
	return doc + "\n", nil
}
