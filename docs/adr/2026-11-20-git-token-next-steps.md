# GitHub tokens: a batch for scripts, a check from inside the sandbox, and tokens running out on the Admin tab

- **Status:** accepted, built 2026-11-20 on `feat/git-token-next-steps`.
- **Decided:** 2026-11-20, with the user; the check from inside changed the same day, once a real token showed GitHub's API couldn't tell
  (below).
- **Came from:** `docs/plan-git-token-next-steps.md`, now deleted: `git show b5e37eb:docs/plan-git-token-next-steps.md`. What it followed
  is `docs/adr/2026-11-12-batch-git-token.md`.

## Context

`vibe --batch-git-token` needed a terminal to tick sandboxes on; the check it made was GitHub's answer about the token, not about the
sandbox holding it; and nothing said, across sandboxes, whose token was about to run out.

## Decision

- **`--sandboxes a,b,c` names the sandboxes instead of ticking them**, with or without a terminal. With none, the token comes from
  `--git-token-env VAR` (required then; it goes with `--sandboxes` alone). A name vibe doesn't know is refused, naming it, before anything
  is given. The token is checked and given as in the asked-for batch, a sandbox whose repository it can't reach is skipped, and the run
  fails when any sandbox didn't get it. It never makes the token the default (decided: no flag for that). On a terminal, `--sandboxes`
  alone names them and asks only for the token.
- **A check from inside the sandbox**, after the token is given (by `--git-token`, the batch, or a renewal): a dry-run push, from the
  sandbox's workspace, over HTTPS to its repository, to a branch that doesn't exist. GitHub refuses it (403) before anything would be sent
  when the token may not push; when it may, `--dry-run` sends nothing. **GitHub's API can't be the check:** `gh api repos/O/R` answered
  `permissions.push: true` for a read-only token, as it reports the account's access, not the token's (seen 2026-11-20, a real push
  refused moments later). Only a running sandbox is asked (running anything in a stopped one would start it); a repository with no commit
  has nothing to name, and says so. `--git-token` says what it found; the batch's summary marks a sandbox GitHub refused (⚠, with how to
  try again) and counts it as not done.
- **Tokens running out, on the Admin tab:** under this sandbox's token, the other sandboxes whose token has expired or expires within
  `gittoken.WarnWithin` (3 days, the session start's warning), from vibe's records (GitHub isn't asked), expired first; and that
  `vibe --batch-git-token` renews several at once. An expired token, this sandbox's or another's, is a reason for the tab's dot, naming
  them; one only about to expire isn't (the dot would be there for days before each expiry).

## Consequences

- A batch can be scheduled (cron, CI) with the token in a secret variable, never on the command line.
- The check costs one `sbx exec` per running sandbox given a token, in the batch's parallel jobs; sbx's keyring stalls slow it (the user
  limits gnome-keyring-daemon's memory, which has kept them away).

## Not verified

The check's `sbx exec` against a real running sandbox: the command was run by hand inside one (refused with a read-only token, passing
with a writable one), and the companion page runs `git -C <workspace>` in sandboxes the same way, but no test runs sbx.
