# Reviewing a range of commits (planned, not built)

Raised 2026-10-23, while asking whether a review that could be re-opened was this branch's (it was another's: the git author fix). Part 1, a
closed review saying what it was, is built: `docs/adr/2026-11-20-closed-review-says-what-it-was.md`; this keeps part 2.

## Reviewing a commit range

Now (`vibe-review start`): the uncommitted changes, `--ref BRANCH` (the changes since the branch parted from it), `--commit SHA` (one commit) or `--unpushed` (the commits not yet pushed). There is no way to say "these commits", which is what is wanted after a few hot-fixes on a branch: go back over them together.

**Proposed**

- `vibe-review start --range A..B`: the changes the commits after A up to B made together (git's own meaning of `A..B`, so `HEAD~3..HEAD` is the last three commits); `--range A^..B` includes A. `B` defaults to HEAD when written `A..`.
- On the page's start screen, a "commits…" choice next to the others: a list of recent commits of the branch (subject, short sha, date) in which the user picks the first one to include and the last (default: the newest), so no sha has to be typed. The list is what `git log` says, bounded (say 100), loaded when the start screen is, not at every poll (git is what polls keep clear of).
- Storage: `Review` gains `From` and `To` (resolved to full shas, so the review is of those commits even after the branch moves); `reviewWhat` (app.js, built with part 1) says "the 3 commits 1a2b3c4..9f8e7d6" with the first and last subjects; the diff is `git diff From..To` through the same path as one commit's (`--commit`).
- Comments on a range anchor as they do on a commit's diff (lines of the new side, or `--side old`); nothing else about threads changes.

**Edge cases to decide when building:** a range with merges in it (the diff is the tree difference, so a merge's side branch is in it; say so); a range where B isn't a descendant of A (refuse: "A isn't an ancestor of B"); a range that is empty; shas that vanish (a rebased branch): the review keeps the diff it was made of? or says it can't be shown (as a one-commit review would today: check what that does, and do the same).

**Tests**: Go, in `internal/review` against a real temporary repository: the diff of a range equals the sum of its commits' changes; `A..` defaults to HEAD; not-an-ancestor and empty ranges are refused with a message that says what to do; the sha is stored in full and the review survives the branch moving; the CLI's `--range` is parsed with its errors; jsdom: the commit list, choosing first and last, the described label.

## Where

It is the Review tab's own, and independent of the model/effort work: a branch of its own off master, alongside `docs/plan-review-tab-enhancements.md` (next-unresolved navigation, Submit's disabled state, and the first build's loose ends), which touches the same code. (The stale-diff notice that used to be planned with them is built: `docs/adr/2026-10-15-review-tab.md`.)
