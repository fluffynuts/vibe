# The agent's permission mode is chosen on the Admin tab, and said when it isn't the one in force

- **Status:** accepted, built 2026-11-24 on `feat/agent-permission-mode-choice` (stacked on `fix/agent-permission-mode`). Not yet
  tried by hand.
- **Decided:** 2026-11-24, with the user, from PR comment #124: after a morning of an agent refusing everything with nothing to say
  why, the mode is to be chosen on the page, and the page is to say when it isn't the one in force.
- **Came from:** `docs/plan-agent-permission-mode.md`, now deleted: `git show 1c709f6:docs/plan-agent-permission-mode.md`. Builds on
  `2026-11-24-agent-permission-mode.md` (auto by default, `permissionMode` in the settings).

## What Claude Code says (checked with 2.1.331)

- The mode in force is in the stream-json `system`/`init` event (`"permissionMode"`), and in a `system`/`status` event when it
  changes; Manual is reported as `"default"`.
- A running agent's mode changes with the control request `{"subtype":"set_permission_mode","mode":…}`. A mode the settings disable
  is refused: `{"subtype":"error","error":"Cannot set permission mode to bypassPermissions because it is disabled by settings or
  configuration","error_code":"bypass_disabled"}`, and the agent stays in its mode. An unknown one: `error_code: "invalid_mode"`.
- A start in a refused mode gets no error at all: the init event simply reports another (the settings' `defaultMode`).

So vibe never reads the policy: what Claude Code says is enough.

## Decision

- **Bypass permissions is the default again** (decided with the user, after PR #38 was closed): the sandbox is the boundary, Auto's
  classifier refuses things now and then, and in older Claude Code (2.1.291) Auto didn't work at all while bypass did. Where an
  organisation disables bypass, the agent falls back to Claude Code's `defaultMode`, which the page now says, offering Auto.
- **Use Auto:** when it was bypass that was asked for and isn't in force, the card's and the balloon's warnings have a button that
  chooses Auto (allowed under such a policy, and it wins over a Manual `defaultMode`). vibe doesn't switch by itself: the mode the
  agent runs in is the user's to choose.
- **Each refusal is said:** a `system`/`permission_denied` event (`tool_name`, `message`) becomes a line of its own in the
  conversation, kind `denied`: "Claude Code didn't let the agent use Bash: Output redirection to … needs approval. (the agent is in
  Manual)", with **Agent permissions…** to the card. The agent may not mention it; the page did nothing with these events before.
  Seen from Manual's refusals; whether Auto's classifier refusals come as the same event isn't known (two tries to provoke one
  were let through).

- **The Admin tab's Agent permissions card** (changed rarely, about risk, per sandbox) has a drop-down of the six modes, Bypass
  permissions marked as the default. Choosing writes `permissionMode` in the profile's `user-preferences.yaml` (never a `settings.yaml`: a
  generated profile's is what composing wrote). Choosing the default writes it rather than taking the key away: under it a
  `settings.yaml` could otherwise choose again, and the page can't see that. A mode a `settings.yaml` chose is shown with the file.
- **Applied before the next prompt**, as the model and effort are: the agent sends `set_permission_mode` when the choice differs
  from what it was last asked for, with a `request_id` the page knows (`vibe-mode-N`) so the answer is read. The card says a change
  applies from the next prompt.
- **Said when it isn't in force:** the conversation keeps the mode the agent reports (init, status) and a refusal's words; the agent
  says what it was asked for. When they differ, the card warns, and the status balloon has a warning that stays while it is so,
  naming both modes and Claude Code's words (or that its settings didn't allow it), and what Manual means here.
- **`userprefs.Update` writes only the keys it lists**, and silently dropped the new field: a test now sets every string field of
  `Prefs` through it and reads it back.

## Not built

- What the agent reports is known only while an agent runs: an idle page (the agent ends ~30 s after its work) shows no mode in force
  until the next prompt.
