# Stopping the agent while it writes a review

Planned 2026-10-29, not built. Raised by the user while reviewing: they started a review from the Review tab, switched to
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

- Whether Escape in the prompt box stops through `/api/conversation/stop` (the memory notes say Escape really stops).
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
