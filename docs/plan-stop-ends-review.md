# Stopping the agent while it writes a review

Planned 2026-10-29; the stop half built 2026-10-30 on `feat/stop-ends-review` (see "Built" at the end). Raised by the user while reviewing: they started a review from the Review tab, switched to
the Conversation tab to ask the agent something, and pressed Stop. Back on the Review tab it still said "the agent is
still reviewing: wait for it to finish", though nothing was. To be built on its own branch from master, straight after the
release that this plan was committed with.

## What goes wrong

A review's state is `in progress` until the agent runs `vibe-review done`. Stopping the agent's turn (the Conversation
tab's Stop) ends the turn and tells the review nothing: it stays `in progress`, the Review tab keeps saying the agent is
reviewing, and the agent isn't. Closing the review from the Review tab is already handled (`closeReview` in `app.js`,
`reviewClose` in `internal/companion/review.go`): it asks, stops the agent, and drops a review that is still being
written. Stop from the other tab goes round it.

The same false "in progress" is left by anything else that ends the turn without `done`: the agent crashing, the sandbox
stopping, an agent that forgets `done`, a turn that errors.

## What there is to build on

- `agentAtWorkOnReview` (`review.go`): the server knows when the agent is at a turn the review began.
- `store.Drop` (`internal/review/store.go`): drops an unfinished review. A replaced review is kept in the folder's
  history, so "lost" means gone from the tab, not deleted.
- The wording, in the Review tab's close dialog: "The agent is still writing this review. Closing it stops the agent,
  and the review is dropped: you can start another at any time."
- `conversationStop` (`conversation.go`) is one line, `s.convo.stop()`: the natural place for the rule.
- The power menu's Stop and Restart already refuse unless the request says it was `confirmed` and sends back the warnings
  it was shown (`internal/companion/power.go`): the same shape.

## The change

1. **Server: `POST /api/conversation/stop` refuses while the agent is writing a review,** unless the request is
   confirmed. A 409 with a `code` (say `review-in-progress`) and the reason. Confirmed, it stops the turn and drops the
   review, as `reviewClose` with `stop: true` does, in one place that both share. A turn the review didn't begin, and a
   review that isn't `in progress`, are stopped as now, with no question. Every client of the call gets the protection,
   not just the button.
2. **Page: the 409 is a confirm dialog** (`confirmThen`) with the Review tab's wording; cancelling leaves the agent
   running; confirming sends the stop again, confirmed. The Review tab then shows its start-a-review options (the
   view `closeReview` leaves).
3. **The stale case: a review `in progress` with no agent at work on it** says so ("the agent stopped before finishing
   this review") and offers to discard it, which resets the tab. This covers the paths the confirmation can't: a crash,
   a stopped sandbox, a forgotten `done`.

## Not checked yet

- Whether Escape in the prompt box stops through `/api/conversation/stop`: to be read in `app.js`, where the conversation's Stop is wired.
- Whether the Admin tab's actions, and the power menu's Stop and Restart, can end a review's turn: they would need the
  same warning, or to be told the review ends with them.
- `vibe --cancel`, and a new prompt sent while the agent is busy, for the same reason.
- What the agent's `vibe-review` calls do once the review has been dropped under it (a `comment` for a review that is
  gone): it should fail loudly and say so, not be taken in silently.

## Tests

- Go: Stop is refused while the agent is writing a review, naming why, and stops nothing; confirmed, it stops the turn
  and drops the review; a turn the review didn't begin is stopped unasked; a review that is `done` is untouched.
- Page (jsdom): the dialog appears with the right words; cancel leaves the agent running and the review as it was;
  confirm sends it confirmed and the Review tab shows its options; a stale `in progress` says so and discarding it resets
  the tab.
- Each written first and watched to fail.

## Size

About 40 lines of Go, 30 of page, and the tests. Medium: the reason it isn't going in with the release that is already
under review, though it behaves as a bug.

## Built 2026-10-30

- **Server:** `POST /api/conversation/stop` takes an optional `{"confirmed": true}`. While the agent is at a turn the review began and the
  review is in progress, it answers 409 with code `review-in-progress` unless confirmed; confirmed, `stopAndDropReview` stops the turn and
  drops the review (the one function `reviewClose` uses too). A turn the review didn't begin, and a finished review (the agent fixing its
  comments), are stopped as before.
- **Stale case:** closing a review that is in progress with nothing at work on it now drops it (`reviewClose`), with "The agent stopped before
  finishing this review, so it was dropped".
- **Page:** the Stop button and Escape both go through `stopAgent`; the 409 is a confirm dialog in the Review tab's words, and confirming
  sends the stop again, confirmed, and puts the Review tab back to its start options. A review in progress with no agent at work says "the
  agent stopped before finishing this review" with a **Discard it** button (which closes it, so it is dropped).
- **The "not checked" list:** Escape in the prompt goes through the same call (now `stopAgent`). The Admin tab's and the power menu's
  stop/restart, and `vibe --cancel`, end the vibe or its turn from outside this call: they leave a review in progress, which the stale case
  now says and lets go of. A new prompt while busy isn't a stop. The agent's `vibe-review` calls into a dropped review fail (a test checks the
  comment is refused).

## Closing on a branch switch (built 2026-10-30/11-02, same branch)

Asked for alongside: a review closes itself when the folder switches branch, since one of a feature is confusing once the folder is back on
master after the release. Decided with the user: (1) switching back doesn't reopen it: it is kept, and reopening is the user's; (2) not
while the agent is at work on it, but on the next look after (as with "nothing left to review"); (3) any change of HEAD's branch counts,
a detached HEAD included, and a review reopened is tied to the branch the folder is on then.

`Review.Branch` and `BranchKnown` remember the branch (a detached HEAD is `""` with BranchKnown set); `Store.SetBranch` notes it and
`Store.CloseOnBranch` closes when it differs. The branch is read by the isolated helper (`review.CurrentBranch`, op `branch`). It is noted
when the review is started from the page and when it is reopened, and, for one started any other way (the agent's own start, one from
before this), at its first look: the review tab's own polls (`reviewState`) do the watching, so a switch is seen when the tab next looks.

## Review fixes (2026-11-02, threads #209–#213)

- **One server-side rule for "abandoned":** a review in progress, no turn of any kind running, and the agent quiet for `reviewQuiet`
  (3 minutes since its last touch, or since the review started). `/api/review` sends it as `abandoned`; the page uses that and no longer
  guesses from `agentAtWork`. While any turn runs, or the agent touched the review lately, it counts as still being written, so an
  agent-started review (inbox) is asked about before Stop too.
- **Branch watcher** waits on `convo.working()` (any turn), like `CloseOver`; branch lookups are throttled to once per `branchLookEvery`
  (5 s) except on `?diff=1`, which always looks.
- **A confirmed stop** stops first, then drops the review only if the turn was still writing it, and answers `stopped` truthfully; a review
  already finished is kept.
