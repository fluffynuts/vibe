# The Review tab's feedback: its diff going stale (built), the next unresolved comment, and Submit saying why (planned, not built)

Raised 2026-10-22, during a review of the git-author work. Decisions below are the user's.

## Status, 2026-11-10

- **The diff going stale: built** (2026-11-09, released with the test-deps hotfix, PR #20), in a different way from the plan's first idea: the signal is a question to vibe, not `reviewAt` (see "What was built", below). Tried by hand by the user, on both paths. One item (5) turned out not to be needed as written, and a narrower form of it was built (2026-11-09, on `feat/copyable-markdown-blocks`).
- **Going to the next unresolved comment: not built** (the arrows still walk every comment, and the reply box's hint still says "Reply… (Ctrl+Enter replies)").
- **Submit says why it can't: not built** (Submit is not disabled while the agent is answering).

## What was built for the stale diff (2026-11-09)

- **The cause, as it turned out** (read in the code, then seen): comments follow their code (`review.Moves`) only when a diff is worked out (`?diff=1`), and the page works one out when the tab is first shown and on Refresh, and polls the review alone, and only while the Review tab is showing. So after an agent turn that changed code, the comments the agent made sat on the wrong line of the older diff, until Refresh.
- **The signal is a question, asked once per turn, not a poll.** `GET /api/review/changed?key=` (`reviewChanged`, `internal/companion/review.go`) says whether the diff is still the one the page holds, by a key over the files' paths, statuses and fingerprints (`review.DiffKey`, sent with every diff as `diffKey`). It only looks: it moves no comment and closes no review, which a diff request does. This answers the plan's "Not decided" about `reviewAt` being enough: a fix committed without touching the review changes the diff, so the key catches it. The question follows the "hide whitespace changes" setting.
- **When the agent's turn ends** (`reviewTurnEnded`, called from `setConvoRunning`), with a review on and a diff held: if the code changed, then with the **Review tab open** a note sits beside Refresh ("The code has changed since this was loaded.", and the button is outlined), the diff left alone, as the plan wanted; with **another tab open**, the diff is dropped (`forgetFiles()` and `files = null`) and the existing poll works it out afresh when the tab is next shown, rather than an eager background refresh, which would run git for nobody and could close a review while no one is looking. A turn that changed nothing does neither: the plan's "not crying wolf".
- **Item 5, as it turned out.** A comment on a file the held diff lacks is drawn under "On files not in this diff" (`R.placeThreads`), so there is always a card, and going to it was never the silent fall-back to the summary that was seen in 2026-10. What was missing was the hint. Going to such a comment (from the pane or a `#7` link) now asks the same question and, only if the code has changed (or the note is already showing), says so in a toast with a Refresh of its own, and shows the note. A toast, because the comments pane is a fly-out over the right of the diff and can cover the note beside Refresh. With the diff current, "not in this diff" is simply true and nothing is said.
- **Tests:** `uitest/reviewstale.test.js` (jsdom: the note, the refresh on return, no question when no review or no diff, the whitespace option, a failed question, an answer about a replaced diff, and the off-diff comments), `internal/companion/reviewchanged_test.go` (the key, the endpoint, read-only), and a real-Firefox layout test that the note fits the Review bar at three widths.

## The plan as first written

Kept as the record of what was thought first; the status above says what became of it.

## What was seen

After the agent resolved review threads (and committed fixes), the Review tab's comments pane said all comments were unresolved, and clicking a card went to the summary comment, not to the comment. A page refresh put it right. Nothing told the user the view was out of date, so the tab was confusing, not just stale.

## Probable cause (as first thought; confirmed in part, see above)

While the Review tab is in view, the page re-reads the review (comments and their statuses) every poll (`loadReview(files === null)` in `app.js`, the `tick` branch for the review tab), but reads the **diff** (the files and lines the comments sit on) only on load and on **Refresh**. So after the agent resolves threads, starts a new review, or commits fixes, the comments are current and the diff is not: a card whose file or line isn't in the loaded diff has nowhere to go and falls back to the summary.

**First step when building: a failing jsdom test** (`uitest/review.test.js`) of exactly that: a loaded diff, then a poll whose review has threads on lines the diff lacks; today the card falls back to the summary silently.

## What to build (as first planned; the user agreed to try it, and to play-test it)

1. **A "may be stale" signal.** *(Built differently: a question to vibe, not `reviewAt`.)* The page already learns when the agent last updated the review (`reviewAt`, in the state poll). Remember the `reviewAt` the loaded diff was read for; when the state's moves past it, the diff is flagged stale. (Asking the host to diff on every poll is the thing the poll avoids: it's git.)
2. **Highlight the Refresh button** *(built: outlined while the note shows)* while the diff is stale: enough to draw the eye, not to jump up in the way. Judge it by playing with it.
3. **A notice, not a modal** *(built: the note beside Refresh)*, above the diff: the agent has updated the review; what you're looking at may be out of date, so some comments may not open where they belong; **Refresh**. Never blocks, and Refresh stays available whatever the user decides. (The user said blocking modals are for judicious use; this isn't one.)
4. **Auto-refresh only when safe:** *(built as a refresh on return, not an eager one; drafts survive a redraw, and the diff is not touched while the tab is in view)* the Review tab is not in view (another tab, or the page hidden) and there is no unsent comment or typing in progress (`typingInABox` and the drafts the tab keeps). If the Review tab **is** in view, never move the page under the user: only the highlight and the notice. No refresh while idle in view either.
5. **No silent fallback.** *(Built in the narrower form above: such comments were already drawn, but the hint was missing.)* A card whose line isn't in the loaded diff says so ("that comment's line isn't in the diff you have: refresh to see it") and highlights Refresh, instead of showing the summary.

## Tests to write

- The stale case above (red first), then: stale flagged when `reviewAt` moves, not when it doesn't; Refresh cleared by a refresh; auto-refresh when the tab is hidden and there's no draft; none when in view; none with a draft or typing; the card message; the notice's wording and that it isn't a dialog.
- A real-Firefox check that the highlighted button is noticeable and nothing shifts when the notice appears (layout test, like the others there).

## Not decided (as first written; settled above)

- Whether `reviewAt` alone is enough: the agent can commit a fix without touching the review; then only the manual Refresh (highlighted by nothing) catches it. If that bites, the page could ask the host for a cheap "has the tree changed" token (HEAD plus index/worktree stat) now and then, but only while the tab is in view.
- Which branch: after the git-author fix merges, on master or the model/effort branch. *(It went out with the test-deps hotfix, from `master`.)*

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

# Still open on the stale diff

- With "hide whitespace changes" on, a whitespace-only edit still counts as a change and shows the note, because a file's fingerprint includes its blob id. Rare, and Refresh then changes nothing visible; a key over the hunks alone would not have this.
- The toast with a Refresh button over the fly-out has been checked in jsdom, and a plain toast over the pane in real Firefox; the two together have not been looked at by hand.
