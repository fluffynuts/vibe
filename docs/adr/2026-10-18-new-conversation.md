# "New conversation" is a blank-slate start, and can begin a branch

- **Status:** accepted, built 2026-10-18 (the same day it was agreed).
- **Decided:** 2026-10-18, with the user.
- **Came from:** `docs/plan-new-conversation.md`, now deleted: `git show bbbb9ba:docs/plan-new-conversation.md`.

## Context

"New conversation" used to clear the conversation and nothing else. Pastes stayed waiting to go with the next message, an open review stayed
open, and the user starting afresh on a new piece of work still had to make a branch for it by hand.

## Decision

The confirmation also offers to tidy up and to branch, one row each, shown only when it applies:

1. **Clear the clipboard** (ticked), the items already sent; and, indented, **clear unsent items too** (ticked, shown only when pastes are waiting).
2. **Close the open review** (ticked). It is closed, not its threads dismissed, so it can be reopened ("Closed for a new conversation.").
3. **Start a new branch** (unticked, shown when the folder is a git repository): a name, pre-filled `feat/`; **from here** (the default) or **from
   origin/&lt;default&gt;** (hidden when already on the default branch; disabled with a dirty tree, "The current branch has uncommitted changes."). A dirty
   tree's changes come with the user to the new branch, and the dialog says so. A merge in progress with conflicts allows no branching, and says why.

**One request, in this order:** check the branch name (`git check-ref-format --branch`, and that it doesn't exist; the tree rechecked for "from
origin"), fast-forward the local default branch if asked, create the branch, close the review, clear the clipboard, reset the conversation. A
failure at the branch stops everything else and the dialog stays open showing why. The fresh conversation opens with a note of what was done, and
anything in the review or clipboard steps that failed is reported there as an error, not hidden. A request with no options does what New
conversation always did.

**The default branch is what `origin/HEAD` names; nothing guesses `main` or `master`.** The dialog and the Review tab share one lookup
(`review.DefaultBranch`, read on the host with go-git). When origin can't say (no `origin`, as in a repository just made with `git init`, or
`origin/HEAD` unset), both say why and offer to set it. What origin says wins, and a setting is refused while it can. The setting is kept in the
repository's git config as `vibe.defaultBranch`, written by git in the sandbox and read on the host only as a branch name, once it is a valid
one (the sandbox can write that file). The name is also remembered in `~/.vibe/settings.yaml` (`defaultBranch`) and suggested next time, the last one
set replacing it. A default branch with no commits yet is shown but disabled for reviewing against and branching from, saying so; "start a new
branch" from here still works.

**Origin and the local default branch:** once "from origin/&lt;default&gt;" is on offer the dialog fetches the default branch once per opening
(cancelling cancels it; a 2-minute backstop), and says if the fetch failed (branching from what was last fetched still works). If local is behind
origin it offers, ticked, to update it, as a **fast-forward only** with no network (`git fetch . refs/remotes/origin/master:refs/heads/master`),
which also refuses a branch checked out in another worktree. If local has commits origin doesn't, a note says so and it is left alone. The new
branch doesn't track origin/&lt;default&gt; (`--no-track`), as after `git checkout -b`.

**Where git runs.** The host reads the repository with go-git, which runs nothing the sandbox could have planted (hooks, config). Anything that
changes the repository (fetching, fast-forwarding, branching) runs as real git **inside the sandbox**, via `sbx exec`, where hooks can do
nothing the sandbox can't already, and where the fetch has the sandbox's network and credentials. Git runs with `GIT_TERMINAL_PROMPT=0` and
`ssh -o BatchMode=yes`, so a missing credential fails rather than waiting for an answer nobody can give.

**A sandbox asleep is woken** when the dialog opens, as sending a prompt does, including one stopped on purpose (the page coming into view
leaves such a sandbox stopped). The clipboard and branch rows say they are waiting, and **Start a new conversation** waits too, since it isn't
known what those rows will offer; if the sandbox can't be started, those two rows say why. The review row needs only the host.

## Consequences

- A new piece of work can start from a clean state in one confirmation, with the branch made from where the user wants.
- The host never runs git that the sandbox could have booby-trapped; the price is that branching needs the sandbox up.
- Enter in the branch name does nothing (more of the form follows it).

## Not built, on purpose

- **The milestone nudge** (a safe point to suggest a new conversation) belongs to `docs/plan-milestone.md`, which is not built.
- **Remembering the dialog's choices** between uses: it opens with the defaults each time.
