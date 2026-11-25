# New conversation: Start doesn't wait for the sandbox, and asks it less

- **Status:** accepted, built 2026-11-25 on `feat/bare-pr-numbers`. Tried by hand 2026-11-25: Start usable at once, warm and cold; an early Start from cold cleared the clipboard; cold options took 5-5.9 s (waking 3.3-4 s, git 1.6-1.9 s in one run), from 11.7 s.
- **Decided:** 2026-11-25, with the user.
- **Came from:** `docs/plan-new-conversation-slow.md` (`git show a8233de:docs/plan-new-conversation-slow.md`).

## Context

The dialog showed at once, but its Start button waited for everything it offers: 2.5-5.3 s with the sandbox up, 11.7 s from cold, more when
the clipboard page was still starting. Measured by the user on 2026-11-25 with the log line the plan added (vibe's log says where a slow
options request's time went). Nearly all of it was sbx: waking the sandbox (one `sbx ls` even when it was up, 0.1-1.3 s; 5.4 s to start it)
and git in the sandbox (three `sbx exec`s one after another, 2.3-4.7 s warm, 6.3 s cold). The host's reads and the clipboard page took 0 s.
When the clipboard page didn't answer yet ("connection reset", while it starts with the sandbox), the dialog asked for everything again
every 3 s, each time waking and running git anew.

## Decision

- **Start is usable at once.** The dialog first asks `GET /api/conversation/new/ready`: whether there is a review, a clipboard page and a
  repository, all from this machine. It offers those rows straight away, ticked as ever, the clipboard's counts saying "counting…" and the
  branch row "Checking the repository in the sandbox…" (a branch needs what the sandbox says). Start does what is ticked: starting wakes the
  sandbox anyway, and clears the clipboard once it is up, as before. So an early Start does what a late one would with the defaults.
  The options (`POST /api/conversation/new/options`) fill the rows in when they come.
- **One run of git in the sandbox, not three or four.** The tree's state, the branch and the branches come from one git alias run
  (`git status --porcelain=v2 --branch` and `git for-each-ref`); a set default branch's having commits is read from the branches listed.
  The sandbox is still asked to run git and nothing else. Making a branch reads the tree the same way.
- **The clipboard alone is asked again**, `POST /api/conversation/new/clipboard`, which costs no sbx call: every second, sixty times,
  rather than the whole options every 3 s, twenty times.
- **The log tells a slow start as one story:** the clipboard page not answering the options (and why), answering on a later try, or still
  not by the last; the options' own line says "1 run" of git.
- **An empty clipboard cleared says nothing** in the new conversation (an early Start can ask it to clear one).

## Not done

- **Skipping the `sbx ls` when vibe thinks the sandbox is up.** vibe doesn't keep that, and a guess is wrong exactly when the user has just
  stopped it; with Start no longer waiting, the check only delays the branch row.
- **A server in the sandbox to run commands without sbx** (the user's idea, to get around sbx's slow sign-in checks): weighed and left. It
  saves nothing from cold (it starts with the sandbox), about a second warm, and a command endpoint reachable from the host widens what the
  sandbox guards against. If warm `sbx exec`s still hurt, a few fixed read-only questions on a server already there is the narrow version.
