# A bare #N that is a pull request links to it on GitHub (planned, not built)

Raised 2026-11-23, with the user, from another sandbox: the agent wrote "#123 is open, MERGEABLE" and "You merge #123", meaning pull
request 123, and the page drew both as links to Review comment 123. It follows `docs/adr/2026-11-20-pr-and-issue-links.md`, which links
"PR #N" and "issue #N" to GitHub but leaves every bare `#N` a Review comment ("only the words before it tell them apart"). Its own
branch, after `feat/git-token-next-steps` is merged (decided: that branch is about tokens, and is big enough).

## Decided

1. **A bare `#N` is checked, not guessed.** In order:
   - a comment on the open review → a link to it on the Review tab, as now;
   - else a pull request in the repository → a link to it on GitHub (`…/pull/N`, new tab), as "PR #N" is;
   - else, or when the check couldn't be made or failed for any reason (no token or access, sandbox stopped, an error, sbx slow), plain
     text. (Today a number no comment has is still a button, which then says the comment isn't there.)
2. **Issues are left out:** a bare `#N` that is an issue stays plain ("I don't refer to issues a lot"). "issue #N" links as now.
3. **Both a comment and a pull request:** the comment wins (that is how the review skill tells agents to write comment numbers); its
   title says pull request #N exists too.
4. **The agent is told** to write pull requests as "PR #N", as a bare `#N` is a Review comment on the page: one line in the
   instructions vibe gives the agent, and in `library/base/review/agent-files/.claude/skills/vibe-review/SKILL.md` beside "write its
   number as the tab shows it". Cheap, but agents don't always follow it, so it complements the check rather than replacing it.

## How (proposed)

- **Which pull requests there are:** `git ls-remote https://github.com/O/R.git 'refs/pull/*/head'` inside the sandbox (`sbx exec`,
  `GIT_TERMINAL_PROMPT=0`, as the push check runs), with the sandbox's own access: one call lists every pull request's number; a
  read-only token is enough, a public repository needs none, and `gh` isn't needed (it isn't in every sandbox). The repository is the
  one the links already use (`review.FolderStatus.Web`: upstream's, else origin's).
- **Never waiting on sbx** (`minimise-sbx-calls`): only a running sandbox is asked (`sbx exec` would start a stopped one). The host
  keeps the list per sandbox; the page draws a bare number first as it would without the list (a comment link, or plain), and links it
  when the list comes, as it does with the repository's address (`setRepoWeb`/`linkRepoRef`). A number not in the list asks again, at
  most once a minute, in the background; a failed ask is remembered as failed (plain), not retried in a loop, and logged, not swallowed.
- **Where it shows:** the conversation's text (`withThreadRefs`/`withCommentRefs`), and wherever else those draw (review comments).

## Tests (to write first)

- The page: a bare `#N` with a comment → comment link; with a pull request and no comment → GitHub link; both → comment, title naming
  the pull request; neither, or no list (a failed check) → plain; the list arriving after drawing links what was drawn.
- The host: the `ls-remote` output parsed into numbers (`refs/pull/12/head`, and the `merge` refs ignored); a stopped sandbox not asked;
  a failure kept and said, not retried at once; the ask rate-limited.
- The instruction line present in what the agent is given.

## Open

- A big repository's `ls-remote` lists thousands of refs: fine for one call a minute, but measure it on one.
- A fork with an upstream: the pull requests are upstream's, which the sandbox's token may not reach (a public upstream needs none).

# Also on that branch: a countdown bar on what closes itself (planned, not built)

Raised 2026-11-23, with the user: a 2px line inside the top edge of anything that dismisses itself, shrinking to nothing as its time
runs out (a reversed Turbo/GitHub navigation bar), so the user can tell it is counting down and will go.

## Decided

1. **Where:** the page's toasts (`toast()`, 4 s / 8 s for a problem / 10 s with an action), the status balloon when it popped up of
   itself (`closeStatusLater`), and the clipboard page's toasts (`clipboard-page.html`, `library/`: hand-testing needs `./test-clean`).
2. **Only what will close:** no bar on a toast that stays (`TOAST_STAYS`), a balloon the user opened, or one with a warning.
3. **Held, it stops:** hovering or focusing holds it, the bar shown full; let go, timer and bar start again from full (as the page's
   toasts' timer does now). The balloon and the clipboard page's toasts gain the hold (decided: yes, both).
4. **One length:** the bar's duration comes from the number the timer uses (a CSS variable set with it), so they can't disagree.
5. **Reduced motion:** still shown (a slow linear shrink, informative); a still line instead if the user says so.

## Tests

jsdom: the bar is there with the timer's duration; absent where nothing will close; restarted after a hold; the balloon and the
clipboard toasts held while hovered. Animations don't run in jsdom: the look is checked by hand.

# Also on that branch: the balloon closes when its list empties (decided, not built)

Raised 2026-11-23 by the user: background work ended while the balloon was open (opened by the user, to look), and it stayed open
saying only "Work the agent is doing in the background will appear here." That followed `docs/adr/2026-11-18-status-balloon.md`
("opened by the user, it stays until closed"), but contradicts its own rule for Stop (the balloon closes once the work has gone,
"left empty, the balloon would only say what it is for").

- **Decided:** whenever the list goes from something to nothing, the balloon closes, however it was opened; keyboard focus inside
  it goes back to its button (as after Stop). Opened while already empty, it still shows the line saying what it is for.
- **Where:** `setStatusItems` in `app.js`; one line of the ADR changes.
- **Test (page):** opened by the user with a task running, the task ending closes it; opened with nothing running, the line shows.
- Designed with the countdown bar above: both are about the balloon closing of itself.

# Also on that branch: why "New conversation" is slow to become ready (to investigate, not decided)

Raised 2026-11-23 by the user: the dialog shows at once, but its button takes "quite a while" to become active, and the page
doesn't say what it is waiting for.

## What it waits on (read from the code, not measured)

Opening it posts `/api/conversation/new/options` (`app.js` `openFresh`, handler `conversationNewOptions` in
`internal/companion/newconversation.go`), and the button waits for the answer. The handler does, one after another:

1. `s.wake`: wakes the sandbox if it sleeps (up to 3 minutes); whether it asks sbx even when vibe already knows it is running is to
   check (sbx can be very slow: `minimise-sbx-calls`).
2. `clipboardOffer`: asks the clipboard page (the page side retries every 3 s, up to 20 times, while it may be starting).
3. `branchOffer`: asks the sandbox's git (`SandboxGit`): how many `sbx exec`s that is, to count.

## To do

- Measure each step on a real sandbox (awake, and asleep), with timings in vibe's log, before changing anything.
- Likely wins, to confirm by the measurements: skip asking sbx when vibe knows the sandbox is up; ask the clipboard page and git at
  the same time rather than in turn; one `sbx exec` for all the git questions; and answer from what vibe already knows (as the
  branch in the bar does) where it can.
- Whatever stays slow, the dialog should say what it is waiting for ("waking the sandbox…", "asking git…"), not just a disabled
  button: nobody should wonder whether it is working.
