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

# Going to the next *unresolved* comment (added 2026-10-22, same plan: both are about the Review tab's comment navigation)

## What the user asked for

Ctrl+Enter in a reply box sends the reply and goes on to the next comment, in the order the user chose; the user wants it to go to the next **unresolved** comment. In a one-shot full review it speeds things up, but once most threads are resolved they scroll through resolved ones, and Ctrl+Enter doesn't help when there's nothing to add.

## What it does now (read, not run)

- The ▲/▼ arrows and the go-on-after-reply (`goToComment`, `goToCommentAfter`, `arrowCards` in `app.js`) walk every thread card on the page, resolved or not, in the page's order or the agent's reading order. Only the comments **pane** has an open/all filter (`pane.filter`); the arrows ignore it.
- Ctrl+Enter with an **empty** reply box does nothing (`send`: `if (op === 'reply' && !body) { box.focus(); return }`).

## Proposed

1. The arrows and the after-reply step go only to **unresolved** (open) threads; a resolved or dismissed one is skipped. At the end they wrap round among the unresolved; when there are none, say so ("No unresolved comments") instead of landing on a resolved one. The comments pane's list is still the way to look at the resolved ones.
2. Ctrl+Enter in an **empty** reply box goes to the next unresolved comment without sending anything, so the same key does the job whether or not there is something to say. (It can't send an empty reply anyway; the box's hint says so: "Reply… (Ctrl+Enter replies, or goes on if empty)".)
3. After a reply the thread just answered stays open until the agent resolves it, so "next" must go past it, as `goToCommentAfter` already does.

## Tests

jsdom (`uitest/review.test.js`): arrows skip resolved and dismissed threads, in both orders; wrap among the unresolved; none unresolved says so and doesn't move; after-reply goes to the next unresolved, not to the one just answered and not to a resolved one; Ctrl+Enter on an empty box goes on and sends nothing (no `/api/review/do` call), and with text still sends then goes on; the arrows' disabled state follows "are there unresolved ones".

## Not decided

- Whether some users want the arrows to include resolved comments (to re-read them). Not building a toggle until someone misses one; the pane covers it.

## Decided

- **"Unresolved" is the whole of it** (`status === 'open'`), the user decided 2026-10-22. A narrower "next that needs me" (open threads whose last word is the agent's) was considered and rejected: when a fix is done the agent posts back and the thread is *resolved*, because the agreed state has been reached, so "the agent spoke last" doesn't mean "waiting on the user". Revisit only if the plain version proves not enough.

# Submit says why it can't (added 2026-10-22, same plan: the Review tab's feedback)

## What happened

The user had replied on a review thread and, while the agent was still answering something else, could have pressed **Submit to agent**. Traced in the code (`reviewSubmit` in `internal/companion/review.go`, `supersede` in `conversation.go`; not run): the host refuses with a 409 ("the agent is still answering the last prompt — submit once it's done"), the page shows that as a red message and stays on the Review tab, and nothing is lost: the comments aren't marked sent until a turn has started, so Submit sends the same set later. But the button looked usable, and the user only learned why after clicking: "it would have communicated to me immediately" had it been disabled with a reason.

## Proposed (the user asked for it, and the disabled state in particular)

1. **Disable Submit while the agent is answering, with the reason on the button**: its label/tooltip says why ("The agent is answering: submit once it's done"), the same way it already says why it's disabled when the sandbox's agent can't be talked to (`review-submit` in `app.js`, near the `unsent`/`state.conversation` handling). It enables itself the moment the agent has finished, with no click or reload; the unsent count stays on it (`Submit 2 to agent`) so it's plain the comments are waiting.
2. The signal already exists on the page: `convo.running` (the conversation is polled from every tab, `setConvoRunning`) and the review's own `agentAtWork`. Use the conversation's, as it is what the host's refusal is about.
3. **Keep the host's refusal**: the button is only a courtesy; a second tab, or a click in the instant between, must still be refused (and say so, as now).
4. The refusal message names the number waiting: "…the agent is still answering the last prompt; your 2 comments are kept, and Submit sends them once it's done".

## Tests

jsdom: Submit disabled with the reason in its title while the conversation says running, and its label still carries the count; enabled once running ends, without a click; disabled with the existing reason when the agent can't be talked to (that one wins); a click on the disabled button sends nothing. Go: the 409's message names the count. Keep the existing test that the host refuses while answering.

## Not decided

- **"Submit when ready"**: the button queueing the submit to go as soon as the agent is done. Left out for now, 2026-10-22; the user likes the idea and hasn't thought it through, and may change their mind when this is worked on. Things to think through then: a pending send that must be cancellable, what happens to comments added meanwhile (sent too, or only those present when it was asked?), what if the agent's answer changes the thread, and what the button says while it waits.
