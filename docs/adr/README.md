# Architecture decision records

One file per decision (or per group of decisions about one subject), named `YYYY-MM-DD-title.md` with the date the decision was made, so the
folder sorts as a history. A plan (`docs/plan-*.md`) is for work not yet built; when it is built it becomes an ADR and the plan is deleted.
A plan only partly built is split: the built parts become an ADR, what is left stays as a smaller plan.

Each ADR has: **Status** (accepted, built, when, and on what branch), **Decided** (when, with whom), **Came from** (the plan it replaced, as a
`git show <sha>:docs/plan-x.md` that still works after the deletion), then Context, Decision, Consequences, and **Not built** and **Not verified**
where there is something to say. A decision that is later changed gets a note in its own ADR, or a new ADR that says what it replaces.

| ADR | Subject |
|---|---|
| [2026-10-13-agent-instructions](2026-10-13-agent-instructions.md) | The user's instructions, per profile, loaded in every session |
| [2026-10-13-bounded-waits](2026-10-13-bounded-waits.md) | Every wait has a backstop; what is left running can be stopped |
| [2026-10-14-headless](2026-10-14-headless.md) | Headless: reaching the page from another machine, noticing a logged-out agent |
| [2026-10-14-profiles-and-sandboxes](2026-10-14-profiles-and-sandboxes.md) | Managing, updating by content, and keeping the user's choices apart |
| [2026-10-15-review-tab](2026-10-15-review-tab.md) | The Review tab: reviews live on the host, in the companion page |
| [2026-10-18-new-conversation](2026-10-18-new-conversation.md) | New conversation: a blank-slate start that can begin a branch |
| [2026-10-18-nuget-without-the-hosts-nuget](2026-10-18-nuget-without-the-hosts-nuget.md) | NuGet packages without giving the agent the host's NuGet |
| [2026-10-19-model-and-effort](2026-10-19-model-and-effort.md) | Choosing model and effort on the page; the pattern for other agents |
| [2026-10-23-page-ergonomics](2026-10-23-page-ergonomics.md) | Questions that stand out, a power menu, reporting an issue |
| [2026-10-28-playwright-feature](2026-10-28-playwright-feature.md) | Browsers installed at build time |
| [2026-10-29-signing-in](2026-10-29-signing-in.md) | Signing in to sbx and the agent; the network policy; a start while signed out |
| [2026-10-29-stop-ends-review](2026-10-29-stop-ends-review.md) | Stopping the agent while it writes a review asks first; a quiet review; closing on a branch switch |
| [2026-11-12-stop-feedback](2026-11-12-stop-feedback.md) | Stopping the agent says so at once; a prompt sent meanwhile waits for the stop |
| [2026-11-12-network-block-and-filter](2026-11-12-network-block-and-filter.md) | The network tabs: Block and Allow beside the rules there are, superseded log rows, a filter, a read sign, polls that change nothing |
| [2026-11-12-batch-git-token](2026-11-12-batch-git-token.md) | One GitHub token for several sandboxes at once; "the token another sandbox has" |
| [2026-11-16-persistent-agent](2026-11-16-persistent-agent.md) | One agent process for the conversation: background work survives a prompt and calls back |
| [2026-11-17-downgrade](2026-11-17-downgrade.md) | One step back for vibe or Docker SBX; what runs is stopped before sbx changes |
| [2026-11-18-status-balloon](2026-11-18-status-balloon.md) | The Conversation tab's status in a balloon from an info button; Stop for background work |
| [2026-11-20-clipboard-toasts-and-review-headers](2026-11-20-clipboard-toasts-and-review-headers.md) | The clipboard page's status line becomes toasts; a Review file's header stays one line |
| [2026-11-20-pr-and-issue-links](2026-11-20-pr-and-issue-links.md) | A pull request or issue the agent names links to it on GitHub, not to a Review comment |
| [2026-11-20-git-token-next-steps](2026-11-20-git-token-next-steps.md) | --batch-git-token for scripts; a push checked from inside the sandbox; tokens running out on the Admin tab; read-only tokens |
| [2026-11-20-closed-review-says-what-it-was](2026-11-20-closed-review-says-what-it-was.md) | A closed review says what it was, so Reopen says what it reopens |
| [2026-11-23-bare-pr-numbers](2026-11-23-bare-pr-numbers.md) | A bare #N that is a pull request links to it on GitHub; agents told to write "PR #N" |
| [2026-11-23-countdown-bars](2026-11-23-countdown-bars.md) | What closes itself shows its time running out, held while hovered; the status balloon closes when emptied |
| [2026-11-24-agent-permission-mode](2026-11-24-agent-permission-mode.md) | The conversation's agent runs in auto mode (bypass can be disabled by an organisation), or the permissionMode settings.yaml chooses |
| [2026-11-24-agent-permission-mode-choice](2026-11-24-agent-permission-mode-choice.md) | The Admin tab chooses the agent's permission mode, applied before the next prompt; the page warns when the mode in force isn't the one chosen |
| [2026-11-24-clipboard-hook-plugin](2026-11-24-clipboard-hook-plugin.md) | The clipboard hook is registered by a plugin: Claude Code ran nothing from the managed-settings drop-in |
| [2026-11-24-conversation-tab-height](2026-11-24-conversation-tab-height.md) | The Conversation tab uses the height under the tab bar: its buttons in the tab bar, the clipboard without a bar |
