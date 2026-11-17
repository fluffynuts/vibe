# Earlier reviews: seeing and getting back the ones the agent replaced (planned, not built; low priority)

Raised 2026-11-16, by the user, in passing: while testing the Review tab they were told "earlier findings are in the review history" and
asked how to get back to one. There is no way to, from the page. Not a common need (the user would not have thought of it unprompted), so
this is for when there is time, and it should not ride on another change.

## What is there today (read from the code, not tried against a real sandbox)

- A review is one file on the host, `~/.vibe/reviews/<sandbox name>/review.json` (`state.ReviewPath`), with a copy in its view folder for the agent.
- When the agent (or the page) starts another review, `Store.keep` writes the one being replaced to `history/<UTC time>.json`, in the same
  folder: all of it, threads and comments. It is skipped when the review had no state and no threads (nothing in it). Starting afresh from a
  review that can't be read sets it aside as `history/broken-<time>.json`. Dropping a review being written (`Store.Drop`) keeps it there too.
- Nothing reads `history/` back. **Close review** closes the review on show, keeping it in `review.json`; **Reopen** reopens that same one.
  Neither ever goes to an earlier review. Nothing prunes `history/`, so it only grows.
- Until this is built the only way back is by hand: with vibe stopped, copy a file from `history/` over `review.json` (not tried: the
  review on show is then lost unless it was copied first, and the restored one's `lastId` may be older than the threads in later reviews).

## What is wanted

A list of earlier reviews for the sandbox, and a way to bring one back as the review on show.

1. **List.** Under the start screen (no review open), and from the review bar's menu when one is: "Earlier reviews (n)", each with
   when it was started, its note, what it was of (ref, commit, unpushed), how many comments and how many were open. Newest first.
   `broken-*` files are not listed (they say so elsewhere).
2. **Bring one back.** Choosing one asks first ("this replaces the review on show, which is kept here too"), then makes it the review on
   show. The one it replaces goes to `history/` as any replaced review does, so nothing is lost and it can be brought back the same way.
3. **Nothing else.** No editing of an old review in place, no deleting, no searching (see "Not decided").

## How it would be built

- **Host:** `GET /api/review/history` lists `history/` (names, and the few fields above read from each file, bounded by `maxReview`
  as `Load` is). A new user op `restore` with the file's name: `Store.Do` keeps the current review (`keep`), reads that file, and writes it as
  the review. The name is checked against the pattern `keep` makes (`YYYYMMDD-HHMMSS.nnnnnnnnn.json`), never a path: no `/`, no `..`, no `broken-`.
- **What a restored review is:** it is the user's, to read: `Reopened` true (so it doesn't close itself for its code or branch having moved on:
  `CloseOver` and `CloseOnBranch`), `Closed` empty, `Branch` unknown (as `reopen-review` does), and its state `done` if it was being written
  when it was replaced (the agent is not writing it now). Its `Started` is kept, so it is the same review by the key the page names
  (`review.Key`), and a page showing the one it replaced is refused, not applied to it.
- **Thread numbers:** `LastID` becomes the larger of the restored review's and the current one's, so a comment made on the restored review
  never takes a number a later review used (they are never reused, and the page and the agent's `vibe-review` refer to threads by number).
- **Page:** the list is asked for when the start screen shows, or the menu is opened, not at every poll (reading files). Restoring goes
  through `doReview`, like every other change, and is taken as `showReview` takes one: its diff worked out afresh (it is of a ref the code
  may have left behind: its comments are placed by the diff's lines, and the ones whose lines read differently now say so, as they always do).

## Tests

- Unit (store): `restore` keeps the current review, brings the named one, keeps `Started`, sets `Reopened`, clears `Closed`, makes a review that
  was in progress `done`, takes the larger `LastID`; refuses a name that isn't a history file's (a path, `..`, `broken-`, one that isn't there);
  a restored review is not closed over by `CloseOver` or `CloseOnBranch`; restoring twice brings the first back.
- Unit (host): the listing has each file's fields, newest first, leaves out `broken-` files and ones that can't be read (said, not fatal); an
  oversize file is skipped, as `Load` refuses it; the endpoint answers the same without a review open.
- Page: the list shows on the start screen and from the menu, is asked for once; choosing one asks first, then shows that review with its
  diff worked out afresh; Cancel changes nothing.
- One real run by the user: start two reviews, bring the first back, check its comments sit on their lines and the second is in the list.

## Not decided

- Whether `history/` should be pruned (by count or age), and whether the list should offer to delete one. It only grows today; at one review
  a day it is not a problem for years, so not decided.
- Whether an earlier review should be read-only in the page (a preview) before it replaces the one on show. Replacing it, with the one it
  replaces kept, is simpler and loses nothing, so that is the plan; a preview is a later addition if bringing the wrong one back annoys.
- Whether the agent's `vibe-review` should list history too (`vibe-review history`). Not asked for.
