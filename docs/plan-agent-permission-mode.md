# Choosing the agent's permission mode on the page, and saying when it isn't the one in force (planned)

Raised 2026-11-24, with the user, from PR comment #124 on `fix/agent-permission-mode`. That hotfix runs the conversation's agent in
`auto` by default, or the `permissionMode` the profile's `user-preferences.yaml` or a `settings.yaml` chooses
(`docs/adr/2026-11-24-agent-permission-mode.md`). This branch, stacked on it, lets the page choose it, applies a change to the
running agent, and says when the agent runs in another mode than was asked for: that morning the agent refused nearly everything,
and nothing said why (an organisation's managed settings had disabled bypass, and Claude Code fell back to the settings' Manual
`defaultMode` without a word).

## What Claude Code says (checked with 2.1.331, 2026-11-24)

- **The mode in force** is in the stream-json `system`/`init` event (`"permissionMode"`), and in a `system`/`status` event after it
  changes. Manual is reported as `"default"`.
- **A running agent's mode changes** with the control request `{"subtype":"set_permission_mode","mode":…}`: the answer is
  `{"subtype":"success","response":{"mode":…}}`, then a `status` event with the new mode.
- **A mode refused by policy** gets `{"subtype":"error","error":"Cannot set permission mode to bypassPermissions because it is
  disabled by settings or configuration","error_code":"bypass_disabled"}`; the agent stays in its mode.
- **An unknown mode** gets `error_code: "invalid_mode"`.
- **A start in a mode that is refused** gets no error at all: the init event simply reports another mode (the settings'
  `defaultMode`).

So vibe never needs to read the policy: what Claude Code says is enough.

## Decided (with the user)

1. **A drop-down on the Admin tab** (changed rarely, about risk, per sandbox): Auto (the default), Bypass permissions, Accept edits,
   Manual, Don't ask, Plan. Choosing writes `permissionMode` in the profile's `user-preferences.yaml` (choosing the default takes the
   key out). The card says where the choice comes from when a `settings.yaml` made it.
2. **Applied to a running agent** with `set_permission_mode`, before the next prompt, as the model and effort are (from the next
   prompt: the card says so).
3. **Said when it isn't in force:** when the mode the agent reports isn't the one vibe asked for, the card says which is in force,
   and why when Claude Code said (a refusal's error); the Conversation tab's status balloon has a warning that stays while it is
   so, as the "no output" one does.
4. **`auto` stays the default.** It works with or without such a policy; bypass refused at start-up falls back to whatever
   `defaultMode` says.

## How

- **Host:** the conversation keeps the mode in force (init, status) and the last refusal (a `control_response` error to a request
  vibe tagged as a mode change); the agent says which mode it was asked for. `GET`/`POST /api/permission-mode`: what is chosen
  and where from, what was asked for, what is in force, why not; a POST writes the preference.
- **Page:** the Admin tab's card with the drop-down; the balloon's warning from the conversation's poll.

## Tests (first)

- Host: the init and status events' mode kept, `default` read as Manual; a refusal kept and cleared by the next success; the
  endpoint reads and writes the preference, refuses an unknown mode; the agent sends `set_permission_mode` when the choice changed,
  and not otherwise.
- Page: the drop-down shows the choice and saves a change; the card and the balloon say when the mode in force isn't the one asked
  for, with the reason; nothing is said when they agree.
