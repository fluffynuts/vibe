# A pull request or issue the agent names links to it on GitHub

- **Status:** accepted, built 2026-11-20 on `feat/status-balloon` (shipped with the status balloon, as "UI enhancements").
- **Decided:** 2026-11-20, with the user: seen in another sandbox's conversation, where "PR #110" was a link to Review comment #110.
  Rather than only stopping that (a smaller first step), the references link to GitHub, in one go.

## Context

The conversation makes every `#N` (not in a word) a link to Review comment N. Agents name pull requests the same way ("PR #110"), so
those became links to comments that weren't there, or worse, to a comment that happened to have the number.

## Decision

- **"PR #110", "PR#110", "pull request #110", "issue #12"** (and the plurals) are pull requests and issues, never Review comments. A bare
  `#5` is still a comment.
- **Linked to GitHub** (`…/pull/110`, `…/issues/12`, in a new tab) when the repository is there. Its address comes with the folder's git
  status, read on the host in the isolated helper as the rest of it is (`review.FolderStatus.Web`): the `upstream` remote's, when that is
  on GitHub (in a fork, origin is the fork and the pull requests are upstream's), else `origin`'s. Any form of URL git takes (https, with or
  without credentials, `git@github.com:`, `ssh://`), owner and name only in the characters GitHub allows; the page takes only a plain
  `https://github.com/owner/name` too, as the links are made of it.
- **Elsewhere** (no remote, another host), plain text. A reference drawn before the page knew the address is linked when it does.

## Not built

GitHub Enterprise and other hosts (GitLab's `!12` merge requests); a bare `#110` that means a pull request (it stays a comment: only the
words before it tell them apart); in a list such as "PRs #3 and #4", only the first is taken for a pull request: the rest are still Review comments.

## Not verified

By hand in a sandbox whose remote is a fork with an upstream.
