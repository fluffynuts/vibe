# Stopping the agent while it writes a review, and a review closing on a branch switch

- **Status:** accepted, built 2026-10-30 to 2026-11-02 on `feat/stop-ends-review`, merged as PR #10 (`2f9755d`), with two review rounds
  (threads #209–#213, #215–#218). Written up as an ADR on 2026-11-20 (the plan had stayed a plan).
- **Decided:** 2026-10-29, with the user; closing on a branch switch asked for alongside (2026-10-30); "quiet, not abandoned" 2026-11-02.
- **Came from:** `docs/plan-stop-ends-review.md`, now deleted: `git show 0a6b8bc:docs/plan-stop-ends-review.md`.

## Context

A review is `in progress` until the agent runs `vibe-review done`. The user started a review from the Review tab, asked the agent something
from the Conversation tab, and pressed Stop: back on the Review tab it still said the agent was reviewing, and nothing was. Anything else that
ends a turn without `done` (a crash, a stopped sandbox, a forgotten `done`) leaves the same false "in progress".

## Decision

- **Stop asks first while the agent is writing a review.** `POST /api/conversation/stop` answers 409 (`review-in-progress`) unless
  `{"confirmed": true}`, so every client of the call is protected, not just the button. Confirmed, `stopAndDropReview` (shared with
  `reviewClose`) stops the turn first, then drops the review if it is still in progress, and `stopped` says only whether a turn was found
  ("The agent had already stopped." when none was). A turn the review didn't begin, and a finished review (the agent answering its
  comments), are stopped as before. While any turn runs, or the agent touched the review lately, it counts as being written, so an
  agent-started review is asked about too.
- **The page:** the Stop button and Escape both go through `stopAgent`; the 409 is a confirm dialog in the Review tab's words ("The agent
  is still writing this review. Closing it stops the agent, and the review is dropped: you can start another at any time."), and
  confirming puts the Review tab back to its start options.
- **Quiet, not "abandoned":** vibe can't tell an agent that stopped from one writing the review in a terminal (no turn here to see), so it
  never claims the agent stopped and never drops a review unasked. `/api/review` sends `quietMinutes` for a review in progress with no
  turn running and the agent quiet for more than `reviewQuiet` (3 minutes); the page says "no word from the agent for N minutes: it may
  have stopped, or be working where vibe can't see". Closing a review in progress always asks (saying how long it has been quiet), and
  drops it only when confirmed (`{"stop": true}`).
- **A dropped review refuses the agent's later calls:** a `vibe-review comment` into it fails, loudly.
- **A review closes itself when the folder switches branch** (one of a feature is confusing once the folder is back on master): any change
  of HEAD's branch, a detached HEAD included; not while the agent is at work on it, but at the next look after; switching back doesn't
  reopen it (it is kept; reopening is the user's), and a reopened review is tied to the branch it is reopened on. The branch is read by
  the isolated helper (`review.CurrentBranch`), looked up at most every `branchLookEvery` (5 s) except on `?diff=1`, one lookup shared by
  the polls that come meanwhile.

## Consequences

- The Admin tab's and the power menu's stop/restart, and `vibe --cancel`, end a turn from outside this call: they leave a review in
  progress, which then shows as quiet, and can be closed (asking) from the tab.
- A new prompt while the agent is busy isn't a stop, and changes nothing here.
