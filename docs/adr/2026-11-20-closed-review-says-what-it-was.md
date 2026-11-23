# A closed review says what it was, so Reopen says what it reopens

- **Status:** accepted, built 2026-11-20 on `feat/git-token-next-steps`.
- **Decided:** 2026-11-20, with the user (picking it up alongside the git-token work); raised 2026-10-23.
- **Came from:** part 1 of `docs/plan-review-reopen-and-ranges.md`, which keeps part 2 (reviewing a range of commits). The plan as it was:
  `git show b5e37eb:docs/plan-review-reopen-and-ranges.md`.

## Context

A closed review showed "The last review was closed. <why>" over a bare **Reopen it**: nothing said what the review was of, when it was
raised, or whether its comments were dealt with, so the user had to open it to find out whether it was the one they meant.

## Decision

Under why it was closed (left as it is: a branch switch's reason is a sentence of its own), a line says what it was: what it was of ("the
changes against master", "commit 1a2b3c4", "the commits not pushed to origin/x", "the uncommitted changes"), the agent's note in quotes,
when it started (as the page shows times), and its comments by status, open ones first and said plainly ("4 comments: 2 still open, 1
resolved, 1 dismissed"; "1 comment, resolved"; nothing when it had none). The button says **Reopen that review**. The bar of an open review says what it is of from the same function (`reviewWhat`), so the two
agree: "of the changes against master", where it used to say "against master" (found in review, #114). A dropped review, with
nothing to reopen, shows neither. The line is drawn only when what it says changes (the start screen is redrawn on polls).

## Not built

From the same part of the plan: listing earlier reviews from the history (not asked for; see `docs/plan-review-history.md`), and saying
when the folder is on a different branch from the one the review was of (a branch switch's own reason already says it, when that closed it).
