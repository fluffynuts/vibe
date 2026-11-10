# The Review tab: reviews live on the host, in the companion page

- **Status:** accepted, built 2026-10-15/16, and grown in real use; the stale-diff note 2026-11-09. What was left over is in
  `docs/plan-review-tab-enhancements.md`; stopping the agent while it writes a review is still its own plan, `docs/plan-stop-ends-review.md`.
- **Decided:** 2026-10-15, with the user, who has used it for real reviews since.
- **Came from:** `docs/plan-review-tab.md` (`git show a7461ba:docs/plan-review-tab.md`) and the built part of
  `docs/plan-review-stale-diff.md` (`git show 10924fd:docs/plan-review-stale-diff.md`), both now deleted.

## Context

diffity, run inside the sandbox, had been unreliable in ways that come from where it runs: its database lived in the sandbox (a rebuild wiped
reviews and replies); its server died with the sandbox's idle stop, and with the turn that started it when the next prompt was sent; its
interactive "Ask" needed an agent waiting in a background loop that the page's turn-per-prompt model ends at every prompt; and its page, on a
port inside the sandbox, needed a forwarder, a published port and a 257-line hook rewriting its URLs for the agent. The companion page already
had what diffity lacked: it runs on the host with a stable port and token, keeps its state in `~/.vibe` (surviving rebuilds), and drives the
agent a turn at a time, reliably.

## Decision

A **Review** tab on the companion page. diffity stayed installed beside it until it had proven itself, then was removed (2026-10-16;
`library.Retired` names it, so a profile that still lists it is settled without it, with a note).

- **The diff is worked out on the host**, from the same folder path the sandbox has: the working tree with untracked files, or against a branch
  (from the merge base), or one commit, or the commits not yet pushed. It is read with **go-git in a helper process** (vibe itself, held to a
  memory limit and stopped when its time is up), not the git command: the sandbox can write the repository's config, and git runs what that
  names (hooks, filters, signature checks, fetches). go-git runs nothing, and needs no git on the host. Files, hunks and lines carry both line
  numbers.
- **Storage has one writer.** `~/.vibe/reviews/<sandbox>/review.json` holds the review (what it is of, its state, the threads, the reading order,
  the files ticked as viewed); the companion is its only writer. The agent's command doesn't write it: it drops requests (`comment`, `reply`,
  `resolve`, …) as files into an `inbox/` mounted into the sandbox, which the companion takes in, so nothing races on a file and a request made
  while vibe isn't running waits. The `view/` folder is read-only to the sandbox. Reviews survive the sandbox stopping, a rebuild, and vibe
  restarting.
- **Anchoring.** A comment is anchored to a file, side and line, with the text of the lines it was made on. When a diff is worked out, a comment on
  the new side whose code is no longer at its line is looked for in the file (exact, then ignoring indentation and spacing; the nearest copy if
  there are several); found, it moves there for good and says "moved from line N"; not found, it stays, marked "the code has changed since". A
  comment on a line the diff doesn't show is drawn under that line anyway, with the lines around it opened; one on a file the diff lacks is
  drawn under "On files not in this diff".
- **Ask is a turn.** "Ask the agent" on a thread sends an ordinary turn through the conversation, with the thread (file, lines, code, what was
  said) and the question, telling the agent to answer with the review command's `reply`; the answer lands in the thread. There is no listening
  loop. **Submit to agent** sends all the unsent comments as one turn and goes to the Conversation tab.
- **The agent's side** is `vibe-review` in the sandbox (`start`, `comment`, `general`, `reply`, `resolve`, `dismiss`, `order`, `done`, `diff`:
  the same diff the tab shows, so the agent reviews what the user sees) and a skill of vibe's own, as the `review` base feature. The tab is on
  the page the user already has open, so the agent gives no address.
- **Grown in use (2026-10-16):** a **+** on lines opened from a gap; comments on line ranges (click one **+**, Shift-click another; dragging a **+**
  moves the comment being written); **Viewed** ticks that fold a file away, unticked, saying so, if its change changes; a formatting toolbar and
  Ctrl+B/I/K on every comment box; **editing a sent comment** (the user's own, not the agent's: it shows **edited**, and goes with the next
  Submit as "edited since you saw it … It said: …"; one the agent has seen can't be deleted); **syntax colouring** on the host with Chroma, a
  file side at a time as its card nears the screen (files over 10,000 lines or 1 MB are left plain, saying so), chosen over highlight.js so the
  browser isn't slowed; and a **comments pane**, a fly-out over the right of the diff below the bar, with short cards, filters All/Open/Yours, the
  arrows' order or newest first.
- **The diff is a snapshot, so the page asks when it has gone stale (2026-11-09).** Comments follow their code only when a diff is worked out
  (`?diff=1`, on first show and on Refresh), and the page polls the review alone, and only while the tab shows, so a comment the agent made after
  changing code sat on the wrong line of the older diff until Refresh. When the agent's turn ends, the page asks vibe, **once**, whether the code is still the
  one its diff was of (`GET /api/review/changed?key=`, a key over the files' paths, statuses and fingerprints, `review.DiffKey`, sent with every diff;
  it follows the "hide whitespace changes" setting). The question only looks: it moves no comment and closes no review. If the code changed, then
  with the **Review tab open** a note sits beside Refresh and the button is outlined, the diff left alone (a refresh forgets the gaps opened and the
  place in it); with **another tab open** the diff is dropped and worked out afresh when the tab is next shown (not eagerly in the background, which
  would run git for nobody and could close a review while no one is looking). A turn that changed nothing does neither. Going to a comment on a file the held diff
  lacks asks the same question, and, only if the code changed (or the note is already showing), says so in a **toast with a Refresh of its
  own**, since the comments pane can cover the note beside Refresh.

## Consequences

- Reviews outlive sandboxes and need no forwarding; the cost is that the host does the work (git, colouring) in its own helper process.
- The inbox is how the sandbox reaches the host here, so the host's reading of it is careful by design: paths the agent names are opened as
  `os.Root`s, a `.git` that leads outside the folder is refused, and nothing the repository's own config names is run.
- The page's picture of a review is a snapshot of the diff plus a live review, so its alignment depends on the question above and on Refresh.
- Considered and left: polling the diff (the cost the poll avoids is git on a large repository every few seconds); `reviewAt` as the signal for staleness
  (it can't see a fix committed without touching the review, which the key can).

## Not built

All of this is in `docs/plan-review-tab-enhancements.md`: tabs open on the same review don't sync; a cap on very large diffs; a Windows host never seen;
going to the next unresolved comment; Submit saying why it is disabled; and the whitespace-only fingerprint wrinkle. Submitting to a GitHub pull
request, reverting hunks and guided tours beyond the reading order were put out of the first build and were never asked for since.

## Not verified

- A Windows host (is the review folder's path the same inside the sandbox? does the host have what it needs?).
- The toast with a Refresh button over the fly-out has been checked in jsdom, and a plain toast over the pane in real Firefox; the two together have
  not been looked at by hand.
