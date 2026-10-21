# Plan: choosing the model and effort from the Conversation tab

Status: planned 2026-10-19, decisions made the same day, not started ("down the road"). Nothing here has been run
against a real agent.

## The outcome

The Conversation tab shows which model and which effort the agent is working with, as two drop-downs, and the
user changes either in one step: say, from Opus to Sonnet for a simple job, or to a higher effort for a hard one.

## What's there today (read from the code, 2026-10-19)

- Every turn is its own process: `claude -p --output-format stream-json --verbose --permission-mode
  bypassPermissions`, resuming the session with `--resume <id>` (`src/vibe/conversation.go`, `agentArgs`,
  `conversationCommand`). So a model or effort chosen on the page can ride on each turn's command line; nothing
  has to be changed in a running agent.
- The installed CLI (2.1.303, in the sandbox this was written in) takes `--model <alias or full name>` (aliases
  such as `fable`, `opus`, `sonnet`) and `--effort <low|medium|high|xhigh|max>`.
- The Conversation tab is only there for a Claude sandbox (`src/vibe/companion.go` turns it off for other
  agents), so this is Claude-only, and a different agent gets no drop-downs.
- vibe keeps the conversation's state on the host (`internal/companion/conversation.go`, saved after every
  turn); the model and effort belong there.
- The stream's `system`/`init` line is already read for the slash commands. It is believed to name the model
  the session started with; **not verified** — check a real line before relying on it.

## Design

### The choice

- Two native `<select>` elements in the conversation's header bar, beside **Stop** and **New conversation**:
  **Model** and **Effort**. Native, so keyboard, screen readers and phones work with nothing more to build.
- Each starts on **Default**: no flag is passed, so the sandbox's own Claude settings (`~/.claude/settings.json`,
  the profile's `env`) decide, exactly as today. Nothing changes for anyone who never touches them.
- Model: Default, then the aliases (opus, sonnet, haiku, and fable while the CLI offers it), then **Other…**
  taking a full model name. Aliases follow the latest model, so the list needn't be updated for each release.
- Effort: Default, low, medium, high, xhigh, max.
- Kept per sandbox, in the conversation's saved state, so it outlives vibe and a page reload, and carries over
  to a new conversation (a "new conversation" resets the session, not the preferences). Rebuilding the sandbox
  keeps it, as it keeps the session.

### What it says is true

- What the select shows is what the user asked for. Beside it, small text says what the agent *reported*
  running: the model from the last `init` line ("running claude-opus-…"). If the two differ (the model name
  isn't one the agent knows, a settings file or `ANTHROPIC_MODEL` overrides, the user ran `/model`), the page
  says so rather than showing only the request. Truth before tidiness.
- A change applies from the **next prompt**. While the agent is working the selects can still be changed, and
  say "applies to your next prompt"; vibe doesn't try to change a turn already running.

### The command

- `conversationCommand` gains the two values and adds `--model X` / `--effort Y` only when set.
- The model string goes into a shell command (the turn runs through `su -c`/`bash -lc`), so it is validated
  strictly on the host, in the API, before it is stored: `^[A-Za-z0-9][A-Za-z0-9._:\[\]-]*$`, a length cap,
  and quoted with the existing `shellQuote`. Effort must be one of the fixed set. Anything else is refused with
  a message saying what's allowed.

### The API

- `GET /api/conversation` already returns the conversation's state; it gains `model`, `effort` (what's asked) and
  `running` (what the agent reported).
- `POST /api/conversation/settings` with `{model, effort}`; same-origin and token checks as the other POSTs.

## Things to find out before building

1. **Resuming with a different model.** Does `--resume` with a different `--model` just work, and what does the
   user lose: the prompt cache for the session (so the next turn is slower and dearer), thinking blocks from
   another model? If there is a real cost the page should say so when the model changes ("the next prompt
   re-reads the whole conversation").
2. **Which efforts each model takes.** Whether `--effort max` on a model that doesn't support it is an error, is
   ignored, or is mapped. If it errors, the page should only offer what the chosen model supports, or show the
   agent's error in the conversation where the user will see it (not hide it).
3. **The `init` line's fields**, to show the running model (above).
4. **`/model` and `/effort`** typed as a prompt in `-p` mode: do they exist, and do they change anything for the
   next turn? If they do, the page and the agent can disagree, which is what the "reported" text is for.
5. **Precedence.** `--model` over `ANTHROPIC_MODEL` over settings: confirm, so "Default" means what the page says.
6. **Fallback models** (`--fallback-model`): out of scope, but note whether a fallback makes "reported" differ.

## Tests

- Unit (Go): command building (flags present only when set, quoting, order with `--resume`); validation (good and
  bad models and efforts, including shell metacharacters); the saved state round-trips; "new conversation" keeps
  the choice.
- Unit (jsdom): the selects show the stored choice and the reported model; changing one posts it; a refused value
  shows the host's message; "Other…" takes a name; they're absent for a sandbox with no conversation.
- Integration: the existing tests run a stand-in `claude`; extend it to record its arguments and assert a turn
  after a change carries the new flags, and one before it doesn't.
- One manual check with a real agent (items 1–5 above), by the user or with their go-ahead (it costs tokens).

## Not in scope

- Other agents (their CLIs differ; nothing here assumes them).
- Choosing a model per review comment, or per slash command.
- Showing cost. The page already shows each turn's cost line from the agent.
- `vibe --headless` and the terminal: the terminal Claude has `/model`; nothing to add.

## Decisions of 2026-10-19

1. **Per sandbox.** The choice survives **New conversation**.
2. **The choice persists across sessions and rebuilds**, now, with the drop-downs. A profile default
   (`model:` / `effort:` in `settings.yaml`) comes later; when it does, **Default** in the drop-down means the
   profile's, and a choice the user has made still wins. Persisting means the same place the conversation's
   session is kept: on the host, by sandbox, outside the sandbox, so `-r` and `-C` keep it, as they keep the
   session. To check when building: that `-R` (which deletes the profile) keeps it too, and that only
   `--delete` and `--cleanup` remove it, with the rest of the sandbox's state.
3. **Warn first.** A change that costs something (item 1 of the things to find out: the prompt cache, if
   `--resume` with another model does lose it) is stopped with the warning before it applies, and the user
   confirms or cancels: the select goes back to what it was on cancel. A change that costs nothing applies at
   once. What counts as costing something is for the real-agent check to settle; until it is, a model change
   warns and an effort change doesn't. The wording says what happens in the user's terms: "the next prompt
   re-reads the whole conversation, which is slower and costs more."
