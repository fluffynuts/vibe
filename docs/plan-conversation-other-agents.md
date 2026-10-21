# Plan: the Conversation tab for agents other than Claude

Status: planned 2026-10-21, first look at OpenCode done the same day (below). Not started. Nothing here has seen a
*successful* OpenCode turn yet: the sandbox's firewall blocked inference (see "Still to find out").

## The outcome

Whichever agent a sandbox runs, the Conversation tab works: the user sends a prompt from the page, sees the agent
work, stops it, and carries on the next day. OpenCode first, since it is where truly free models are, so vibe is
usable at no cost. Then Codex, tried by someone with a subscription. Every other agent is left with the same
support and relies on user reports.

This is the groundwork for [plan-model-effort.md](plan-model-effort.md): the model and effort drop-downs need
the agent's own lists and flags, so they ride on this. Build Claude's drop-downs first; this plan follows.

## What's Claude-only today (read from the code, 2026-10-21)

It is a gate and some hard-coding, not a design that excludes others:

- `conversationFor` (`src/vibe/companion.go:227`) returns nothing for any agent but `claude`, so the page has no
  Conversation tab for them.
- The turn command is Claude's: `agentArgs` in `src/vibe/conversation.go:23` (`claude -p --output-format
  stream-json --verbose --permission-mode bypassPermissions`, plus `--resume <id>`).
- The page's reading of the agent's output is Claude's stream: `system`/`init` (session id, slash commands),
  `system`/`status`, `assistant`, `result` (`internal/companion/conversation.go`, around line 258).
- The session id is read from a line's `session_id` (`src/vibe/conversation.go`, around line 201).
- The kept copy of the agent's transcript (so a session survives a rebuild) reaches into the sandbox's
  `~/.claude/projects/...` (`sandboxTranscripts`).
- The slash commands the page offers come from Claude's `init` line.

So the work is: describe an agent (its command, how to read its output, where it keeps a session), and have the
rest of vibe ask the description. Claude becomes the first description, with behaviour unchanged.

## What OpenCode looks like (CLI 1.19.3, installed with npm into a scratch folder, 2026-10-21)

Real output of `opencode run --help` and `opencode models`, not a guess:

- **One prompt per process**, like Claude's `-p`: `opencode run [message..]`.
- **Machine output:** `--format json` ("raw JSON events"). A community write-up (secondhand, not the official
  docs, which the sandbox couldn't fetch) says the events are JSON lines with a `type` of `step_start`,
  `tool_use`, `text`, `step_finish` and `error`, each carrying a `sessionID` of the form `ses_…`. One real
  line seen: `{"type":"error","timestamp":…,"sessionID":"ses_…","error":{"name":"APIError","data":{"message":…,
  "statusCode":403,…}}}`.
- **Model:** `-m provider/model`, e.g. `opencode/big-pickle`.
- **Resume:** `-s <session id>` ("session id to continue"); `-c` continues the last one; `--fork` branches.
- **Effort:** `--variant` ("model variant (provider-specific reasoning effort, e.g., high, max, minimal)").
- **Permissions:** `--auto` ("auto-approve permissions that are not explicitly denied (dangerous!)"). The sandbox
  already is the boundary, as it is for Claude's `bypassPermissions`; to be confirmed that a turn without it
  doesn't wait for an answer nobody can give.
- **The live model list:** `opencode models` (add a provider to filter). Twelve at present, all under the
  `opencode/` provider and listed with a cost of 0: big-pickle, cat-jam-free, deepseek-v4.1-flash-free,
  glm-5.3-flash-free, ll-alpha-free, longcat-2.5-preview-free, mimo-v2.6-flash-free,
  muse-spark-1.3-contributor-free, nemotron-3-ultra-free, nemotron-3.5-lightning-free,
  nemotron-3.5-super-vl-free, space-bunny-free. The list worked with no sign-in.
- **A per-model effort list.** `opencode models --verbose` prints each model's JSON, including `variants`. It
  differs by model and can be empty:

  | model | variants |
  |---|---|
  | big-pickle, mimo-v2.6-flash-free, nemotron-3-ultra-free, nemotron-3.5-lightning-free | none |
  | deepseek-v4.1-flash-free, glm-5.3-flash-free | low, high, max |
  | longcat-2.5-preview-free | low, medium, high |
  | cat-jam-free, ll-alpha-free, space-bunny-free | low, medium, high, xhigh, max |
  | muse-spark-1.3-contributor-free | minimal, low, medium, high, xhigh |
  | nemotron-3.5-super-vl-free | none, minimal, low, medium, high, xhigh |

  So for OpenCode the effort drop-down must follow the chosen model, and be absent for a model with none. (Claude
  gave no such list.) Each model also reports whether it takes tool calls (`capabilities.toolcall`): a model that
  can't use tools can't do the agent's work, and the page should not offer it as if it could.
- **Sessions live in a database**, `~/.local/share/opencode/opencode.db`, not in per-session files. `opencode
  export [sessionID]` and `opencode import <file>` move a session as JSON: the likely way to keep a copy that
  survives a rebuild, in place of Claude's folder of transcripts.
- Other things the CLI offers that may matter: `opencode serve` / `--attach` (a server to attach to, which might
  be a better way to stream and to stop a turn), `opencode acp` (the Agent Client Protocol: a standard some agents
  speak, which could serve several agents with one reader; worth a look before writing a reader per agent), and
  `opencode session`.

## Design

### An agent description

One place says, for an agent: the turn's command (with the prompt, the session to resume, and, later, the model
and effort), how to read a line of its output into what the page already shows (text, a tool being used, the end
of the turn, an error, the session id), where its session lives and how to keep a copy, and its lists of models
and efforts. Claude's is written first from what is in the code, with the existing tests passing unchanged: that
is the proof it is a refactor and not a behaviour change.

The page and the host speak of the description's *events*, not Claude's lines. Today the page's reading of
`assistant`/`result` etc. lives in `internal/companion/conversation.go`; it moves behind the description.

### What the page can promise per agent

Not every agent can do everything Claude does, and the page says so rather than pretending:

- **Slash commands:** only where the agent reports them.
- **Cost line:** only where the output has one.
- **Stop:** ending the turn's process. To be proven for OpenCode (does it leave a half-written session? does a
  child process outlive it?).
- **Pasted images:** only where the model takes them (`capabilities.input.image`).
- **Resume across a rebuild:** where the agent has a way to export and import a session; if not, say so, and the
  conversation starts fresh after a rebuild instead of failing.

## Still to find out (needs a turn that gets through)

The sandbox's firewall answered the first OpenCode call with 403 "Approval required for opencode.ai:443"
(`https://opencode.ai/inference/...`). So no successful run has been seen. To finish the check the user approves it on
the host (`sbx policy approval ls`, then `sbx policy approval respond <id> --option <option>`). Then, with the
scratch install (`/tmp/oc-install`, to be deleted), capture:

1. A whole successful `--format json` turn: every event type and its fields, including how a tool call and the
   end of a turn look, whether a cost or token count is reported, and the session id's first appearance.
2. `-s <id>` resuming: does it carry the earlier conversation, and does `-m` with another model work mid-session?
3. Whether a turn waits for a permission answer without `--auto`, and what a denied tool looks like.
4. Stopping: kill the process mid-turn; is the session left usable?
5. `opencode export` / `import` round-trip: does a session survive moving to a fresh home?
6. A bad model name: how it fails, and whether it is a line in the stream or only stderr.
7. `--variant` with a value the model doesn't list: ignored (as Claude does), an error, or mapped?
8. How the sandbox's `opencode` is installed and signed in (sbx's own image; whether its agent user, `PATH` and
   home match what `conversationCommand` assumes for Claude).
9. Whether the free models need the `opencode.ai` inference endpoint only (the one blocked), or others too, so the
   user knows what to allow.
10. Whether `opencode acp` could be the one reader for several agents (Codex too).

For Codex: the same list against its CLI, by someone with a subscription (or a free way, if one turns up). Until
then, Codex gets a description written from its documentation, marked untested.

## Tests

- Unit (Go): Claude's description produces the same command and the same events as today (the existing tests pass
  untouched); an OpenCode description reads recorded real lines (from the check above) into the page's events,
  including an error line; an unknown line is kept as unread, not dropped or crashed on.
- Unit (jsdom): the tab appears for an agent with a description and not for one without; what is unavailable (slash
  commands, cost, images) isn't shown.
- Integration: the stand-in agent the tests use today speaks Claude's format; add one that speaks OpenCode's, from
  the recorded lines, to prove a whole turn, a stop, and a resume through the real command path.
- One manual run against real OpenCode, with a free model, by the user or with their go-ahead.

## Not in scope here

- The model and effort drop-downs (their own plan; this one makes them possible for other agents).
- Terminal use of any agent: unchanged.
- Agents with no usable non-interactive mode: they get no tab, and the page says why instead of showing nothing.

## Decisions of 2026-10-21

1. This is a separate plan from model/effort, built after Claude's drop-downs.
2. Order: OpenCode (free models, the user tests), then Codex (a friend, or a free route if found), then the rest on
   user reports.
3. Keep the work agent-neutral from the first change; Claude's behaviour must not change while it is made so.
