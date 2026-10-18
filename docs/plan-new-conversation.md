# Plan: "New conversation" as a blank-slate start

Status: agreed 2026-10-18; built the same day (uncommitted).

## What it's for

"New conversation" used to clear the conversation and nothing else. Pastes stayed waiting to go with the next message,
and an open review stayed open. Now its confirmation also offers to tidy those up and to start a new branch, so a new
conversation can begin from a clean state.

## The dialog

Each row shows only when it applies.

1. **Clear the clipboard** (ticked). Clears the items already sent to the agent. Shown when the clipboard page has
   items.
   - **Clear unsent items too** (indented, ticked). Shown only when there are pastes waiting to go with the next
     message.
2. **Close the open review** (ticked). Shown when a review is open. It's closed, not its threads dismissed, so it can be
   reopened. The reason given is "Closed for a new conversation."
3. **Start a new branch** (unticked). Shown when the folder is a git repository. Ticking it shows:
   - The branch name, pre-filled with `feat/` and the cursor at the end.
   - **Branch from here** (the default) / **Branch from origin/\<default\>**.
     - "From origin/\<default\>" is hidden when you're already on the default branch.
     - It's disabled when the tree is dirty, with "The current branch has uncommitted changes."
     - It carries a note: switching to its files changes them under anything else using the folder (an editor, or an
       agent in the terminal).
   - When the tree is dirty: "Your uncommitted changes will come with you to the new branch. Commit them there before
     switching back, or they'll follow you again."
   - When the default branch can't be told, the option is hidden and the dialog says why, offering to set it (see
     "Setting the default branch" below).
   - A merge in progress with conflicts: no branching at all, saying why.

## Setting the default branch

A repository's default branch is what `origin/HEAD` names. No guessing `main` or `master`. Both the dialog and the
Review tab use the same lookup (`review.DefaultBranch`, read on the host with go-git).

When origin can't say (no `origin` remote, as in a repository just made with `git init`, or `origin/HEAD` not set),
both say why and offer to set it: "Set it for this repository:", with a box and **Set**.

- **What origin says wins.** A setting is used only while origin can't say, and setting one is refused while it can.
- **Where it's kept:** in the repository's git config, as `vibe.defaultBranch`, written by git in the sandbox. vibe
  reads it on the host only as a branch name, once it's a valid one: the sandbox can write that file.
- **The name is remembered** in `~/.vibe/settings.yaml` (`defaultBranch`) and suggested next time, in any repository.
  The last one set replaces it, so someone moving from `master` to `main` gets `main` suggested from then on. The
  repository's branches are offered too, when it has any. A brand-new repository has none, so any valid name can be
  typed.
- **Branching from a set default branch** starts from the branch here (it's labelled "master (local)"). There's no
  copy at origin, so no fetch and no "update it too".
- **A set default branch with no commits yet** (just named, in a new repository): branching from it, and reviewing
  against it, are shown but disabled, saying "master has no commits yet". "Start a new branch" from here still works:
  git renames the branch the first commit will make.

## Origin and the local default branch

- Once "Start a new branch" is ticked and "from origin/\<default\>" is on offer, the dialog fetches the default branch
  from origin ("Checking whether origin/master is up to date…"). It fetches once per opening of the dialog. Cancelling
  the dialog cancels the fetch, and the fetch has a 2-minute backstop.
- **The fetch failed** (no token in the sandbox, no network, …): "Couldn't check whether origin/master is up to date:
  \<git's reason\>. Check it yourself." Branching from origin/master still works, from what was last fetched.
- **Local master is behind origin:** "Local master is N commits behind origin: update it too?", ticked. Done as a
  fast-forward only, with no network (`git fetch . refs/remotes/origin/master:refs/heads/master`), which also refuses
  a branch checked out in another worktree.
- **Local master has commits that aren't on origin** (ahead or diverged): a note, and local master is left as it is.
- **Up to date:** nothing is shown.
- The new branch doesn't track origin/master (`--no-track`): it gets its own upstream when it's first pushed, as after
  `git checkout -b`.

## Where git runs

The host reads the repository with go-git, which runs nothing the sandbox could have planted (hooks, config).
Changing the repository (fetching, fast-forwarding, branching) runs as real git **inside the sandbox**, with
`sbx exec`, where hooks can do nothing the sandbox can't already. That also gives the fetch the sandbox's network and
credentials. Git runs with `GIT_TERMINAL_PROMPT=0` and `ssh -o BatchMode=yes`, so a missing credential fails rather
than waiting for an answer nobody can give.

## The sandbox asleep

Opening the dialog starts the sandbox if it's stopped, as sending a prompt does: a new conversation is about to be
had. That includes a sandbox stopped on purpose (the Admin tab's Stop, `vibe --stop`), which the page coming into view
leaves stopped. The clipboard and branch rows say they're waiting for it, and **Start a new conversation** waits too:
it isn't known yet what those rows will offer. If the sandbox can't be started, those two rows say why. The review row
needs only the host, so it always works.

Enter in the branch name does nothing: more of the form follows it, so you click **Start a new conversation**.

## What happens on confirm

All in one request, in this order:

1. **Branch:** check the name (`git check-ref-format --branch`, and that it doesn't exist yet). For "from
   origin/\<default\>", check again that the tree is clean, since it may have changed since the dialog opened. Then
   fast-forward the local default branch if asked, and create the branch. A failure here stops everything else, and
   the dialog stays open showing why.
2. **Close the review.**
3. **Clear the clipboard.**
4. **Reset the conversation.**
5. The fresh conversation opens with a note of what was done ("Started on new branch `feat/x`, from origin/master
   (local master brought up to date first). Closed the review. Cleared the clipboard."). Anything in steps 2–3 that
   failed is reported there as an error, not hidden.

A request with no options (`{}`) does what "New conversation" always did: it resets the conversation alone.

## Out of scope this round

- The milestone nudge (`docs/plan-milestone.md`, "The page" item 3).
- Remembering the dialog's choices between uses: it opens with the defaults each time.
