# Plan: milestone, a safe point to start a new conversation

Status: planned 2026-10-16, not built. Build it on master, after `feat/review-tab` has merged, as its own small release.
Phase 2 (the feature loop, below) builds on it, as a separate release after milestone has had real use.

## Goal

A long session gets summarised as it runs, and each summary drops detail. A new conversation started from good notes
does better, but starting one can lose whatever was only in the conversation. A milestone gets everything that matters
written down, then says plainly whether it's safe to start afresh.

The user can ask for one by saying "milestone" (in the conversation, a terminal session, or headless mode), or by
clicking a button on the Conversation tab. Both run the same steps. The steps are fixed, so none can be forgotten, and
the agent says what it's doing at each one.

## When to use it

- When a change has landed (committed, pushed, CI green), before starting something unrelated.
- During a change only once it has been summarised more than once; otherwise keep going.

## Decisions made

1. **It's a skill**, shipped by vibe like `vibe-review` (`library/base/.../skills/milestone/SKILL.md`), so every sandbox
   has it and its steps are written down rather than remembered.
2. **The agent never commits or pushes in a milestone.** It reports what's uncommitted or unpushed; committing stays
   the user's call.
3. **The agent can't start a new conversation itself.** The milestone gets everything ready, and the user clicks
   "New conversation" (or runs `/clear` in a terminal).
4. **The verdict is read mechanically, not taken from the agent's prose.** The skill ends by running a vibe command
   that records the result: safe, or blocked with reasons. The page reads that record. If the record is missing, the
   page says the milestone didn't finish, never that it's safe.
5. **The page shows the next step:** one button, then the verdict, then the next action. The safe path should be the
   obvious one.

## The skill's steps

The agent says what it's doing at each step.

1. **Where things stand:** uncommitted changes, unpushed commits, the branch, and CI's result for the latest push
   (when it can be read: with `gh`, or `curl` for a public repository's run list. If CI can't be checked, say so; never
   assume it's green).
2. **The work's record:** if a plan doc covers the work (`docs/plan-*.md`, or whatever the conversation named), update
   it: what's done, what's decided and why, and what's open. Don't create one if nothing has been planned.
3. **Memory:** correct memories that are no longer true, remove finished ones, and add what a new conversation must
   know (open items, pending tests, reminders such as `./test-clean`).
4. **A handover:** a short note of the next step and what a new conversation should read first. Where it's kept is
   open (see Open questions).
5. **The verdict:** record it (see Decision 4), then say it. Either "safe to start a new conversation", or what's
   blocking that: uncommitted or unpushed work, red or unknown CI, a change half-made.

## The page

1. **A "Milestone" button beside "New conversation"** on the Conversation tab. It sends the same message as typing
   "milestone". It's disabled while a turn is running, as sending a message is.
2. **When the milestone's turn ends, the page shows its verdict** under the conversation. If it's safe, a "Start a new
   conversation" button appears there (it opens the same confirmation as the existing one). If it's blocked, the page
   lists the reasons, with no button.
3. **"New conversation" nudges** when there has been no milestone since the last turn: the confirmation gains a line
   ("No milestone since the last change. Run one first?") and a button that runs one. Starting anyway is still allowed.

## Steps to build

Each step leaves the tests green.

1. The verdict record: the vibe command the skill runs, where it writes (host-side, following how the review inbox
   works), and the companion API that reads it. Go tests.
2. The skill (`SKILL.md`), with the steps above. A CLI test like `reviewcli_test.go` for the command it runs.
3. The page: Milestone button, verdict display, nudge in the "New conversation" confirmation. jsdom page tests for
   each: disabled while busy, safe shows the button, blocked lists reasons and shows none, missing record says it
   didn't finish, nudge shown only when there's been a turn since the last milestone.
4. Docs: readme section; the skill named in the agent instructions.
5. Play-test with `./test-clean` (the skill is under `library/`, so a sandbox rebuild is needed).

## Open questions

1. **Where does the handover go?** Options: in memory (always read at the start of a session, but it's in the agent's
   home), in the plan doc (in the repository, so visible and versioned, but only when a plan exists), or a vibe-kept
   file shown on the page. Leaning: the plan doc when there is one, else memory.
2. **How does the verdict reach the host?** Probably like the review inbox: a mounted folder the skill's command writes
   to and the companion reads. That's a new mount, so existing sandboxes need a rebuild to get it.
3. **"No milestone since the last turn":** count by turns the page has seen, or by the repository's state (HEAD and
   status) at the milestone? The repository's state is more accurate, but costs a status read when the dialog opens.
4. **Should a milestone also be offered on its own,** for example after a push turns CI green? Leave it out at first;
   decide after real use.

## Phase 2: the feature loop

Planned 2026-10-16, after phase 1. For work that takes several commits; small changes still go straight to master.

### Decisions made

1. **"In a feature" is the branch:** a branch named `feat/…`. vibe keeps no state of its own about it, so the page
   can't disagree with git.
2. **Two buttons on the Conversation tab:**
   - **New feature / Feature complete:** one button, its label following the branch.
   - **Milestone:** always there, since a long feature still needs milestones partway through.
3. **New feature:**
   - **The name, two ways,** since people think about it differently. The dialog offers both:
     - **Name it:** a box with `feat/` already filled in, for someone who knows what they want.
     - **Describe it:** plain text ("I want to create a review pane"). The agent suggests a branch name, and the user
       confirms or changes it before anything is made.
   - **Where it starts from is a choice.** The default is the current branch, brought up to date from its remote first.
     The default branch is the other choice offered. Starting from the current branch is what makes a feature within a
     feature possible (see Decision 6).
   - **Uncommitted work prompts; it isn't refused.** Starting something experimentally and then realising it needs a
     branch is common (the review tab started that way). The choices:
     - **Take them onto the new branch** (the default): branch from where you are, and the changes come along.
     - **Stash them:** start clean, with the stash named so it can be found again.
     - **Cancel.**
   - If local commits on the default branch aren't pushed, branching from where you are takes them along. The agent
     says so, and asks before removing them from the default branch; it never does that quietly.
   - Ends by starting a new conversation, with the milestone nudge if one is due.
   - The agent makes the branch, in the sandbox; the host still never runs git on the workspace.
4. **Feature complete** checks more than a milestone does:
   - everything committed and pushed;
   - CI green on the latest push;
   - no open threads on the Review tab;
   - nothing open in the plan doc, and no pending tests in memory.

   If all pass, it runs a milestone. If any fail, the page lists them, with **Finish anyway**. Forcing is allowed, but
   what was still open is written into the handover ("finished with these still open: …"), never dropped.
5. **Merging stays the user's**, like committing and pushing. Offering to open a PR may come later.

6. **A feature within a feature.** Starting from a `feat/` branch makes a smaller feature inside it, merged back into
   it rather than into master. This keeps each review small while the release stays whole: the bigger feature reaches
   master only once all its parts are done.
   - **Naming:** git can't have both `feat/review-tab` and `feat/review-tab/store`, because a branch name can't also be
     a folder of branches. A sub-feature is named after its parent with a separator: `feat/review-tab--store`.
   - **The parent is recorded in git**, in the repository's config (`branch.<name>.vibeBase`), when the branch is
     made. So "Feature complete" knows what to merge into and what to review against. The branch name still says
     which feature you're in; the config only says where it came from, and git keeps both.
   - Without that record (a branch made by hand), the agent asks where the feature came from.

### Open questions

1. **Does "in a feature" need the `feat/` prefix?** Some teams make one-line `fix/` branches; the user doesn't. Suggestion
   to weigh when building: count any branch that isn't the default branch. Then the prefix only shapes the name
   suggested, the button reads "Finish `<branch>`", and the checks are the same for all. They cost little on a one-line
   branch, and nobody has to choose which prefixes count. Undecided.
