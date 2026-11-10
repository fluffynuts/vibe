# Agent instructions: the user's, per profile, loaded in every session

- **Status:** accepted, built 2026-10-13 (steps 1–6 of its plan).
- **Decided:** 2026-10-13, with the user; all open questions were answered the same day.
- **Came from:** `docs/plan-agent-instructions.md`, now deleted: `git show 7700047:docs/plan-agent-instructions.md`.

## Context

An agent's standing instructions (how this user likes to work) belong in every session of every sandbox, not only when the agent
decides they are relevant. vibe already had a way to give an agent text: a profile's composed features add instructions, which sbx
hands to the agent as optional "read when relevant" context (`kits-agent-context/<kit>.md`, pointed to from a parent `CLAUDE.md`).
That is the wrong channel for a user's rules, and the file the features wrote was called `agent-instructions.md`, the name the user
would expect for their own.

## Decision

1. **Two files are the user's**, both called `agent-instructions.md`: `~/.vibe/agent-instructions.md` (for every sandbox) and
   `~/.vibe/profiles/<profile>/agent-instructions.md` (for that profile's sandboxes). What composing wrote under that name moves to
   **`kit-instructions.md`**, and stays read-when-relevant through sbx.
2. **They are always on.** vibe merges them (universal first, then the profile's, each under a heading; the more specific is last, so it
   wins a clash; a blank file leaves its section out) and writes the result into the agent's home as its own file
   (`~/.claude/vibe-instructions.md` for Claude), imported from the agent's global instructions file by a line between markers
   (`<!-- vibe: instructions begin … -->` … `<!-- vibe: instructions end -->`). Nothing outside the markers is touched: the agent writes
   to that file too, when asked to remember something. Codex has no imports, so it gets the merged text itself between the markers.
3. **Applied when a sandbox starts** (`nudgeOnStart`) and straight away to running sandboxes when a file is saved from the Admin tab.
   A stopped sandbox gets it at its next start. A session already open in a terminal picks it up on restart, and the page says so.
4. **The Admin tab edits them**: a card per file, Save with a conflict dialog when the file changed on disk since the page loaded it (merge, using
   the upgrade's three-way merge through git; overwrite; or reload), a size hint (the files are loaded into every turn, so size costs
   tokens), and **Optimise**, which asks the agent for a tidier version. Optimise runs as a one-off, tools-off `claude -p` with no
   `--resume`, so it is not part of the conversation; the result goes only into the editor, and nothing is saved until **Accept**
   (Revert puts back the text from before). It takes the document from between `<document>` tags, not from a fenced block, since
   instructions often hold fenced examples.
5. **Migration and carrying:** a guided profile from before has its composed text moved to `kit-instructions.md` once; a hand-made
   profile's file is its author's own and becomes its project instructions as it is. A blank file is made where there is none
   (`--install` makes the universal one). A re-compose carries the user's file across, as it does the theme.
6. **One table of per-agent facts** holds the instructions file, whether it supports imports, and how to run a one-off, tools-off
   prompt. Claude has all three; Codex and Gemini have the file parts; an agent with no one-off prompt shows "Optimise isn't available
   for it yet".

## Consequences

- The user's instructions cost tokens on every turn, so the page shows their size and offers Optimise.
- Writing as the agent user (not root) keeps the files editable by the agent.
- **Guards from review:** the project editor is read-only for a profile that exists only in the bundle (a save would otherwise make an
  overlay folder that hides the bundled profile); the merge normalises `\r\n`; restoring a backup from before the migration is
  migrated by the next re-compose.
- **While a terminal session is attached, an Admin action reports only to the page and the terminal's title**, never printing over the
  agent's full-screen interface. This holds for every Admin action, not only Optimise.
- Optimise uses the sandbox's own model; there is no per-task model choice, which gets awkward across agents.
- A later change (2026-11) relies on this channel: vibe's own rule for handing over text to paste (`copyable-blocks`) is put in the merged
  text, at the top, because a rule left in the kit's read-when-relevant text was not read by a fresh agent.

## Not built

- **Other agents' files.** `opencode` (`~/.config/opencode/AGENTS.md`, inline) and `kiro` (a steering file) were marked "verify first",
  and any other agent "later". The readme says other agents don't load these files yet; the editors still work for them, and the
  page says so.

## Not verified

- Whether Claude Code asks for approval before following the `@` import from the user-level file was to be checked in a real sandbox
  before relying on it. It works in daily use (the sandboxes this was built in load their instructions that way), but the check was never
  written up.
