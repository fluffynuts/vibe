# From review to pull request: the next unresolved comment, Submit saying why, and the branch's pull request and its status in the top bar

- **Status:** accepted, built 2026-11-25/26 on `feat/review-tab-pr-link` (PR #41). Tried by hand: the pull request link, and its
  dot (a red ring for PR #41, a draft whose last checks failed; yellow while CI ran, green after); the arrows, the count, Ctrl+Enter in
  open, resolved and empty reply boxes, Resolve and Dismiss going on (and the toast with none left), the going-on setting off, and
  Submit disabled with its reason while the agent answered. The reviewers-and-conversations dot on a work pull request (2026-11-26):
  pulsing yellow and pulsing red while checks ran (red pulsing as a cancelled check was re-run: something failed, and checks are
  running again).
- **Decided:** with the user: the first two raised 2026-10-22 ("unresolved" is status open, decided then), the link 2026-11-25.
- **Came from:** `docs/plan-review-tab-enhancements.md` (`git show 0bf625a:docs/plan-review-tab-enhancements.md`).

## The arrows go to the unresolved comments

**Context:** Ctrl+Enter after a reply, and the ▲/▼ arrows, walked every thread, resolved or not: once most were resolved, the user
scrolled past them.

- **The arrows and going on after a reply** visit only unresolved (open) threads, in the order chosen (path or the agent's reading
  order), round at the ends. The bar says how many are left: "3 unresolved" (its title "3 unresolved of 8 comments"), "no unresolved comments" with the arrows off. It said where the page was among them first ("2/3 unresolved comments", then "at 2 of 3 unresolved"): the user found it read as how many were unresolved, followed the scroll so was often a guess, and left out the general comment the page starts at; the total is what is watched. The arrows still go on from where the page is. The comments
  pane still lists every thread.
- **Ctrl+Enter in an empty reply box goes on** to the next unresolved comment, sending nothing (it couldn't send an empty reply anyway);
  the box's hint says so. Going on, by either, says when there's no other unresolved comment, rather than landing on a resolved one.
- **Resolve and Dismiss go on too**, with "go on after reply" ticked, to the next unresolved comment: the thread they leave is one the
  arrows pass over (the user, trying it). Unticked, the page stays; Reopen always stays, as a thread is reopened to work on.
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
- **Asked of the host's gh** (`gh api graphql`, the query in `src/vibe/branchpr.go`: the repository's open pull requests whose head
  branch has this name), run in an empty
  folder of its own, never the project's: there gh would run git, and the project's `.git/config` can be written from inside the sandbox
  (`core.fsmonitor` and the like run commands on this machine). The repository and branch come from vibe's own read (go-git, which runs
  nothing).
- **In the background**, never waited on: asked when the branch changes, then every minute (two at first; the user asked for one, 2026-11-26). Nothing is asked on a detached
  HEAD, outside GitHub, or without gh; a failure (not signed in, say) is logged once, not at every ask.
- **The top bar is drawn again only when it changes**: drawn at every poll, the link lost keyboard focus.
- **A dot before the link says its status**, decided with the user 2026-11-26, conservatively ("start strict, dog-food, revise"):
  - **red, something needs you:** a check failed (failed, timed out, cancelled, needing action; a commit status erroring); anyone's
    latest review requests changes; it conflicts with its base; or a review is required and nobody is asked (no one will approve it).
  - **yellow, on its way:** a check still running, and then the dot **pulses** (not with reduced motion: the title says it); or,
    steady: a review asked for and not given (each person asked counts on their own, so two people from one team both must approve;
    a team asked as a team is done when one member reviews, as GitHub has it); a person whose latest review only commented (a sign-off
    is an approval: a reviewer who only commented, and a passer-by's comment, both hold it yellow); any review conversation unresolved,
    whoever started it, even under an approval; a review still required; GitHub not yet sure it merges; more conversations than are
    read (100), said rather than guessed.
  - **green, good to go:** every check passed, every person who reviewed approved, every agent has reviewed, no conversation is
    unresolved, nothing is asked for. With nobody asked and no review required (this repository: the user alone), checks passing is
    enough.
  - **People and agents** are told apart by account type (`Bot`: Copilot's reviewer, any GitHub App), not by a list of names. An agent
    never approves (Copilot's reviews are comments), so it is done once it has reviewed and its conversations are resolved: the user
    resolves an agent's comment when they have dealt with it.
  - It comes from one `gh api graphql` request (as `gh pr list` can't give the conversations): the open pull requests from the branch,
    with checks, each reviewer's latest review, the reviews asked for, and the conversations. The title and a screen reader's label
    say the status in words, with why ("waiting on @bob; 1 conversation unresolved (copilot-pull-request-reviewer)").
- **A draft has a ring, not a dot**, in its status's colour: a draft is often opened to watch the checks, so the colour stays, and the
  shape says draft (fading the dot was thought of: 80% is hard to tell apart, and reads as disabled). Its title says "draft".
- **Asked every 30 s while a check is running**, so the dot turns green or red soon after the checks end; every minute otherwise,
  including while it is yellow only for a review waited for (news then comes when someone acts: the user's call, review #149). It asks
  GitHub, not sbx, and GitHub's limits are far above that.

## A fork's pull request isn't this branch's

A branch is matched by its name alone (`--head`, and the query's `headRefName`): on golang/go, `master` listed three pull requests, all from forks. So up to 20
are listed, and the first from this repository (`isCrossRepository` false) is the branch's; with none, there's no link. Found in review
(#146), where the first build's ADR had called it "not seen".
