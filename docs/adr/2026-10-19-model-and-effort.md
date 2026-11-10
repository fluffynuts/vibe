# Choosing the model and effort from the Conversation tab, for Claude, as the pattern for other agents

- **Status:** accepted, built for Claude 2026-10-21 (`feat/model-and-effort`, merged), and refined in use until 2026-10-22. Other agents are not built:
  see "The pattern for other agents", which is what this ADR is for, and `docs/plan-conversation-other-agents.md`.
- **Decided:** 2026-10-19 and 2026-10-21, with the user.
- **Came from:** `docs/plan-model-effort.md`, now deleted: `git show 79ca54a:docs/plan-model-effort.md`.

## Context

Every turn of the conversation is its own process (`claude -p --output-format stream-json --verbose --permission-mode bypassPermissions`, resumed with
`--resume <id>`), so a model or effort chosen on the page can ride on each turn's command line and nothing has to be changed in a running agent. The
user wanted to see which model and effort the agent works with, and change either in one step: Opus to Sonnet for a simple job, a higher effort for a
hard one. The Conversation tab exists only for Claude sandboxes, so this was Claude-only to begin with.

## Decision

### The choice

- Two native `<select>`s in the conversation's bar (native, so keyboard, screen readers and phones work with nothing more to build), beside the prompt:
  **Model** and **Effort**. The bar under the prompt is a grid (`1fr auto 1fr`): Stop on the left, the drop-downs centred, Send on the right, so Stop
  appearing moves nothing; in a narrow column the drop-downs take a row of their own. Each has a width of its own (a drop-down sizes itself to its
  longest option, which made the group jump when the lists arrived: found in a real browser, not jsdom, and now pinned by Firefox tests).
- **The lists are live, from the agent**, so nothing is hard-coded per release. For Claude: `claude -p /model` answers locally with no model call ("Available:
  sonnet, opus, haiku, fable, best, …"); `claude --help` gives the `--effort` values. The plain model names go in the drop-down; `best`, `opusplan` and
  the `[1m]` variants go behind **Other…**, an in-page dialog that also takes a full model name. Only effort values the flag is known to accept are
  offered (`low`, `medium`, `high`, `xhigh`, `max`); `auto` and `ultracode` appear in the agent's own help but its flag rejects them: reliability over
  completeness.
- **Aliases follow the latest model,** so the version isn't in the list: vibe asks `/model <alias>` for each (one `sbx exec` loop, local, no model call)
  and shows plain models by their name alone ("Opus 5.5") and the rest as `Name · alias`. Models are listed biggest first (fable, opus, sonnet, haiku;
  a name that isn't one of those comes after, alphabetically, claiming no size). There are no visible labels (the entries say what they are, and the
  effort entries read "Low effort" … "Extra High effort"); the labels stay for screen readers.
- **You are on a model; "default" is only a word.** Once a model is in use there is no Default entry: the agent's default is marked "(default)", and a
  default that moves under the user (claude re-fetches server-side settings on every run) moves only the mark, never what runs. When none is chosen or
  inherited, the host *adopts* the one that ran, or the default's alias, into the profile. Effort keeps "Default" (claude doesn't expose its default).
- **Changing the model resets effort to Default**, since which efforts mean what differs by model and agent. A change applies from the **next prompt**
  (`pendingTurns` stops a turn that was already running from raising a false "ran another model"). A model change on a conversation that has a
  session warns first, in an in-page dialog, that the next prompt may re-read the whole conversation (slower, dearer); an effort change doesn't.

### What it says is true

- What the select shows is what the user asked for. A small **"i" button with a bubble** says what the agent *reported* running (from the `init` line's
  `model` field; there is no effort field), and speaks **only when it is news**: nothing when it matches what was asked; an amber note, at the end of every
  turn it happens, when it doesn't (the name isn't one the agent knows, a settings file or `ANTHROPIC_MODEL` overrides, the user ran `/model`). A dot says
  something hasn't been looked at. What is inherited from the global settings is said, since Default in the profile can't take it away.
- **Failures:** something the user asked for that couldn't be kept is a modal dialog over a frosted page; failing to get the lists when the page opens is
  not something the user did, so it is in the bubble, opened, with **Retry**. That departs from "errors in a dialog" for that one case.
- **In-page dialogs only**, never `alert`/`confirm`/`prompt`, with a tinted backdrop that stops click-through; `dialogs_test.go` fails if the page ever uses
  the browser's own.

### The lists are kept, and asked again

- **Per sandbox** (`companion/<name>.model-options.json`, removed and moved with the sandbox): the lists depend on the `claude` in that sandbox, the account
  and a project's settings. Stale-while-revalidate: the last lists show at once and the agent is asked again in the background. A file that can't be read, is
  another version, or lists no models is *said* and not used (it is a cache). **Fresh for ten minutes**, and the page asks silently at the end of every
  turn, so a new default or model is taken in; a model or effort in use that the new lists no longer offer opens the bubble once, amber.
- First ask: the widget is dimmed and a spinner shows, with a **30 s** limit on the lock (a guess; the user agreed to a limit, not to 30). Kept lists
  leave it usable, only the spinner showing, so nothing surprises a user who has a list open.
- What the sandbox's `claude` may say is bounded, since the agent can replace it: at most 50 models, names of at most 100 characters, `--help` 256 KB, and
  only the aliases asked about are taken from the names. The `--effort` list is read only from its own description as exactly one parenthesised list of two or
  more lower-case names; anything else is an error, not a guess. The model string goes into a shell command, so it is validated on the host
  (`^[A-Za-z0-9][A-Za-z0-9._:\[\]-]*$`, a length cap) and quoted; effort must be in the fixed set.

### Where it is kept

- **With the profile,** so it outlives vibe, a page reload and a rebuild, and carries over a new conversation (which resets the session, not the preferences).
  A profile can hold several sandboxes, which then share the choice: accepted. First written to the profile's `settings.yaml`, which made the profile look
  edited by hand, so on 2026-10-22 it moved to `user-preferences.yaml`
  (`docs/adr/2026-10-14-profiles-and-sandboxes.md`): the profile's preferences, then its `settings.yaml`, then the global one.
- API: `GET /api/conversation/choice`, `POST /api/conversation/choice` (a model change resets the effort, enforced on the host), `POST .../choice/options`
  (`{"wake": bool}`: asked when the page opens **without waking** a sleeping sandbox; Retry wakes it) and `POST .../choice/adopt`. All the POSTs are in the guard
  test's list of changing routes.

## The pattern for other agents

Keep the model and effort code free of any one agent's specifics: **no Claude flag names or list formats outside the Claude description.** The lists, the flags
and the "what ran" come from a per-agent description. Switching agent resets both choices. Staged: Claude first; then generic, play-tested with an open model through
OpenCode (the user's choice, to see it on a free model) and a Codex user; then the rest by user reports. Some agents may not be able to do this, which is accepted.

**A new agent gets a cut-down real-agent check first.** What was found for Claude (CLI 2.1.306), and so what to ask of another:

1. Does a resumed session answer with another model (same session id)? For Claude it does. (The prompt-cache cost couldn't be shown on a tiny session, hence "*may* re-read".)
2. What does an unknown effort do? Claude ignores it with a warning on stderr; an accepted value that isn't honoured is not visible; there is no per-model list.
3. What does the first stream line say? Claude's `init` has `model`, not effort, and echoes an unrecognised name back as given.
4. What does a bad model look like? Claude: a result with `is_error: true`, shown in the conversation like any error.
5. Are in-prompt `/model` and `/effort` lasting? In Claude they are "this session only", so the flags are the mechanism.
6. Precedence of the flag over the environment: Claude's `--model` beats `ANTHROPIC_MODEL`, and with no flag the environment decides, so "default" means what the sandbox says.

## Consequences

- The agent's own settings still decide when nothing is chosen, and the page never lies about what ran.
- Per-sandbox lists and the host's adoption mean the page's choice is stable while the agent's default moves.
- Agent output is bounded and validated on the host, because the agent controls the `claude` that answers.

## Not built

- **Other agents** (`docs/plan-conversation-other-agents.md`: the Conversation tab doesn't exist for them yet; the drop-downs ride on it), a **global default**
  beyond what the settings layers already give, and OpenCode-style **per-model effort lists**.

## Not verified

- Whether `--effort` beats a `CLAUDE_EFFORT` environment variable (effort isn't visible in `init`); a measured cache cost of switching models mid-session; a
  fallback model's effect on what is reported (out of scope). The first build was tried by the user on a real sandbox and browser and worked.
