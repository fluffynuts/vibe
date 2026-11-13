# `vibe --batch-git-token`: one GitHub token for several sandboxes at once (planned, not built)

How a sandbox's token works today is `src/vibe/gittoken.go` (`askGitAccess`, `setGitAccess`, `giveGitAccess`, `renewGitAccess`,
`shareDefaultToken`, `vetGitToken`); the page only shows its state ("Giving vibe the token stays in the terminal", Admin tab).

## What is wanted (raised 2026-11-12)

Several sandboxes use the same GitHub token (the user's work on one set of repositories). Bringing a new sandbox online means making or
regenerating a token, and a regenerated token kills the old one, so every other sandbox holding it stops being able to push. Today the fix is
`vibe --git-token` in each sandbox's folder, one after another, with the same token pasted each time.

**Proposed:** `vibe --batch-git-token`

1. Lists the sandboxes vibe knows, as a checklist, each with what its token is now (`own, expires 2027-11-07`, `default`, `none`: read from
   vibe's own records, no call to GitHub), so the ones holding an old token are easy to spot side by side.
2. The user ticks the ones to give the token to. **Nothing is ticked to begin with**, and **the list can be filtered** (see below): the token being
   entered need not be for the sandboxes whose token is about to expire, so vibe must not guess which.
3. The token is got by the flow `--git-token` already has, once: a new one (GitHub's form is opened, with the repositories of every ticked
   sandbox listed, since no URL can pick them), one the user already has, the default, or **the token another sandbox has** (below); checked
   with GitHub as it is now.
4. It is given to each ticked sandbox in turn, each is **checked afterwards** (below), and a summary says which got it and which didn't, and why,
   with each one's old expiry and new expiry.

Terminal-only, as `--git-token` is: a token is never typed into the page.

## The filter

Filtering works as the other filtered list in the terminal does (`Filter`, `internal/prompt/filter.go`, which picks one): **typing narrows the list**
to the labels containing what is typed (ignoring case), with no key to start it; backspace widens it, Ctrl-U clears it, Up/Down move through what
matches, Enter answers, Ctrl-C ends vibe. Letters are typed, so `j`, `k` and `q` are not keys, as there. Where this differs, because it ticks
and `Filter` picks:

- **Escape clears what is typed** when anything is; with nothing typed it cancels, as it does in `Filter`. Cancelling changes nothing.
- **Tab ticks** the highlighted one (and moves down), and **Ctrl-A ticks everything now shown** (or unticks it, if it was all ticked). Space is
  typed, as in `Filter`, so it can't be the tick key; if Space should tick instead, that is a one-line change and the filter then can't contain a
  space, which a label (name, folder) rarely needs.
- **Ticks are kept for the sandboxes the filter hides**: filter for `yumbi`, tick those, clear it, filter for `phoenix`, tick those. The answer
  is every ticked one, shown or not, and the header says how many are ticked in all.

A **new** variant in `internal/prompt` (`MultiFilter`, beside `MultiSelect` and `Filter`); the existing checklist is left exactly as it is.

## What already exists, and so is reused

- **The token flow**: `openTokenPage`, `enterGitToken`, `pickTokenDays`, `vetGitToken` (the check with GitHub, with try again / use anyway /
  skip). `askGitAccess` mixes "which token" with "this one sandbox" (its question, its repo, what it has now): the "which token" half is
  pulled out so both use it, with the single-sandbox behaviour exactly as it is, bar the one change below.
- **Giving it**: `setGitAccess` per sandbox (the sbx secret, the copy on the host, the agent's instructions for pushing, the state record). A
  sandbox keeps **its own rules** (`inst.GitRules`), as `shareDefaultToken` does today; `--git-rules` presets them for all.
- **The same idea for the default token**: replacing the default is already offered to the sandboxes using it (`shareDefaultToken`). The batch
  covers what that does not: sandboxes holding their own token, and several that were never on the default.

## Taking the token another sandbox has (in both flows)

`sbx` never shows a secret again, but **vibe doesn't need it to**: `giveGitAccess` keeps its own copy of each sandbox's token on the host
(`~/.vibe/git/sandboxes/<name>.token`, readable by the user alone, with `.expires` beside it; `state.GitTokenPath`), to hand to a rebuilt
sandbox. So "take the token sandbox X has" is `gittoken.Load` of that file: nothing is revealed and nothing is pasted. The token is never
printed.

**This is not in `--git-token` today** (its answers are: renew, keep, none, the default, a new one, one you have), so it is added there too, and
the batch gets it from the same code: a new answer, "yes, with the token another sandbox has", then a `Filter` list of the sandboxes that have a
copy (never the one being asked about), each with its folder and when the token expires. A sandbox without a copy (the write failed with a
warning, or it was never given a token) isn't offered, and if none is the answer isn't shown. The token is checked with GitHub like any other,
against **this** sandbox's repository: a copy gone stale, or one that can't push here, says so before anything is given. The recipient records
the same `source` as the donor (`default` stays `default`, so a later renewal through `--setup` still reaches it; `own` stays `own`).

## The check afterwards

Once given, each sandbox's token is **checked with GitHub for that sandbox's repository** (`checkGitToken(token, repoOf(inst.Target))`, which
already says whether it can push), and the summary shows a tick or a cross per sandbox, so a sandbox the update missed is seen at once, not when
its agent next tries to fetch. (The agent that suggested this ran `gh api repos/<owner>/<repo>` from inside the sandbox, which tests the token
as the sandbox has it. Whether that can be done from vibe, with `sbx exec` and whatever the sandbox's `gh` or `git` takes the secret from, is not
known and is not assumed: the host-side check is the plan, and the end-to-end one an addition if it turns out to be cheap.)

## The changes to the single-sandbox flow

When a token reaches more repositories than the sandbox's (`rep.TooBroad()`), `vetGitToken` asks "what now?" with, as its last answer, "keep the
token it has" (`skipLabel(true)`). Chosen by someone expecting the new token to be used, it leaves the **old, dead** token in place, and the
page then says nothing. The labels get both expiries: `keep the token it has (expires 2027-11-07) — not the new one (expires 2027-11-13)`, and
the same wherever `skipLabel` puts that answer. A test pins the wording. This is a change to `--git-token`'s prompt, independent of the batch, and
worth doing even if the batch isn't.

The other change is the new answer above, "the token another sandbox has". Both are in what the batch reuses, so they are built first, on their
own, and `--git-token` alone gets better before the batch exists.

## Decisions made, and open

- **Sandboxes, not profiles.** The token belongs to a sandbox (`state.Instance`), and a profile can have several. The list is of sandboxes
  (name, folder, profile), not profile names.
- **Checks per repository, not per run.** Each sandbox has its own repository, so the token is checked against each distinct one. A sandbox
  whose repository the token can't push to is **skipped with a warning and the rest go on**, and it is in the summary: the token not covering it
  is the user's to fix on GitHub (add the repository to the token). The ask-or-skip prompt of `vetGitToken` is asked once, not once per sandbox.
- **No network at list time, and nothing ticked.** (Decided: an earlier idea of ticking the sandboxes whose token "no longer works" is dropped.)
- **After the batch: also keep it as the default?** If yes, those sandboxes are given it as `default`, and the next renewal through `--setup`
  reaches them together (`shareDefaultToken`). If no, they hold it as `own`. Asked once, at the end, default no.
- **Not interactive yet.** `--batch-git-token --git-token-env VAR` would still need the sandboxes ticked, so it needs a terminal; naming them
  (`--sandboxes a,b,c`) for scripts is a later addition, not this.
- **A sandbox that is gone, stopped or unreachable.** One `sbx` doesn't know is dropped from the list. One that is stopped: `setGitAccess`
  writes into the sandbox, so it is reported as not done, with the command that tries it again for that sandbox, not started behind the user's back.

## What must stay true

- Quitting the checklist or the token question (Escape, Ctrl+C) changes **nothing**, as every question in vibe answers.
- One sandbox failing (sbx refuses the secret, the sandbox can't be reached) stops none of the others, and is repeated in the summary with
  the command that tries again for it (`vibe --git-token` in its folder).
- No token is ever printed, logged or put in the summary.
- The flag is exclusive with the other actions, in `cliargs` and in the usage text and readme, like `--git-token`.

## Tests

Unit, with the stand-ins `gittoken.go` already has (`checkGitToken`, the secret and instruction setters):
- every ticked sandbox and no other gets the token, with its own rules kept; nothing ticked, or quitting, changes nothing;
- a sandbox whose repository the token can't push to is skipped and reported while the rest are given it; a failure to set one secret stops no
  other; the summary names each outcome, old and new expiry, a tick or cross from the check afterwards, and never the token;
- the host copy, in `--git-token` and the batch: a sandbox with one is offered as a source and its token is the one given; one without isn't
  offered, nor the sandbox being asked about; with none to offer the answer isn't shown; the token is checked against the recipient's repository
  like any other; the recipient records the donor's source;
- `--git-rules` presets all; the flag parses and is exclusive (`cliargs`);
- the filter (in `internal/prompt`, driven as `Filter`'s tests drive it): typing narrows with no key to start it, backspace widens, Escape clears
  what is typed and then cancels, Tab ticks, ticks survive a filter that hides them, Ctrl-A ticks only what is shown, and cancelling returns
  nothing;
- the prompt's labels carry both expiries; the single-sandbox `--git-token` tests otherwise pass unchanged, which pins the extraction of the
  shared flow.

One real run by the user on their sandboxes finishes it: it can't be done without `sbx` and GitHub.

## Not decided

Whether to keep a sandbox's host-kept copy of the old token for a rollback (no: it is dead, and keeping dead credentials is clutter); whether the
Admin tab should say, per sandbox, which tokens are near expiry (it can, from the records, with no call to GitHub: a good follow-up, not part of
this); whether the end-to-end check from inside the sandbox is cheap (see above).
