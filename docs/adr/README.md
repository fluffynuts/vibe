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
