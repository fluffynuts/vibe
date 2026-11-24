# File references with a line number: shown, and taken to that line (planned, not built)

Raised 2026-11-24 by the user. Rides `feat/bare-pr-numbers` (one release for both).

## Today

The conversation turns code that reads like a path (`internal/companion/ui/app.css`) into a button that shows the file in the
viewer (`app.js`: `looksLikeFile`, `fileCandidate`, `checkFileCandidates` asking `/api/workspace-files`, `fileRef`, `viewFile`).
`internal/companion/ui/app.js:6342` is left as plain code: `looksLikeFile` wants the text to end in an extension, so `:6342` fails it.
The viewer shows code as one `<pre>` with no line numbers (`codeView`).

## Plan

1. **A path with a line is a file reference.** Accepted forms, the ones agents write: `path:12`, `path:12:5` (the column is
   ignored), `path:12-20` (a range), and in a Markdown link's address `path#L12` / `path#L12-L20` (GitHub's). The path part is
   checked exactly as today; the host is asked about the path alone, so nothing changes on the Go side. The button shows the text as
   written (`app.js:6342`), and its title says "Show app.js at line 6342".
2. **The viewer numbers its lines.** A gutter of numbers beside the code, not selectable and not copied with the text (copying the
   code must not pick up the numbers). Same for coloured and plain code.
3. **Opening at a line** scrolls that line into view (near the top, a few lines of context above) and marks it (the range, when one
   is given), e.g. a background, as a code host does. The line is part of the viewer's history step, so Back goes to the right
   place; a step with a line starts there rather than at the top.
4. **A line past the file's end** (the file changed since the agent wrote it) opens the file at its end and says "line 6342 is past
   the end (5900 lines)", rather than nothing.

## Tests first

- jsdom: each accepted form becomes a button once the host says the path is a file; `path:abc`, `1.2.3:4`, `http://x:80` don't;
  pressing one shows the file with that line marked and scrolled to; Back returns to it; the line past the end says so; copying the
  code leaves the numbers out.
- Firefox: the gutter lines up with the code lines (coloured and plain, a long line wrapping or scrolling), and the marked line is
  in view after opening.

## Open questions for the user

1. A line in a Markdown file: the viewer shows Markdown rendered, which has no lines. Show the file's source (numbered) when a line
   is given, with a way to the rendered view? Or open rendered and ignore the line?
2. Line numbers always on in the viewer, or a toggle? (Always is simpler; proposed.)
