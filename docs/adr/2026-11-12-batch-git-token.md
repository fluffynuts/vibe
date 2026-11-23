# `vibe --batch-git-token`: one GitHub token for several sandboxes at once

- **Status:** accepted, built 2026-11-16 on `feat/batch-git-token`, merged (PR #26). Tried by the user on their own sandboxes. What is not
  built was in `docs/plan-git-token-next-steps.md`, built since: `docs/adr/2026-11-20-git-token-next-steps.md`.
- **Decided:** 2026-11-12, with the user; refined while building (2026-11-16), as the user tried it.
- **Came from:** `docs/plan-batch-git-token.md`, now deleted: `git show 7b56280:docs/plan-batch-git-token.md`.

## Context

Several sandboxes use the same GitHub token (the user's work on one set of repositories). Bringing a new sandbox online means making or
regenerating a token, and a regenerated token kills the old one, so every other sandbox holding it stops being able to push. The fix was
`vibe --git-token` in each sandbox's folder in turn, pasting the same token each time.

## Decision

**`vibe --batch-git-token`**, terminal-only like `--git-token` (a token is never typed into the page):

1. **A checklist of sandboxes** (not profiles: a token belongs to a sandbox, and a profile can have several), each with what its token is now
   (`own, expires 2027-11-07`, `default`, `none`), read from vibe's own records with no call to GitHub. **Nothing is ticked to begin with**:
   the token being entered need not be for the sandboxes whose token is about to expire, so vibe doesn't guess.
2. **The list filters as it is typed into** (`MultiFilter`, a new variant in `internal/prompt` beside `MultiSelect` and `Filter`; the existing
   checklist is unchanged): typing narrows to labels containing it (name, folder and profile), backspace widens, Escape clears what is typed
   and then cancels, **Space ticks** (Tab was the first choice; the user found Space the natural key, so a filter can't contain a space),
   Ctrl-A ticks what is shown, and **ticks survive a filter that hides them**.
3. **The token comes from `--git-token`'s own flow**, pulled out so both use it: a new one (GitHub's form, with every ticked sandbox's
   repository listed), one the user has, the default, or **the token another sandbox has**. That last is new in `--git-token` too: vibe
   keeps its own copy of each sandbox's token on the host (`~/.vibe/git/sandboxes/<name>.token`, 0600), so nothing is revealed or pasted.
   The recipient records the donor's source (`default` stays `default`).
4. **Whether to keep it as the default token too** ("…so a renewal through 'vibe --setup' reaches these sandboxes together?") is asked
   right after the token is chosen (the source recorded depends on it). Yes stores it and offers it
   to the sandboxes still on the old default that weren't picked, each keeping its rules.
5. **It is checked with GitHub once per repository, before giving**: a sandbox it can't push to is skipped, with the reason, and the rest go
   on. The ask-or-skip question is asked once, not per sandbox.
6. **Given `--jobs N` at a time** (4 unless said: the user measured 4 against 1 and 16), each sandbox keeping its own rules (`--git-rules`
   presets them all). Each sandbox's lines are shown together when it is done, above a spinner (`⠋ 3 of 10 done — working on a, b, c`).
   The record is reloaded before each is given (a running vibe may have changed it).
7. **A stopped sandbox is given its token without being started**: `sbx secret set` doesn't start one, and the agent's instructions are now
   written only when `sbx ls` says it is running (`ifRunning`, for `--git-token` too); a stopped one gets them at its next session start.
8. **The summary** names each sandbox's outcome, its old and new expiry, a tick or a cross from the check, and the command that tries again
   for one that failed. No token is ever printed, logged or put in it.

Also in `--git-token`: when a token reaches more repositories than the sandbox's, "keep the token it has" now carries both expiries
(`keep the token it has (expires 2027-11-07) — not the new one (expires 2027-11-13)`). Twice the old wording left a dead token in place.

## Consequences

- Quitting the checklist or the token question changes nothing; one sandbox failing stops none of the others.
- The flag is exclusive with the other actions, and needs a terminal: there is no way yet to name the sandboxes for a script.

## Not built

`--sandboxes a,b,c` for scripts, a check from inside the sandbox, and the Admin tab saying which tokens are near expiry: all built since,
in `docs/adr/2026-11-20-git-token-next-steps.md`.

## Not verified

By the author: the real prompts and spinner, and GitHub's token page with several repositories (the user has run it on their sandboxes).
