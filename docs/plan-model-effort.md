# Plan: choosing the model and effort from the Conversation tab

Status: planned 2026-10-19; decisions of 2026-10-19 and 2026-10-21 below; the real-agent check for Claude was run
on 2026-10-21 (findings below) and found no blocker. **Built 2026-10-21** on `feat/model-and-effort`, committed as
it went; see "As built" at the end. A real browser and a real sbx sandbox are still to try.

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
- Model: Default, then the plain-named models from a **live list** (see "Where the lists come from"), then
  **Other…**, which opens an in-page dialog listing every other choice the agent reports (the ones not already in
  the drop-down) and takes a full model name. Aliases follow the latest model, so nothing is hard-coded per release.
- Effort: Default, then only the values the agent's flag accepts (for Claude: low, medium, high, xhigh, max).
  Values the agent's own console shows but its flag rejects (`auto`, `ultracode`) are not offered: reliability
  wins over completeness, and they can be added if a working route turns up.
- **Changing the model resets effort to Default**, and the user picks again if they want to: which efforts mean
  what differs by model and by agent, and nothing tells us which a model supports.
- Kept with the **profile**, written back as if it were the workspace's correct profile (least surprise), so it
  outlives vibe, a page reload and a rebuild, and carries over to a new conversation (a "new conversation" resets
  the session, not the preferences). The user's own pattern is one profile per sandbox, but a profile can hold
  several sandboxes, in which case they share the choice: accepted.
- The confirmation before a costly change is an **in-page dialog**, never `confirm()`; it blocks (the user must
  answer), with a tinted backdrop so nothing behind it can be clicked.

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

## Where the lists come from

- Claude: a `/model` prompt in `-p` mode answers without a model call (cost not measured) with the current model and
  "Available: sonnet, opus, haiku, fable, best, sonnet[1m], opus[1m], fable[1m], opusplan, default, or a full
  model ID". That is the live list. The plain names go in the drop-down; `best`, `opusplan`, `default` and the
  `[1m]` variants go in the **Other…** dialog.
- The `/effort` usage text lists `low|medium|high|xhigh|max|auto|ultracode`; only the first five are valid
  `--effort` values.
- Other agents: whatever their CLI reports; to be found when each is tried (see "Agents, in stages").

## Findings of the real-agent check, Claude CLI 2.1.306, 2026-10-21

Run with one-word prompts on haiku and sonnet; costs of a long session were not measured.

1. **Resume with another model works** (same session id, answered by the new model). The prompt-cache cost could
   not be shown on a tiny session, so the warning says the next prompt *may* re-read the whole conversation.
2. **Effort:** an unknown value is ignored with a stderr warning ("Unknown --effort value … ignoring it");
   `--effort max` on haiku is accepted silently, and whether it is honoured is not visible. There is no
   per-model list of efforts.
3. **The `init` line** has a `model` field (`claude-haiku-5-5`) and no effort field. So the "running" text covers
   the model only. An unrecognised model name is echoed back as given.
4. **A bad model** comes back as a result with `is_error: true` ("There's an issue with the selected model …"):
   shown in the conversation like any error.
5. **`/model` and `/effort` as prompts** work in `-p` mode but are "this session only": the next turn starts on
   the default again. They change nothing lasting, so the flags are the mechanism.
6. **Precedence:** `--model` beats `ANTHROPIC_MODEL`; the variable beats nothing when the flag is absent, so
   "Default" means whatever the sandbox's settings/env say.
7. **Not verified:** whether `--effort` beats the `CLAUDE_EFFORT` environment variable (effort isn't visible in
   `init`); a measured cache cost; a fallback model's effect on what is reported (out of scope).

## Agents, in stages

Today only Claude is wired: `conversationFor` (`src/vibe/companion.go`) gives no conversation to any other agent,
and the turn command and stream parsing are Claude's (`agentArgs`, `src/vibe/conversation.go`). The conversation
tab working for other agents is its own piece of work; the drop-downs ride on it.

1. **Claude** first, as the user and their team use it.
2. **Generic:** keep the model/effort code free of Claude's specifics (the lists, the flags and the "what ran"
   come from a per-agent description), then play-test with an open model through OpenCode (the user's choice, to
   see it on a free model), and ask a Codex user to try it.
3. **Untested agents** get the same options, and rely on user reports if they don't work. Some agents may not
   be able to do this; that is accepted.

Do not paint into a corner: no Claude flag names or list formats outside the Claude description. Switching agent
resets the model and effort to that agent's defaults.

## Tests

- Unit (Go): command building (flags present only when set, quoting, order with `--resume`); validation (good and
  bad models and efforts, including shell metacharacters); the saved state round-trips; "new conversation" keeps
  the choice.
- Unit (jsdom): the selects show the stored choice and the reported model; changing one posts it; a refused value
  shows the host's message; "Other…" takes a name; they're absent for a sandbox with no conversation.
- Integration: the existing tests run a stand-in `claude`; extend it to record its arguments and assert a turn
  after a change carries the new flags, and one before it doesn't.
- The real-agent check for Claude is done (findings above). Repeat a cut-down version for each new agent tried.
- Sweep: no `alert()`/`confirm()`/`prompt()` in the page (none today, 2026-10-21); consider a test that fails if one appears.

## Not in scope

- Other agents in the first build (their CLIs differ; the structure leaves room: see "Agents, in stages").
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

## Decisions of 2026-10-21

1. A model change resets effort to Default; an agent change resets both.
2. The lists are live, from the agent. Plain names in the drop-down, the rest behind **Other…** in a dialog. Only
   effort values the flag is known to accept are offered (reliability over completeness).
3. In-page dialogs only. A blocking question gets a tinted backdrop that stops click-through.
4. Stored with the profile, written back as the workspace's profile. Shared by every sandbox in that profile.
5. Staged: Claude, then generic with OpenCode, then Codex by a friend, then leave the rest to user reports.
6. Commit as the build goes, on `feat/model-and-effort`.

## As built (2026-10-21)

- `internal/modelchoice`: reads the agent's lists (`ParseModels` from a `/model` answer, `ParseEfforts` from
  `--help`; real captured output is in `testdata/`) and checks a choice is safe to put on a command line.
- The choice is `model:` / `effort:` in the profile's `settings.yaml` (`src/vibe/modelchoice.go`, `profileChoice`),
  read for each turn, so a change is the next prompt's. `--re-compose` deletes and rebuilds a profile, so it
  reads the choice first and writes it back (as it does the theme and the user's instructions).
- The turn adds `--model X --effort Y` before `--resume`, only when set (`conversationCommand`); a stored value
  that isn't safe is refused with where to fix it, before anything is sent.
- The lists are asked of the sandbox's own claude over `sbx exec`: `claude --help` and `claude -p /model
  --output-format json --no-session-persistence` (a local command: no model call). Asked when the page opens
  **without waking** a sleeping sandbox; the page's **Retry** wakes it. Kept for the life of the page's vibe.
- Routes: `GET /api/conversation/choice`, `POST /api/conversation/choice` (a model change resets the effort,
  enforced on the host), `POST /api/conversation/choice/options` (`{"wake": bool}`). Both POSTs are in the guard
  test's list of changing routes.
- The page (`app.js`, "the model and effort"): two selects in the conversation bar, an in-page `<dialog>` for the
  warning (asked only when the conversation has a session: nothing to re-read otherwise) and one for **Other…**.
  Dialogs are modal with the usual tinted backdrop. `internal/companion/dialogs_test.go` fails if the page ever
  uses the browser's own `alert`, `confirm` or `prompt`.
- Decided while building: the warning is skipped when there is no session yet; **Default** is labelled with the
  model the agent names as its default; "what ran" is judged loosely (an alias is part of the full name) and not
  at all for `best` / `opusplan`, and not while a change is waiting for its first turn.
- Not done: other agents (see `plan-conversation-other-agents.md`); a global default beyond what settings
  already layer; the OpenCode-style per-model effort lists.

### Layout, after the first real try (2026-10-21)

The user tried it on another sandbox: it worked (the warning on a conversation under way, a new conversation on
Haiku answered and the reported model matched, effort switched). Then asked for:

- **Centred.** The bar under the prompt is a grid, `1fr auto 1fr`: Stop on the left, the drop-downs in the middle,
  Send on the right, so Stop appearing doesn't move the middle. In a column under 920px (raised from 820 when the drop-downs got wider and Send began to wrap; a container query on
  the form: the column, not the window, is what counts) the drop-downs take a row of their own above Stop and
  Send. What the agent ran has a slot of its own (fixed width, an ellipsis, the full text on hover).
- **An "i" button with a bubble**, over the button, taking no room. It says that a change applies from the next
  prompt, with **Don't show me this again** (kept in the browser, `vibe-companion-choice-quiet`: each sandbox's
  page is its own origin, so once per sandbox for now; vibe-wide later if it annoys); after it, the button
  still carries a dot and opens the bubble when pressed. The agent having run another model than asked for is
  said in the same bubble, in amber, at the end of **every** turn it happens, with no tick to hide it. It isn't
  said on a page just opened (the caption and the button show it, though). Closed by its button, by Escape
  inside it (Escape in the prompt box stays the agent's Stop), and by a click elsewhere.
- **Failures.** What the user asked for that couldn't be kept is a modal dialog over a frosted page (the
  `.offline` glass, on `dialog.glass::backdrop`). A failure to get the lists when the page opens is not
  something the user did, so it is in the bubble, opened, with **Retry**, not in the way: **this departs from
  "errors in a dialog"** for that one case, and is easy to change.
- The conversation-under-way warning is unchanged: asked every time, no tick.
- Tests: the jsdom file (25), and three in real Firefox for the layout (the group doesn't move as Stop comes and
  goes at three widths, is centred, the bubble moves nothing and stays on the page); the Firefox page is now
  served with a fake agent and choice for that.

### The agent's lists are kept (2026-10-21)

Asked for by the user: the first time, a plain fetch with nothing known; after that, the last lists are shown
at once and the agent is asked again in the background (stale while revalidate).

- **Per sandbox** (the user's decision, after weighing per agent): `companion/<name>.model-options.json` under
  vibe's home (`state.ModelOptionsPath`), removed with the sandbox by `Remove` and moved by `Move`. The lists
  depend on the `claude` in that sandbox, the account and a project's settings, so another sandbox's would
  not be the truth. Written whole (`fscopy.WriteWhole`) with a version and when it was asked.
- A file that can't be read, is another version, or lists no models is **said** (one note naming it) and not
  used: it's a cache, so the agent is asked as if nothing were kept.
- The host serves what's kept at once (`GET /api/conversation/choice` carries `options`, `optionsAt`,
  `optionsFresh`) and the page then asks (`POST .../choice/options`, no waking). A fresh answer replaces the kept
  one in memory and on disk; a failure to write it is logged, and doesn't fail the request.
- **Not held to a stale list:** an effort is checked against the agent's list only once it has been asked in
  this run; before that, by shape. A list from an earlier run may lack what the agent now takes, and an unknown
  effort is only ignored (with a warning) by claude anyway.
- **A failed refresh with something kept is quiet:** the kept lists stay, the bubble doesn't open itself, the
  button carries its dot, and the bubble, when asked, says "The lists are from <time>; they couldn't be
  refreshed: <reason>" with **Retry** (which wakes the sandbox). With nothing kept, the failure is as before
  (the bubble opens, amber, with Retry).
- A known small untruth: "Default (Opus 5.5)" is part of what's kept, so it can be briefly out of date, until
  the refresh.

### Order, and when the dot goes (2026-10-21)

- **Models are listed biggest first** where the size is known: fable, opus, sonnet, haiku (`modelchoice.BySize`,
  matching the family as a whole word in the name: `opus[1m]` and `claude-opus-5-5` are opus, `opusplan`
  isn't). Any name that isn't one of those (`best`, `opusplan`) comes after them, alphabetically: no size is
  claimed for it. Applied when the lists are read from the agent and again when read from the kept file, so
  lists kept before this show in the new order. Fable's place at the top is from the user's screenshot of
  claude.ai's own picker (Fable 5.1 "for your toughest challenges", then Opus, Sonnet, Haiku), not from
  anything the agent reports; it's one entry in `sizeOrder`.
- **The dot on the "i" says something hasn't been looked at.** It goes when the bubble is closed, whichever way
  (the X, the button, Escape, a click elsewhere), and comes back with the next change, the next turn that ran
  another model, or a new failure to refresh. The amber outline stays while the cause does (another model
  ran; no lists), since that is a state, not a notice.

### While the lists load (2026-10-21)

- **Nothing known** (first fetch in a sandbox, or before vibe has said what is chosen): the whole widget is dimmed
  and disabled, a spinner and "Loading models…" in the slot beside it, `aria-busy`, and a visually hidden status
  for screen readers. The stored choice is still shown. **Kept lists:** usable, only the spinner (the user's
  reason: if the lists change while they have it open, nothing should surprise them).
- **A limit on the lock:** 30 seconds (`CHOICE_LOCK_MS`, the same patience as the clipboard page's), then the
  drop-downs are usable, the bubble says it is still asking, and the lists fill in when they arrive. A
  Retry locks it again, the same way. The number is a guess; the user agreed to a limit, not to 30.
- **The slot only speaks when it's news.** "Running <model>" is gone when it matches what was asked (or the default
  is chosen): the user expects the drop-downs to work. The bubble says what the last turn ran. A mismatch is
  still amber, still said at the end of every such turn.
- **Found in a real browser, not jsdom:** a drop-down sizes itself to its longest option, so the group got 70px
  wider when the lists arrived and everything moved. The two now have widths of their own (`14em`, `7em`), and a
  Firefox test checks that what they hold changes nothing.

### Versions in the list (2026-10-21)

- The agent's list gives only aliases (`opus`), which follow the latest model, so the version isn't in it. A
  `/model <alias>` prompt answers locally, with no model call, "Set model to `Opus 5.5` for this session only": vibe
  asks for every listed alias (but `default`) in **one** `sbx exec` loop and keeps the names with the lists
  (`Options.Names`; `modelchoice.ParseModelName`). Real answers, claude 2.1.306: fable → Fable 5.1, opus → Opus 5.5,
  sonnet → Sonnet 5.5, haiku → Haiku 5.5, `opus[1m]` → Opus 5.5 (1M context), `sonnet[1m]` → Sonnet 5.5 (no
  mention of the 1M: the agent's own wording), `best` → Fable 5.1 (what it stands for now), `opusplan` → Opus in plan
  mode, else Sonnet.
- **Shown:** the plain models by that name alone ("Opus 5.5": what claude.ai's picker says); the rest, in
  **Other…** and when one is chosen, as `Name · alias` (changed from `alias — name` on 2026-10-21, to lead with a
  title-case name); an alias with no name as itself, **in title case** ("Opus[1M]"), as are the efforts ("Low",
  "High"), `xhigh` being "Extra High" (the one special case, the user's choice over "Xhigh"). Title case is only for the short names the agent hands out: a full model id typed under **Other…**
  is exact, and is shown as typed. What is kept and sent is never changed. The value kept in
  `settings.yaml` stays the alias, so a choice still means "the latest opus".
- A failure to ask for the names doesn't fail the lists: it's said in vibe's output (a note), the page shows aliases,
  and the next refresh tries again. A name that can't be read leaves out only that one.

### The spinner's place (2026-10-21)

The spinner sits to the left of the widget, ahead of the Model label, in a 14px slot that is there whether or not it
shows, so it coming and going moves nothing (a Firefox test at two widths checks that). The words ("Loading
models…", "ran …") stay in the slot on the right.

### No visible labels (2026-10-21)

The user's idea, after the names carried the versions: "Model" says nothing the entries don't ("Opus 5.5"). Dropped, and
the effort entries now say what they are instead: **Default effort, Low effort, Medium effort, High effort, Extra High
effort, Max effort** (the user's pick of four options; "Default (Medium)" wasn't possible, as claude can't be asked what
its default effort is). The labels stay for screen readers (`sr-only`, still tied to their drop-downs), and both drop-downs
have a tooltip. The effort drop-down is 11em wide for "Extra High effort"; Firefox tests measure that the longest
everyday entries fit, and that Send stays on one line at eleven widths from 520 to 1900px.

### After the review of 2026-10-22

Eleven threads, one P2; all taken up.

- **A change made during a turn** is for the next one, so that turn ending on the old model is no word on it: `pendingTurns`
  (2 when saved while the agent answers, else 1), counted down at each turn end. It used to raise a false, unswitchable-off
  "ran another model" warning.
- **A redraw leaves a drop-down's options alone** unless they differ (an open drop-down is closed by a rebuild).
- **`--effort`'s list** is read only from its own description, up to the next flag, as exactly one parenthesised list of
  two or more lower-case names (at most 20); anything else is an error, not a guess. Tested against help shaped like
  each way it could go wrong.
- **`settings.yaml` is written whole-and-rename** (`fscopy.WriteWholeMode`, keeping the file's mode), so a turn starting as
  the choice changes reads one choice or the other, never an empty file (which meant the default model, quietly).
  See the memory note on atomic writes.
- **What the sandbox's `claude` may say is bounded** (it can be replaced by the agent): at most 50 models, names of at most
  100 characters, the default's name at most 200, `--help` 256 KB, the list 64 KB, the names 256 KB; only the aliases
  asked about are taken from the names. What's left out is said in vibe's output.
- **One ask of the agent at a time** (a second page waits for the first's answer, and asks itself only if it failed; one
  that gives up isn't waited for).
- **What is inherited is said:** with `model:` in the global settings, **Default** in the profile can't take it away; the
  page is told which of the two come from there and the bubble says so (and opens when Default was asked for and not given).
- **`mode:` is layered like every other setting:** the profile's `settings.yaml` over the base one.
- **Which names pick by a rule** (`best`, `opusplan`) is said by the host (`Options.Rules`), once, in Go; the page no longer has its own list.
