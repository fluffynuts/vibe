# Saying what a review is when it is offered again, and reviewing a range of commits (planned, not built)

Raised 2026-10-23, while asking whether a review that could be re-opened was this branch's (it was another's: the git author fix).

## 1. "Reopen it" doesn't say what it reopens

Now (`drawStart` in `app.js`, `review-closed` and `review-reopen` in `index.html`): a closed review shows "The last review was closed. <why>" over a bare **Reopen it**. Nothing says what the review was of, when it was raised, or whether its comments were dealt with, so the user has to open it to find out whether it's the one they mean.

The review already holds all of that (`internal/review/store.go`, `Review`): `Ref` (a branch, for "the changes against X"), `Commit` (one commit), `Unpushed`, none of them (the working tree), `Note` (what the agent said it was), `Started`, `State` (in progress / done), `Closed` (why), `Threads` with their statuses. The page already puts the *what* in the bar of an open review (`review-of`: "against master", "of commit abc", …), so one function should say it for both.

**Proposed**

- One `describeReview(rv)` for the *what*: "the changes against master", "commit 1a2b3c4", "the commits not pushed to origin/x", "the uncommitted changes", followed by the agent's note in quotes when there is one. The bar and the start screen both use it.
- The start screen's closed line becomes, for example: *The last review, of the changes against master ("git author: ask to give the agent the user's git author"), started 22 Oct 13:04, was closed: <why>. 8 comments: 8 resolved, 0 open.* and the button says what it reopens (**Reopen that review**). A review with open threads says so in plain words, as that is what makes it worth reopening.
- The *when* is shown as the page shows its other times (`stamp`, local time as 2026-10-22 13:04, with the full time as a tooltip).
- Spelling: "Reopen" stays (it's the standard spelling).
- Not asked, but the same data answers it: if the repository's branch isn't the one the review was of (the review's `Ref`/commit isn't reachable from where the folder is now), say so ("this review was of a different branch"), since opening it would show a diff of something else.

**Open question:** only the last review is kept to reopen; "starting again replaces the last review (it's kept in the folder's history)". Whether the start screen should list the history (a few recent reviews, each with the line above) is a bigger change than this; do the line first, and see whether it is missed.

**Tests (jsdom, `uitest/review.test.js`)**: the line for each kind of review (branch, commit, unpushed, working tree, with and without a note); the counts (resolved/open/dismissed); no counts line when there were no threads; the time shown; the button's label; nothing shown for a dropped review that left nothing to reopen (the existing rule); a review with open threads says so.

## 2. Reviewing a commit range

Now (`vibe-review start`): the uncommitted changes, `--ref BRANCH` (the changes since the branch parted from it), `--commit SHA` (one commit) or `--unpushed` (the commits not yet pushed). There is no way to say "these commits", which is what is wanted after a few hot-fixes on a branch: go back over them together.

**Proposed**

- `vibe-review start --range A..B`: the changes the commits after A up to B made together (git's own meaning of `A..B`, so `HEAD~3..HEAD` is the last three commits); `--range A^..B` includes A. `B` defaults to HEAD when written `A..`.
- On the page's start screen, a "commits…" choice next to the others: a list of recent commits of the branch (subject, short sha, date) in which the user picks the first one to include and the last (default: the newest), so no sha has to be typed. The list is what `git log` says, bounded (say 100), loaded when the start screen is, not at every poll (git is what polls keep clear of).
- Storage: `Review` gains `From` and `To` (resolved to full shas, so the review is of those commits even after the branch moves); `describeReview` says "the 3 commits 1a2b3c4..9f8e7d6" with the first and last subjects; the diff is `git diff From..To` through the same path as one commit's (`--commit`).
- Comments on a range anchor as they do on a commit's diff (lines of the new side, or `--side old`); nothing else about threads changes.

**Edge cases to decide when building:** a range with merges in it (the diff is the tree difference, so a merge's side branch is in it; say so); a range where B isn't a descendant of A (refuse: "A isn't an ancestor of B"); a range that is empty; shas that vanish (a rebased branch): the review keeps the diff it was made of? or says it can't be shown (as a one-commit review would today: check what that does, and do the same).

**Tests**: Go, in `internal/review` against a real temporary repository: the diff of a range equals the sum of its commits' changes; `A..` defaults to HEAD; not-an-ancestor and empty ranges are refused with a message that says what to do; the sha is stored in full and the review survives the branch moving; the CLI's `--range` is parsed with its errors; jsdom: the commit list, choosing first and last, the described label.

## Where

Both are the Review tab's own, and independent of the model/effort work: a branch of their own off master after `feat/model-and-effort` merges, alongside `docs/plan-review-stale-diff.md` (the stale diff notice, next-unresolved navigation, Submit's disabled state), which touches the same code. Do them together or in that order: the description (1) first, as the range (2) needs it for its label.
