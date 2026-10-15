# Plan: a review tab in the companion page

Status: proposed 2026-10-15; the proof of concept's steps 1–7 built 2026-10-15, uncommitted then, tested
with unit tests, real git, the real `vibe-review` script against the real store, and the tab in jsdom; not
yet in a real browser or sandbox. Where: the diff (`internal/review/git.go`, `parse.go`), the store
(`internal/review/store.go`), the server (`internal/companion/review.go`), the tab (`ui/review.js`,
`ui/app.js`), vibe's wiring (`reviewTabFor` in `src/vibe/companion.go`, the mount in `create.go`), and the
agent's side (`library/base/review/`: `vibe-review`, its skill and instructions). Next: use it for real
reviews alongside diffity, and decide.

## Why

diffity, run inside the sandbox, has been unreliable in ways that come from where it runs rather than from
bugs to fix one by one:

| What went wrong | Why | Fixable from vibe? |
|---|---|---|
| Reviews and replies lost | its database is in the sandbox (`~/.diffity/<hash>/reviews.db`); a rebuild wipes it | partly: mount a host folder there |
| The review page dies | its server runs in the sandbox: it goes with sbx's idle stop, and with the turn that started it when the next prompt is sent | only by keeping the sandbox up for it |
| Interactive mode ("Ask") has no one listening | it needs an agent waiting in `diffity agent await`, a background loop the page's turn-per-prompt model ends at every prompt | no: it fights how the page talks to the agent |
| Port trouble | its page is inside the sandbox on 5391: vibe needs a forwarder, a published port, and a 257-line hook rewriting its URLs for the agent | works, but the most fragile part |

The companion page already has what diffity lacks here: it runs on the host, with a stable port and token,
keeps its state in `~/.vibe` (surviving rebuilds), and drives the agent a turn at a time, reliably (the
sandbox started first, kept awake while the agent works, compaction shown).

## The outcome

A **Review** tab on the companion page:

- shows the diff of the sandbox's folder: the working tree (with untracked files), or against a branch or
  commit;
- shows review comments inline, on lines of that diff, in threads, plus general comments;
- lets the user reply, resolve and dismiss, and **ask the agent** about a thread: that is an ordinary turn,
  whose answer lands in the thread;
- lets the agent review (leave comments, set a reading order, mark the review in progress and done) with a
  small command in the sandbox, guided by a skill of vibe's own;
- keeps reviews in `~/.vibe/reviews/<sandbox>/`, so they survive the sandbox stopping, being rebuilt, and
  vibe restarting.

diffity stays installed alongside until the tab has proven itself; then the diffity feature goes.

## How

### The diff: on the host

The sandbox's folder is the same path on the host (Linux, macOS; Windows to check). The companion runs
`git` there: `git diff HEAD` plus untracked files (`git ls-files --others --exclude-standard`, each shown as
added) for the working tree; `git diff <ref>...` (merge base) for a branch; `git show` for a commit. The
unified diff is parsed into files, hunks and lines with old and new line numbers. No forwarding, nothing
that dies with the sandbox. Needs git on the host: say so plainly when it's missing.

### Storage: one writer

`~/.vibe/reviews/<sandbox>/review.json` holds the review: what it's of (the ref), its state (in progress,
done), threads (file, line range, side, status, comments with author and time), general comments, and the
reading order. The companion is its only writer, as the clipboard server is for its history.

The agent's command doesn't write it: it drops requests (`comment`, `reply`, `resolve`, ...) as files into
`~/.vibe/reviews/<sandbox>/inbox/`, mounted into the sandbox, which the companion takes in. Nothing races on
one file, and a request made while vibe isn't running waits for it.

### Anchoring

A comment is anchored to file, side and line, with the text of the line(s) it was made on. When the diff
changes, a comment whose lines no longer match is shown as out of date, at the nearest place it can be, not
dropped.

### Ask: a turn

"Ask the agent" on a thread sends a turn through the conversation, with the thread (file, lines, the code,
what was said) and the user's question, telling the agent to answer with the review command's `reply`. The
answer shows in the thread; the turn shows in the conversation as any other. No listening loop.

### The agent's side

- `vibe-review` in the sandbox (a small node script, as the clipboard's are): `start`, `comment`,
  `general`, `reply`, `resolve`, `dismiss`, `order`, `done`, and `diff` (the unified diff the tab shows, so
  the agent reviews what the user sees: fetched from the companion? or run in the sandbox: same folder,
  same git).
- A `vibe-review` skill replacing diffity-review: what to look for, severities, how to comment, the reading
  order, and to finish with `done`.
- The agent is told the tab's address the way it's told the companion's now.

## Not in the proof of concept

Syntax highlighting (the largest single cost), submitting to a GitHub pull request, a file-tree browser,
reverting hunks, guided tours beyond the reading order, side-by-side view.

## Order of work

1. **The diff, on the host.** `internal/review`: run git, parse the unified diff (files, hunks, lines, both
   line numbers; added, deleted, renamed, binary files), untracked files as added. Tested against real git
   repos made in the tests.
2. **The store.** `review.json` and the inbox: reading, taking in requests, writing atomically. Tests.
3. **The tab, read-only.** An API for the diff and the review; the tab shows files, hunks and comments.
4. **Replying, resolving, dismissing** from the tab.
5. **The agent's command and skill**, the inbox mounted into the sandbox (a rebuild needed, as for the
   clipboard).
6. **Ask as a turn.**
7. **Out-of-date comments**, then use it for real reviews alongside diffity, and decide.

## Open questions

- Windows: is the sandbox's folder the same path, and does the host have git? (The tab says so if not.)
- Large diffs: a cap on what's shown at once, with files collapsed past a size.
- The user's global instructions name diffity for reviews: they change once the tab takes over.

## Testing it (notes for the first real try)

- Run the branch's own `./vibe`, not the installed release: that's where the tab and `vibe-review` are.
- The test sandbox needs rebuilding for the agent's side (the review folder mounted, `vibe-review` and its
  skill): the new base feature makes its profile out of date, so the session-start update check offers it.
  A profile edited by hand isn't re-composed automatically: then `vibe -C` (if `vibe-review` isn't found).
- Feature branch: `vibe-review start` reviews uncommitted changes unless given `--ref <base branch>`; ask
  for "a review of this branch against main", or the review may come back empty.
- A sandbox with the diffity feature tells the agent, every prompt, to review with diffity (its hook's
  UserPromptSubmit context, and its agent instructions), against the review feature's instructions. Until
  diffity goes, say "use vibe-review" when asking.
- Not yet seen anywhere: a Windows host (is the review folder's path the same inside the sandbox?).
