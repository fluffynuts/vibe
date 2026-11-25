# The Review tab: enhancements (planned, not built)

What is left of the Review tab after `docs/adr/2026-10-15-review-tab.md`: the loose ends
of the first build. None is urgent. `docs/plan-review-reopen-and-ranges.md` is a separate plan for the same tab, and
`docs/adr/2026-10-29-stop-ends-review.md` for stopping the agent while it writes a review (built).

## Loose ends of the first build

- **Tabs open on the same review don't sync.** Each has its own drafts, open composer and edit in progress, and only sees another's changes on its
  next poll. Ideally they would sync (the user, 2026-10-16). Meanwhile, edit and delete find a comment by when it was said, not its place, so a tab
  behind the times can't change the wrong one.
- **Large diffs:** a cap on what is shown at once, with files collapsed past a size, was an open question. (Only an untracked file too large to show is
  marked; a very large tracked diff has no cap, and meets the helper's time and memory limits, which stop it with "reading the repository took too
  long".)
- **A Windows host** has never been seen: is the review folder's path the same inside the sandbox, and does the host have what it needs? The tab
  is meant to say so if not.
- The user's global instructions name diffity for reviews, which is gone: theirs to change.
- Never asked for since the first build, so only listed: submitting to a GitHub pull request, reverting hunks, guided tours beyond the reading
  order.

Built 2026-11-25 on `feat/review-tab-pr-link`, and moved to `docs/adr/2026-11-25-review-to-pr.md`: going to the next unresolved comment,
Submit saying why it can't, and the branch's pull request in the top bar (`git show 0bf625a:docs/plan-review-tab-enhancements.md` for
the plan as it was).
