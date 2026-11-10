# The Review tab: enhancements (planned, not built)

What is left of the Review tab after `docs/adr/2026-10-15-review-tab.md`: two feedback changes planned 2026-10-22 and never built, and the loose ends
of the first build. None is urgent. `docs/plan-review-reopen-and-ranges.md` is a separate plan for the same tab, and
`docs/plan-stop-ends-review.md` for stopping the agent while it writes a review.

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

## Going to the next *unresolved* comment (raised 2026-10-22)

### What the user asked for

Ctrl+Enter in a reply box sends the reply and goes on to the next comment, in the order the user chose; the user wants it to go to the next **unresolved** comment. In a one-shot full review it speeds things up, but once most threads are resolved they scroll through resolved ones, and Ctrl+Enter doesn't help when there's nothing to add.

### What it does now (read, not run)

- The ▲/▼ arrows and the go-on-after-reply (`goToComment`, `goToCommentAfter`, `arrowCards` in `app.js`) walk every thread card on the page, resolved or not, in the page's order or the agent's reading order. Only the comments **pane** has an open/all filter (`pane.filter`); the arrows ignore it.
- Ctrl+Enter with an **empty** reply box does nothing (`send`: `if (op === 'reply' && !body) { box.focus(); return }`).

### Proposed

1. The arrows and the after-reply step go only to **unresolved** (open) threads; a resolved or dismissed one is skipped. At the end they wrap round among the unresolved; when there are none, say so ("No unresolved comments") instead of landing on a resolved one. The comments pane's list is still the way to look at the resolved ones.
2. Ctrl+Enter in an **empty** reply box goes to the next unresolved comment without sending anything, so the same key does the job whether or not there is something to say. (It can't send an empty reply anyway; the box's hint says so: "Reply… (Ctrl+Enter replies, or goes on if empty)".)
3. After a reply the thread just answered stays open until the agent resolves it, so "next" must go past it, as `goToCommentAfter` already does.

### Tests

jsdom (`uitest/review.test.js`): arrows skip resolved and dismissed threads, in both orders; wrap among the unresolved; none unresolved says so and doesn't move; after-reply goes to the next unresolved, not to the one just answered and not to a resolved one; Ctrl+Enter on an empty box goes on and sends nothing (no `/api/review/do` call), and with text still sends then goes on; the arrows' disabled state follows "are there unresolved ones".

### Not decided

- Whether some users want the arrows to include resolved comments (to re-read them). Not building a toggle until someone misses one; the pane covers it.

### Decided

- **"Unresolved" is the whole of it** (`status === 'open'`), the user decided 2026-10-22. A narrower "next that needs me" (open threads whose last word is the agent's) was considered and rejected: when a fix is done the agent posts back and the thread is *resolved*, because the agreed state has been reached, so "the agent spoke last" doesn't mean "waiting on the user". Revisit only if the plain version proves not enough.

## Submit says why it can't (raised 2026-10-22)

### What happened

The user had replied on a review thread and, while the agent was still answering something else, could have pressed **Submit to agent**. Traced in the code (`reviewSubmit` in `internal/companion/review.go`, `supersede` in `conversation.go`; not run): the host refuses with a 409 ("the agent is still answering the last prompt — submit once it's done"), the page shows that as a red message and stays on the Review tab, and nothing is lost: the comments aren't marked sent until a turn has started, so Submit sends the same set later. But the button looked usable, and the user only learned why after clicking: "it would have communicated to me immediately" had it been disabled with a reason.

### Proposed (the user asked for it, and the disabled state in particular)

1. **Disable Submit while the agent is answering, with the reason on the button**: its label/tooltip says why ("The agent is answering: submit once it's done"), the same way it already says why it's disabled when the sandbox's agent can't be talked to (`review-submit` in `app.js`, near the `unsent`/`state.conversation` handling). It enables itself the moment the agent has finished, with no click or reload; the unsent count stays on it (`Submit 2 to agent`) so it's plain the comments are waiting.
2. The signal already exists on the page: `convo.running` (the conversation is polled from every tab, `setConvoRunning`) and the review's own `agentAtWork`. Use the conversation's, as it is what the host's refusal is about.
3. **Keep the host's refusal**: the button is only a courtesy; a second tab, or a click in the instant between, must still be refused (and say so, as now).
4. The refusal message names the number waiting: "…the agent is still answering the last prompt; your 2 comments are kept, and Submit sends them once it's done".

### Tests

jsdom: Submit disabled with the reason in its title while the conversation says running, and its label still carries the count; enabled once running ends, without a click; disabled with the existing reason when the agent can't be talked to (that one wins); a click on the disabled button sends nothing. Go: the 409's message names the count. Keep the existing test that the host refuses while answering.

### Not decided

- **"Submit when ready"**: the button queueing the submit to go as soon as the agent is done. Left out for now, 2026-10-22; the user likes the idea and hasn't thought it through, and may change their mind when this is worked on. Things to think through then: a pending send that must be cancellable, what happens to comments added meanwhile (sent too, or only those present when it was asked?), what if the agent's answer changes the thread, and what the button says while it waits.

## The stale diff: still open

- With "hide whitespace changes" on, a whitespace-only edit still counts as a change and shows the note, because a file's fingerprint includes its blob id. Rare, and Refresh then changes nothing visible; a key over the hunks alone would not have this.
- The toast with a Refresh button over the fly-out has been checked in jsdom, and a plain toast over the pane in real Firefox; the two together have not been looked at by hand.
