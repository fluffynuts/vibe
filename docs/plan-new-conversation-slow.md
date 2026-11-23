# Why "New conversation" is slow to become ready (to investigate, not decided)

Raised 2026-11-23 by the user: the dialog shows at once, but its button takes "quite a while" to become active, and the page
doesn't say what it is waiting for. Split from `docs/plan-bare-pr-numbers.md` (`git show 24c1c9e:docs/plan-bare-pr-numbers.md`), whose
other parts were built (`docs/adr/2026-11-23-bare-pr-numbers.md`, `docs/adr/2026-11-23-countdown-bars.md`).

## What it waits on (read from the code, 2026-11-23; not yet measured on a real sandbox)

Opening it posts `/api/conversation/new/options` (`app.js` `openFresh`, handler `conversationNewOptions` in
`internal/companion/newconversation.go`), and the button waits for the answer. The handler does, one after another:

1. **On the host:** the review (a file) and `repositoryHere`, which reads the folder's git status afresh (`readGit`, not the state's
   cached one).
2. **`s.wake`** (`readyForTurn`): always one `sbx ls` (`runningNow`), even when vibe already knows the sandbox is up; starting it if not
   (up to 3 minutes).
3. **`clipboardOffer`:** an HTTP call to the clipboard page (the dialog asks again every 3 s, up to 20 times, while it may be starting).
4. **`branchOffer`:** `sbx exec … git` three times in turn (`status`, `branch --show-current`, `for-each-ref`), four when the default
   branch was set locally (`rev-parse`); the default branch itself is read on the host.

So with the sandbox up: one `sbx ls` and three or four `sbx exec`s, one after another. At 0.2–1.3 s each that is 1–5 s, and much more
when sbx is slow (the keyring: see `minimise-sbx-calls`).

## Measuring

Built 2026-11-23: when the options take over 2 s, vibe's log says where the time went, e.g. "companion page: New conversation's options
took 6s: the review and the folder's git status on the host 0s, waking the sandbox 3s, the clipboard page 0s, git in the sandbox 3s (3
runs)". To do: open the dialog on a real sandbox, awake and asleep, and read those lines.

## Likely wins (to confirm by the measurements)

- Skip `sbx ls` when vibe knows the sandbox is up (a session attached, or a turn just ran).
- One `sbx exec` (`sh -c`) for all the git questions, rather than three or four.
- The clipboard page and git at the same time, not in turn.
- `repositoryHere` from the state's cached git status.
- Whatever stays slow, the dialog says what it is waiting for ("waking the sandbox…", "asking git…"), not just a disabled button.
