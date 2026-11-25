# A file named with a line opens at that line, in a viewer that numbers its lines

- **Status:** accepted, built 2026-11-25 on `feat/bare-pr-numbers`. Not yet tried by hand.
- **Decided:** 2026-11-25. Raised by the user 2026-11-24; the two open questions (Markdown at a line, numbers always on) were left to the
  agent's suggestion, to be tried.
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
- **Markdown** is shown formatted, which has no lines. Opened at a line, its source is shown instead, numbered and marked; a button in the
  viewer's bar switches between the source and the formatted page, for any Markdown file. Other files have no such button.
