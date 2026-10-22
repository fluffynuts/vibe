# The Review tab's diff going stale (planned, not built)

Raised 2026-10-22, during a review of the git-author work. Decisions below are the user's; the cause is a hypothesis until a test shows it.

## What was seen

After the agent resolved review threads (and committed fixes), the Review tab's comments pane said all comments were unresolved, and clicking a card went to the summary comment, not to the comment. A page refresh put it right. Nothing told the user the view was out of date, so the tab was confusing, not just stale.

## Probable cause (to confirm first)

While the Review tab is in view, the page re-reads the review (comments and their statuses) every poll (`loadReview(files === null)` in `app.js`, the `tick` branch for the review tab), but reads the **diff** (the files and lines the comments sit on) only on load and on **Refresh**. So after the agent resolves threads, starts a new review, or commits fixes, the comments are current and the diff is not: a card whose file or line isn't in the loaded diff has nowhere to go and falls back to the summary.

**First step when building: a failing jsdom test** (`uitest/review.test.js`) of exactly that: a loaded diff, then a poll whose review has threads on lines the diff lacks; today the card falls back to the summary silently.

## What to build (the user agreed to try it, and to play-test it)

1. **A "may be stale" signal.** The page already learns when the agent last updated the review (`reviewAt`, in the state poll). Remember the `reviewAt` the loaded diff was read for; when the state's moves past it, the diff is flagged stale. (Asking the host to diff on every poll is the thing the poll avoids: it's git.)
2. **Highlight the Refresh button** while the diff is stale: enough to draw the eye, not to jump up in the way. Judge it by playing with it.
3. **A notice, not a modal**, above the diff: the agent has updated the review; what you're looking at may be out of date, so some comments may not open where they belong; **Refresh**. Never blocks, and Refresh stays available whatever the user decides. (The user said blocking modals are for judicious use; this isn't one.)
4. **Auto-refresh only when safe:** the Review tab is not in view (another tab, or the page hidden) and there is no unsent comment or typing in progress (`typingInABox` and the drafts the tab keeps). If the Review tab **is** in view, never move the page under the user: only the highlight and the notice. No refresh while idle in view either.
5. **No silent fallback.** A card whose line isn't in the loaded diff says so ("that comment's line isn't in the diff you have: refresh to see it") and highlights Refresh, instead of showing the summary.

## Tests to write

- The stale case above (red first), then: stale flagged when `reviewAt` moves, not when it doesn't; Refresh cleared by a refresh; auto-refresh when the tab is hidden and there's no draft; none when in view; none with a draft or typing; the card message; the notice's wording and that it isn't a dialog.
- A real-Firefox check that the highlighted button is noticeable and nothing shifts when the notice appears (layout test, like the others there).

## Not decided

- Whether `reviewAt` alone is enough: the agent can commit a fix without touching the review; then only the manual Refresh (highlighted by nothing) catches it. If that bites, the page could ask the host for a cheap "has the tree changed" token (HEAD plus index/worktree stat) now and then, but only while the tab is in view.
- Which branch: after the git-author fix merges, on master or the model/effort branch.
