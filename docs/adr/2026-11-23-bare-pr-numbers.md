# A bare #N that is a pull request links to it on GitHub

- **Status:** accepted, built 2026-11-23 on `feat/bare-pr-numbers`. Not yet tried by hand in a sandbox.
- **Decided:** 2026-11-23, with the user: another sandbox's agent wrote "#123 is open, MERGEABLE" and "You merge #123", meaning pull
  request 123, and the page drew both as links to Review comment 123. Amends `2026-11-20-pr-and-issue-links.md`, which left every bare
  `#N` a comment ("only the words before it tell them apart").
- **Came from:** `docs/plan-bare-pr-numbers.md` (`git show 24c1c9e:docs/plan-bare-pr-numbers.md`), whose last part is still a plan:
  `docs/plan-new-conversation-slow.md`.

## Decision

- **A bare `#N` is checked, not guessed**, in this order: a comment on the open review → a link to it on the Review tab; else a pull
  request in the repository → a link to it on GitHub (`…/pull/N`, new tab), as "PR #N" is; else a comment on a closed review → a link
  that says the review is closed; else plain text. (It used to be a button whatever the number, which then said there was no such comment.)
- **Both a comment and a pull request:** the comment wins on an open review (the review skill tells agents to write comment numbers so); its
  tooltip says there is a pull request #N too. On a closed review the pull request wins.
- **Issues are left out:** a bare `#N` that is only an issue is plain text. "issue #N" links as before.
- **Which comments there are** comes with the page's state (`reviewThreads`, `reviewClosed`), read where the state already reads the
  review: the Conversation tab doesn't load the review to know.
- **Which pull requests there are:** `git ls-remote https://github.com/O/R.git 'refs/pull/*/head'` in the sandbox (`sbx exec`, its
  own access: a read-only token is enough, a public repository needs none; `gh` isn't needed), for the repository the links already use
  (`review.FolderStatus.Web`). One call lists every pull request: 0.7 s, for 5,867 of them (golang/go).
- **Never waiting on sbx:** only a running sandbox is asked (`sbx exec` would start a stopped one: one `sbx ls` first). The host keeps
  the list (`GET /api/pull-requests`); the page draws a bare number as it can without it and relinks it when the list comes (the state
  carries the list's version). A number the list lacks asks the host, which asks the sandbox in the background at most once a minute.
  A failed ask is said (`unavailable`) and logged, not retried at once; a list already read is kept through a failure. A stopped sandbox
  isn't a failure, and isn't logged.
- **The agent is told** to write "PR #N" and "issue #N", never a bare `#N`: a line in what an agent with reviews always loads
  (`instructions.WithPullRequestRule`), and in the vibe-review skill. Agents don't always follow it, so it complements the check.

## Not built

A fork whose upstream the sandbox's token can't reach: the list is upstream's (as the links are), so such a fork's bare numbers stay plain.

## Not verified

By hand in a sandbox; with a fork and its upstream.
