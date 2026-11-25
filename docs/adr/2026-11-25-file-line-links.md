# A file named with a line opens at that line, in a viewer that numbers its lines

- **Status:** accepted, built 2026-11-25 on `feat/bare-pr-numbers`. Tried by hand 2026-11-25: code files opened at a line and a range, marked; a selection pasted without numbers; Markdown at a line and a range, formatted and as source.
- **Decided:** 2026-11-25, with the user: numbers wherever a file's text is shown; Markdown always formatted, the block a line is in
  marked (the agent's first build showed its source at a line instead, which the user didn't want: Markdown is read formatted).
- **Came from:** `docs/plan-file-line-links.md` (`git show a8233de:docs/plan-file-line-links.md`).

## Context

The conversation made code that reads like a path (`internal/companion/ui/app.css`) a button showing the file, but left
`internal/companion/ui/app.js:6342` as plain code: a path had to end in an extension. The viewer showed code as one block, with no numbers.

## Decision

- **A path with a line is a file reference**, as agents write them: `path:12`, `path:12:5` (the column isn't shown), `path:12-20`, and
  in a link's address `path#L12`, `path#L12-L20` (GitHub's). The host is asked about the path alone, as before; the button shows what was
  written, its title where it opens ("Show app.js at line 6342").
- **The viewer numbers every file's lines.** The numbers are the style's (`::before`), so what is selected and copied is the file's text
  alone; a Firefox test selects the code and compares it with the file.
- **Opened at a line**, that line (or range) is marked across the viewer and scrolled to the middle. The line is part of the viewer's
  history step, so Back and Forward come back to it, marked, where it was read.
- **A line past the end** (the file changed since it was named) says so, with the file's length, and shows its end.
- **Markdown** is shown formatted, as ever, at a line too: a formatted page has no lines, so the block the line is in (the paragraph,
  list, table or code block that starts at or before it, the blank lines after it counting as its) is marked, with a bar at its left,
  and scrolled to the middle; a range marks every block it touches. The parser says the line each top-level block starts on (not
  enumerable: where a block is isn't what it is). A Source button in the viewer's bar shows any Markdown file's text, numbered, the line
  itself marked there, and Formatted goes back; the user wanted it kept, to copy parts of the raw Markdown (the copy button copies the
  whole file as written either way).
