# Plan: the Conversation tab for agents other than Claude

Status: planned 2026-10-21; OpenCode 1.19.3 checked for real the same day, on a free model (findings below; real
output saved in `docs/samples/opencode/`). Not started. Codex not looked at.

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
- **A per-model effort list** (confirmed real). `opencode models --verbose` prints each model's JSON, including `variants`. It
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
  be a better way to stream and to stop a turn), `opencode acp` (see "ACP" below), and `opencode session`.

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

## What the real OpenCode run showed (1.19.3, free models, 2026-10-21)

Inference needed `opencode.ai:443` allowed on the host; before that the first call was a 403 (an `error` event).
Samples of real output: `docs/samples/opencode/` (a text-only turn, a turn with a tool call, the two error cases,
and `opencode models opencode --verbose`).

**The event stream** (`--format json`, one JSON object a line, each with `type`, `timestamp`, `sessionID`, `part`):
`step_start`, `text` (`part.text`), `tool_use` (`part.tool`, `part.state.status`/`input`/`output`; **only once the
tool has finished**, so the page can't show "running a tool" live, only what it did), `step_finish` (`part.reason`:
`tool-calls` means another step follows, `stop` means the turn is over; `part.tokens`, `part.cost`), `error`
(`error.name`, `error.data`). The session id is on every line, from the first. The model that answered is **not**
in the stream (Claude's `init` says; here the page can only show what was asked). A turn ends when `step_finish`
says `stop`; the process then exits 0.

**Resuming:** `-s <id>` carries the earlier conversation (it answered about the first message), and `-m` with a
different model mid-session works. A turn killed mid-way leaves a session that resumes.

**Errors are poor and exit 1:** an unknown model (`opencode/nonsense`, or an unknown provider) gives an `error`
event "Unexpected server error. Check server logs for details." with a `ref`; a `--variant` the model doesn't
list gives the same. (Claude, by contrast, said what was wrong, and ignored an unknown effort with a warning.) So
the page validates against the model list *before* sending, and says what is wrong in the user's terms.
`--variant high` on a model with *no* variants was accepted silently.

**Permissions:** without `--auto` a request for something outside the project is auto-rejected ("permission
requested: external_directory …; auto-rejecting", on stderr) and the tool_use comes back as an error: it doesn't
hang, and the agent can't do the work. With `--auto` it ran a command writing outside the project. For parity with
Claude's `bypassPermissions` the turn needs `--auto`.

**Stopping:** SIGTERM ends it. SIGINT sent mid-turn did **not** end it within 3 seconds in the one run seen
(it may take longer; not pursued). How vibe stops a Claude turn today needs reading before deciding.

**A runaway is possible:** a free model given a vague prompt wrote and re-edited files in a loop for minutes.
That is the agent being an agent; but it makes Stop matter, and a turn that never ends needs the user's Stop
(vibe's existing backstops apply).

**Keeping a session across a rebuild:** copying `~/.local/share/opencode` (the database and its `-wal` and `-shm`
files, 5.6 MB after an afternoon) into a fresh home worked: the session resumed. `opencode export` then
`opencode import` into a fresh home listed the session but **resuming it hung silently with no output** (twice,
including from the original directory). So the copy is the way, taken when no turn is running; export/import is not
to be relied on. A hang with no output is the kind of thing vibe's backstops must cover for any agent.

**Not seen / still to find out:**

1. How the sandbox's `opencode` is installed and signed in (sbx's own image; agent user, `PATH`, home as
   `conversationCommand` assumes for Claude): the above ran with an npm install in a scratch folder on the
   host-side sandbox, not in an sbx `opencode` sandbox.
2. Which hosts a free model needs besides `opencode.ai:443`, and whether `models.dev` (the model list) needs
   one: the list worked without an approval in this run, perhaps from a bundled copy.
3. A SIGINT that does end a turn (or whether only SIGTERM does).
4. Whether `opencode acp` (or `serve`/`--attach`) is a better channel than `run`: see the ACP section below.
5. Images: whether a model that takes them can be sent one (`-f`).
6. Slash commands and a cost line: nothing seen in the stream; cost is `part.cost` per step (0 on free models).

## ACP (Agent Client Protocol): a possible common channel

Raised by the user 2026-10-21; looked at lightly, **nothing has been run over it**.

- `opencode acp` exists ("start ACP (Agent Client Protocol) server": seen in OpenCode's own `--help`).
- From the protocol's own pages (as found by a web search, not read in the repositories; the registry snapshots
  disagreed on version numbers): ACP began with the Zed editor; it is JSON-RPC between a client and a coding
  agent, with a shared shape for prompts, streamed output, tool calls, permission requests and cancel. It is
  **not** IBM's "Agent Communication Protocol", which shares the initials.
- Listed there as ACP agents: OpenCode, Gemini CLI, Goose, Kimi CLI and others. **Claude Code and Codex CLI are
  listed through adapters** (Zed publishes them), not natively: `claude --help` (2.1.306) shows no `acp` command.
  An adapter is one more program to install and keep current in the sandbox, and another party's code between
  vibe and the agent.
- What it might give (from the protocol's description, unproven): one reader for several agents; a defined cancel
  (instead of the SIGINT/SIGTERM guesswork above); permission requests handled instead of bypassed with `--auto`;
  the answering model and live tool events, which `opencode run --format json` lacks.
- Likely shape: keep Claude on `claude -p` stream-json (tested, works); use ACP for agents that speak it
  natively, OpenCode first. Per-agent readers may remain for Claude and Codex.
- **First step when this is picked up:** a read-only check. Start `opencode acp` in a scratch folder, drive a turn
  by hand, and compare what it reports (model, tool events, cancel, permissions) with `run`. Decide on ACP from
  that, before writing any per-agent reader.

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
4. Branch: this is built on its own branch, after the model/effort feature is out (user, 2026-10-21).
