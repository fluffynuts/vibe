# Plan: universal and per-profile agent instructions

Status: planned, not started (2026-10-13). Open questions at the end need answers before building.

## Goal

Let the user give every sandbox's agent standing instructions that are loaded in **every session**, not
just when the agent thinks they're relevant:

- `~/.vibe/agent-instructions.md`: universal, for every sandbox.
- `~/.vibe/profiles/<profile>/agent-instructions.md`: specific to one profile's projects.

They are merged (universal first, then project) and loaded by the agent at the start of every session,
including every companion `claude -p` turn. Both can be edited, and optimised by the agent, from the
companion page's Admin tab.

## Decisions made

1. **Names stay consistent.** The user's files are `agent-instructions.md` in both places. What vibe
   generates today under that name in a profile (the kits' instructions, appended when composing) moves
   to `kit-instructions.md`.
2. **Always on, not conditional.** Kit instructions reach the agent through sbx's `agentInstructions`,
   which sbx turns into optional "read when relevant" context (`kits-agent-context/<kit>.md`, pointed to
   from a parent `CLAUDE.md`). The user's instructions must not go that way.
3. **Order:** universal first, then project; the more specific comes last and wins a conflict.
4. **Delivery:** a separate merged file in the agent's home, pulled in by an import in the agent's
   global instructions file, with vibe's part between markers. Nothing outside the markers is touched,
   since the agent itself writes to that file.
5. **Interactive sessions:** the companion picks changes up at its next prompt. The terminal's
   interactive session loads memory at its start, so it only sees changes after a restart, or when
   asked to re-read the file. The page says so after a save.
6. **A stopped sandbox:** saving writes the host files. The sandbox gets them at its next start
   (applied from `nudgeOnStart`, which runs on every start).
7. **Other agents** are mostly a path difference. Do the ones known for sure now (see Agents).
8. **Startup reminder:** while a file is blank, print a tip naming it. Once it has content, one short
   line. Adjust after trying it.
9. **Creating the files:** `--install` creates a blank `~/.vibe/agent-instructions.md` if missing.
   Creating a sandbox and re-composing first, **before any init or compose work**, migrate the profile
   (see Migration), then make sure a blank profile `agent-instructions.md` exists.
10. **Conflicts on save:** if the file changed on disk since the page loaded it, ask:
    ```
    This file has changed on disk. What would you like to do?
    > merge your changes here into it
      overwrite it
      discard your changes here and reload from disk
    ```
    Merge is the default, using the upgrade's three-way merge (`internal/upgrade`, via git), with the
    version the page loaded as the base.

## Merged file format

```
Universal instructions
------
<contents of ~/.vibe/agent-instructions.md>

Project-specific instructions
------
<contents of ~/.vibe/profiles/<profile>/agent-instructions.md>
```

A file that is missing or blank (whitespace only) loses its section. When both are, no merged file is
written, and vibe's marked block is removed from the agent's file. Other content in that file is left
alone.

## Agents

| Agent  | Global file                  | How vibe's part gets in                                           | Status    |
|--------|------------------------------|-------------------------------------------------------------------|-----------|
| claude | `~/.claude/CLAUDE.md`        | `@~/.claude/vibe-instructions.md` import, between markers         | now       |
| gemini | `~/.gemini/GEMINI.md`        | `@` import of `~/.gemini/vibe-instructions.md`, between markers   | now       |
| codex  | `~/.codex/AGENTS.md`         | no imports: the merged text inline, between markers               | now       |
| opencode | `~/.config/opencode/AGENTS.md` | inline, between markers                                       | verify first |
| kiro   | `~/.kiro/steering/vibe.md`   | a steering file of its own                                        | verify first |
| others | unknown                      | editors still work; the page says the agent doesn't load them yet | later     |

Markers: `<!-- vibe: instructions begin -->` … `<!-- vibe: instructions end -->`.

**Verify in a real sandbox before relying on imports:** Claude Code asks for approval before following
some imports. That is believed to be only for project `CLAUDE.md` files importing from outside the
project, not the user-level file. If the user-level file asks too, `claude -p` can't answer, so fall
back to inline text between the markers, as for Codex.

## Migration (profile)

Runs early in sandbox creation and in re-compose (and the Admin tab's re-compose), before composing:

1. If the profile has no `kit-instructions.md` and has an `agent-instructions.md`, rename it to
   `kit-instructions.md` (guided profiles only; see open question 1).
2. If the profile has no `agent-instructions.md`, create a blank one.

From then on:

- `library.Compose` appends kit fragments to `kit-instructions.md` (was `agent-instructions.md`).
- `loadKit` reads `kit-instructions.md` for sbx's `agentInstructions` (was `agent-instructions.md`).
- **Re-compose deletes and rebuilds the profile folder, so it must carry `agent-instructions.md`
  across**, the way it already carries the Claude theme (`claudetheme.FromProfile` /
  `SaveToProfile` in `recomposeProfile`). It must survive every later re-compose.
- Restoring a profile backup restores whatever the backup had; re-compose then migrates it as above.

## Applying it in the sandbox

A function `applyInstructions(vibeHome, name, profileDir, agent)`:

1. Read and merge both files on the host.
2. Over `sbx exec`, write the merged file (e.g. `~/.claude/vibe-instructions.md`), and rewrite the
   marked block in the agent's global file: insert, update, or remove. Writing goes through
   `sbxrun.ExecInput` with a small script, as other writers do.
3. Called from `nudgeOnStart` (every start), and after a save from the Admin tab when the sandbox is
   running.

## Admin tab

A card per file (universal, project), each with:

- A text area with the file's content, its path, its size, and a hint past about 8 KB: these are
  loaded into every session's context, so their size costs tokens on every turn.
- **Save**, with the changed-on-disk dialog (Decision 10). The server stores the file's modification
  time or hash when sending it and checks it on save.
- **Optimise**: asks the agent for a tidier version and puts it in the text area.
  - Then Optimise is disabled until **Accept** (saves, via the same path as Save) or **Revert** (puts
    back the user's text from before optimising). After either, Optimise is available again.
  - Runs as a separate one-off `claude -p` in the sandbox, without `--resume`, so it isn't part of the
    conversation. The prompt asks it to remove repetition and anything its own base system prompt
    already covers, keep the meaning, and return only the document.
  - Needs a running sandbox and a Claude agent; disabled otherwise, saying why. Each run costs a turn.
  - Refused while the agent is answering a conversation turn? It's a separate process, so not
    strictly needed; decide when building.
- **Undo:** setting a text area's `value` from script wipes the browser's undo history. To keep Ctrl-Z
  working through Optimise, Accept and Revert, select everything and use
  `document.execCommand('insertText', …)` (deprecated but still in Chrome and Firefox), falling back to
  setting `value`.

API sketch: `GET /api/admin/instructions` (both files: text, path, version), `POST
/api/admin/instructions` (which, text, base version, and on conflict the chosen action), and `POST
/api/admin/instructions/optimise` (which, text) returning the optimised text. Same protections as
the other Admin calls (token, same-origin JSON, a size cap, say 256 KiB).

## Build order

1. Profile migration plus `kit-instructions.md` in compose and `loadKit`, and re-compose carrying
   `agent-instructions.md` across. Tests in `internal/library`, `internal/profilegen` and `src/vibe`.
2. `--install` creating the universal file, and the startup reminder.
3. Merging and `applyInstructions` for claude, gemini and codex, with marker handling (insert, update,
   remove, leave the rest alone). Unit tests for merge and markers; a real-sandbox check of the Claude
   import approval question.
4. Admin editors with Save and the conflict dialog (three-way merge).
5. Optimise, with Accept and Revert, and undo-friendly replacement.
6. Readme: the two files, where they land per agent, and the Admin editors.

## Open questions

1. **Hand-made (non-guided) profiles:** their `agent-instructions.md` is the user's own text, not
   kits'. Moving it to `kit-instructions.md` would demote it from always-on to read-when-relevant.
   Proposal: migrate guided profiles only, and treat a hand-made profile's existing file as the user's
   project instructions.
2. **Paths:** the request mentioned `~/.vibe/projects/<project>/`; this plan assumes
   `~/.vibe/profiles/<profile>/` is meant.
3. **Kit instructions** stay read-when-relevant (through sbx) while the user's are always on.
   Proposal: keep it that way, since they are "how to use diffity" material.
4. **Merge needs git on the host**, as the upgrade does. When the changes clash, the dialog says so and
   offers only overwrite or reload.
