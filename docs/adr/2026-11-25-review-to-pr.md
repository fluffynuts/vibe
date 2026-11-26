# From review to pull request: the next unresolved comment, Submit saying why, and the branch's pull request and its status in the top bar

- **Status:** accepted, built 2026-11-25/26 on `feat/review-tab-pr-link` (PR #41). Tried by hand: the pull request link, and its
  dot (a red ring for PR #41, a draft whose last checks failed). The arrows and Submit not yet.
- **Decided:** with the user: the first two raised 2026-10-22 ("unresolved" is status open, decided then), the link 2026-11-25.
- **Came from:** `docs/plan-review-tab-enhancements.md` (`git show 0bf625a:docs/plan-review-tab-enhancements.md`).

## The arrows go to the unresolved comments

**Context:** Ctrl+Enter after a reply, and the ▲/▼ arrows, walked every thread, resolved or not: once most were resolved, the user
scrolled past them.

- **The arrows and going on after a reply** visit only unresolved (open) threads, in the order chosen (path or the agent's reading
  order), round at the ends. The bar counts them: "2/3 unresolved comments", "no unresolved comments" (the arrows then off). The comments
  pane still lists every thread.
- **Ctrl+Enter in an empty reply box goes on** to the next unresolved comment, sending nothing (it couldn't send an empty reply anyway);
  the box's hint says so. Going on, by either, says when there's no other unresolved comment, rather than landing on a resolved one.
- Tests that stepped through threads used a fixture with one resolved: they now step through an all-open copy (they are of how the
  arrows step), and the count's wording is "unresolved comments".

## Submit says why it can't

**Context:** the host refuses a submit while the agent is answering, but the button looked usable; the user only learned why after
pressing it.

- **Submit to agent is disabled while the agent answers**, its count kept ("Submit 2 to agent"), its title saying the comments are kept
  for once it has finished; it is usable again the moment the answer ends (drawn again when the conversation's poll says so).
- **The host still refuses** (a second tab, a click in between), and its refusal names what is kept: "your 2 comments are kept, and Submit
  sends them once it's done".

## The branch's pull request in the top bar

- **"PR #N", a part of the top bar of its own** after the git status, past a divider as the folder and the status are: the pull request
  open for the current branch, opening on GitHub in a tab of its own. Only a pull request's address on GitHub is linked.
- **Asked of the host's gh**, `gh pr list --repo=owner/name --head=<branch> --state=open --json=number,url,isCrossRepository,isDraft,reviewDecision,mergeable,statusCheckRollup --limit=20`, run in an empty
  folder of its own, never the project's: there gh would run git, and the project's `.git/config` can be written from inside the sandbox
  (`core.fsmonitor` and the like run commands on this machine). The repository and branch come from vibe's own read (go-git, which runs
  nothing).
- **In the background**, never waited on: asked when the branch changes, then every minute (two at first; the user asked for one, 2026-11-26). Nothing is asked on a detached
  HEAD, outside GitHub, or without gh; a failure (not signed in, say) is logged once, not at every ask.
- **The top bar is drawn again only when it changes**: drawn at every poll, the link lost keyboard focus.
- **A dot before the link says its status** (the user, 2026-11-26): red when a check failed (failed, timed out, cancelled, needing
  action; a commit status erroring), a review asked for changes, or it conflicts with its base; yellow when a check is still running, a
  review is required and not given, or GitHub hasn't worked out whether it merges; else green. The same gh call carries it
  (`isDraft,reviewDecision,mergeable,statusCheckRollup`), worked out on the host (`PullRequestChecks.Status`). Its title, and its label for
  a screen reader, say it in words, with why ("test (windows-latest) failed"): colour alone says nothing to some.
- **A draft has a ring, not a dot**, in its status's colour: a draft is often opened to watch the checks, so the colour stays, and the
  shape says draft (fading the dot was thought of: 80% is hard to tell apart, and reads as disabled). Its title says "draft".
- **Asked every 30 s while yellow**, so the dot turns green or red soon after the checks end; every minute otherwise. It asks GitHub,
  not sbx, and GitHub's limits are far above that.

## A fork's pull request isn't this branch's

`--head` matches a branch by its name alone: on golang/go, `--head=master` listed three pull requests, all from forks. So up to 20
are listed, and the first from this repository (`isCrossRepository` false) is the branch's; with none, there's no link. Found in review
(#146), where the first build's ADR had called it "not seen".
