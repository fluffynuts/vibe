# Cursor in the Conversation tab: discovery

Status: discovery 2026-10-26 on `feat/support-cursor`; nothing built. Cursor Agent CLI `2026.10.22-27dcf5f`,
logged in (browser flow) on a free plan, Auto model only. Real output is in `docs/samples/cursor/`.

Approach agreed with the user: find out how Cursor really behaves first, keep the design parallel to Claude's,
and only generalise (the "agent description" in plan-conversation-other-agents.md) if it falls out of having two.

## What we saw

- **One process per turn**, like Claude's `-p`: `cursor-agent --trust --force -p --output-format stream-json
  [--stream-partial-output] [--resume <session_id>] "<prompt>"`. `--force` is the bypass-permissions equivalent
  (the sandbox is the boundary); `--trust` stops the workspace-trust prompt. No stream-json input.
- **Envelope is Claude-like, details differ.** JSON lines. `system`/`init` carries `session_id`, `model` (display
  name, e.g. "Auto"), `cwd`, `permissionMode`. Then `user` (echo of the prompt), `assistant`, `result`
  (`subtype: success`, `is_error`, `result` text, `duration_ms`, `request_id`, `usage` with `inputTokens` /
  `outputTokens` / `cacheReadTokens` / `cacheWriteTokens`; no cost).
- **`thinking` events** (`delta` with `text`, then `completed`) appear even without `--stream-partial-output`.
- **`assistant` messages** are plain `{role, content:[{type:text,text}]}`. With `--stream-partial-output`, an
  `assistant` line carrying `timestamp_ms` appeared before the final one without it (text "pong" twice): how to tell
  a delta from the final message is not yet pinned down; a text turn with more than one word is needed.
- **Tools are `tool_call` events**, not content blocks: `subtype` `started` then `completed`, a `call_id`, and a
  `tool_call` object keyed by kind: `readToolCall`, `editToolCall`, `shellToolCall` (each with `args` and, on
  completion, `result.success`). The `result` text of the turn concatenates all assistant text with no separator
  ("…shows up.README.md says…").
- **Resume works** (`--resume <session_id>`): the same `session_id`, context kept (it knew `notes.txt`).
- **Models:** `cursor-agent --list-models` prints `id - Display name` lines (~40, effort/fast baked into the id,
  e.g. `gpt-5.6-sol-high-fast`), so effort is not a separate flag for most. Help also mentions bracket overrides
  (`claude-opus-4-8[context=1m,effort=high,fast=false]`), not tried.
- **Free plan + named model fails with exit 1 and NO error event.** The stream has only `init` and `user`; the
  message ("ActionRequiredError: Named models unavailable Free plans can only use Auto…") is on **stderr**. vibe
  must capture stderr and show it.
- **`--model` is sticky.** One run with `--model <x>` rewrote `~/.cursor/cli-config.json`'s `model`, so later runs
  without `--model` used it too (and failed on the free plan) until `--model auto` was passed. Anything vibe does
  with a model must always pass `--model` explicitly, `auto` included, or it will leak into the user's own use.
  (Also: running a shell tool appended `Shell(ls)` to the config's allow list.)
- **Sessions on disk:** `~/.cursor/chats/<workspace hash>/<session_id>/{meta.json,store.db}` (SQLite). Keeping a
  session across a sandbox rebuild means copying that folder, as with Claude's transcripts; not yet tried.
- **Network:** `cursor.com`, `downloads.cursor.com` (install, needed approval), `api2.cursor.sh`,
  `agent.api5.cursor.sh` answered. Login is a browser link (`cursor-agent login`) valid for the process's lifetime.
- Cold turn ~3–9 s on Auto.

## Not seen yet

- How to stop a turn (SIGINT vs SIGTERM) and what's left on disk.
- Text-turn partial streaming shape for longer answers; whether `result` can be trusted vs summing `assistant`.
- Slash commands/skills for the page's list (Cursor has skills under `~/.cursor/skills-cursor`).
- Error shapes beyond named-model: bad session id, rate limit, network loss; exit codes.
- A real vibe (`sbx`) cursor sandbox: all of this was the scratch CLI in this sandbox.
- Anything with a paid model (effort/fast ids, `thinking` shape on other models): the user will find a tester.

## Plan and model availability (probed 2026-10-26, free plan)

- `cursor-agent about --format json` reports `subscriptionTier` (here `"Free"`): the plan is readable without a probe.
- `--list-models` has **265 ids** (not ~40), effort/fast baked into the ids.
- Probed the first 159 named ids with `--model <id> "test"`: **all 159** failed identically (exit 1, the same stderr
  line, 6–10 s each). Not probed: the last ~105 ids. Results: `docs/samples/cursor/probe-named-models-free-plan.tsv`.
- (Superseded: the plan is not readable under an API key; see the first play-test.) A full probe is therefore ~25 min and rewrites the sticky `cli-config.json` model on each run: unfit as a startup
  or on-demand job. The tier is the cheap, honest signal; what a paid tier unlocks is not known (no paid account).

### Decision (user, 2026-10-26)

Model list = all of `--list-models`. When `subscriptionTier` is `Free`, only Auto is offered as usable (the rest
shown disabled with the reason; picking one warns and falls back to Auto). On any other tier nothing is disabled;
a turn that fails with `ActionRequiredError` shows Cursor's stderr and offers Auto. Revisit if free opens up more.

## More discovery (2026-10-26)

- **Partial streaming rule.** With `--stream-partial-output`, a streamed fragment is an `assistant` line with
  `timestamp_ms` and no `model_call_id`. Two lines repeat text already streamed: one with `timestamp_ms` + `model_call_id`
  (a finished segment, emitted before its tool calls) and the last one with neither (the whole final segment). A client
  that renders deltas must drop both. Without the flag, every `assistant` line is a complete message and is kept.
  (`docs/samples/cursor/turn-with-tools-partial.jsonl`, `turn-text-only-partial-long.jsonl`.)
- **Stopping a turn.** SIGINT and SIGTERM both end the process in ~0.25 s. SIGINT: exit 130 and "Aborting operation..."
  on stderr. No `result` event: the stream just ends (mid-`tool_call` here). The shell command the agent had started
  (`sleep 60`) is **orphaned and keeps running**, so stopping needs to kill the process group or the tool's children
  too. SIGTERM's exit code was not measured.
- **An unknown `--resume` id does not fail.** It starts a new empty session with that id, exit 0, and answers as if
  nothing were wrong. A session lost on a sandbox rebuild would therefore silently lose its context: vibe has to check
  the session exists (`~/.cursor/chats/<hash>/<id>`) before resuming, and say so if not.
- Worth knowing for tests: `kill $(pgrep -x sleep)` also matches the sandbox's keepalive (`sleep infinity`, pid 3) and
  takes the whole sandbox down; match on `sleep 60` explicitly.

## What was built (2026-10-26, for play-testing; uncommitted)

Parallel to Claude's path on purpose, so what is shared and what isn't can be seen when a third agent comes.

| | Claude | Cursor |
|---|---|---|
| command | `conversationCommand` (`src/vibe/conversation.go`) | `cursorCommand` (`src/vibe/cursor.go`) |
| turn | `converse` | `converseCursor` |
| what the page gets | the agent's lines as they are | `internal/cursorstream.Translate`: Claude's shape (assistant text, tool_use, tool_result, result) |
| session kept across rebuilds | `~/.claude` (`sandboxTranscripts`) | `~/.cursor/chats` (`sandboxCursorSessions`), via the same `keepingTranscript` |
| model/effort drop-downs | yes | **no** (hidden). No `--model` is passed, so the user's own (sticky) choice stands; a turn that fails with "Named models unavailable" is said so on the page and run once more with `--model auto` (`converseCursor`) |

Shared as it was: `wrapTurn`/`stopTurn` (stop kills the turn's process tree, which also catches Cursor's orphaned
tool children), `asAgent`, `keepingTranscript`, the page.

Added for Cursor (and available to any agent): `transcripts.lost` + a `vibe_note` line when a resumed session is
missing; `turnLog` (`src/vibe/turnlog.go`) and `vibe --turn-log [path]`, which prints what the last turns said and
how they ended, stderr included; the page draws `vibe_note` as a note.

Not built: the model drop-down (tier from `cursor-agent about --format json`; see the decision above), slash
commands, the sandbox login (a rebuilt sandbox loses `~/.config/cursor`), anything for rate limits.
Unverified: the binary name inside a real sbx cursor sandbox (`cursor-agent`, else `agent`), that its `~/.cursor`
is where the sessions go, and that `Agent` is recorded as `cursor` in the instance (`vibe --info` shows it).

### First real play-test (2026-10-26, user's sbx cursor sandbox)

The path worked end to end through sbx. The first prompt failed with "Named models unavailable…": the sandbox's
Cursor config had the Opus model the user had picked in its TUI (sticky), and with no model drop-down there was no
way to get back to Auto.

First fix, which did not work: ask `cursor-agent about --format json` for `subscriptionTier` and pass `--model auto`
on Free. In the sandbox Cursor authenticates with an API key from the environment (`"apiKeySource":"env"`, not the
browser login used for discovery), and with a key `about` says `"subscriptionTier": null` and `userEmail: null`. The
plan can't be read there, and the tier approach was removed (it also cost an extra Cursor start every turn).

Second fix: react to the failure. The error is specific, comes ~2 s in with no tokens spent, and is on stderr: the
turn is run once more with `--model auto`, after a note on the page saying so. Verified against the real CLI with a
sticky Opus (plain turn fails with that text, `auto` works and resets the remembered model, a plain turn then works).
The retry itself is covered by unit tests with a stand-in for `sbxrun.ExecStream`; not yet seen through sbx.

## Model and effort pickers, slice 1 (2026-10-26, built, awaiting the user's test)

Decisions (user): list every family; effort follows the model; a model with no efforts shows the single entry
"Default effort"; "fast" is part of the effort entries ("High effort · Fast"), so the page keeps its two drop-downs;
ids that don't fit the split are shown as they are (the user will report what looks wrong). Greying and recovery are
slice 2, tested after this one.

- `internal/cursormodels`: reads `--list-models` into families. Effort = the last word of the id before `fast`
  (and before a trailing `thinking`); `-extra-high` is xhigh. 265 ids → 55 families, tested on the real list.
- Stored as Cursor's own id in the profile's `model` (effort left empty), e.g. `gpt-5.6-sol-high-fast`. The page's
  (family, effort) are worked out from the id, and back from the list when a choice is kept. A stored model that is
  not in the list (left by claude, say) is not shown as chosen and not run; with no list kept yet, none is named.
- A family with no plain id has no Cursor default: "Default effort" there means `high`, else `medium`, else its
  first; the drop-down then shows what it chose.
- Page: `Options` gained `effortsByModel`, `effortNames`, `onlyListed` (no "Other…") and `note` (in the info
  bubble: a chosen model also becomes Cursor's own). Claude's pickers are unchanged (tests pin that).
- Turn: `--model <id>` only when a model is chosen; the "Named models unavailable" retry on Auto stays (and now
  does not retry when Auto itself was the model).
- Known until slice 2: on a free plan a chosen non-Auto model fails, is said so and re-run on Auto, on every turn.
- Not seen in a real sbx sandbox yet: the list as the sandbox's Cursor prints it (269 ids there, 265 in the sample).

### Tested for real by the user (2026-10-26, their sbx cursor sandbox, free plan)

Worked: prompt/answer with tools, the model/effort pickers filling from the sandbox's list, picking Opus → refused →
note → answer on Auto → the selector set back to Auto at once. Not yet tested for real: Stop mid-tool, a second turn
resuming the session, `--re-compose` (session copy + the "no record" note), `vibe --turn-log` after a failure,
paid plans (nothing here can), slice 2 (greying, recovery).

### Two places Cursor keeps a conversation (found 2026-10-26, while checking a "too good" result)

- **The session**: `~/.cursor/chats/<hash>/<uuid>/{meta.json,store.db}`. What `--resume <uuid>` loads, and what vibe
  keeps and puts back across a rebuild. Verified: delete it and a resume starts empty (Cursor recreates an empty
  folder; nothing is kept server-side under the id).
- **The transcripts**: `~/.cursor/projects/<folder-slug>/agent-transcripts/<uuid>/<uuid>.jsonl`, plain text per chat.
  Not loaded by `--resume`, but the agent *searches them itself* (glob, grep, read) when asked about an earlier
  chat: a brand-new session asked "what number did I ask you to remember?" found it that way. So deleting only the
  session (and vibe's copy) does not make the agent forget; a test of the restore must delete both, or ask
  something a search can't answer. A rebuild loses these (vibe doesn't carry them): cross-chat recall of
  conversations from before a rebuild is lost; the resumed conversation itself is not.
- Verified for real after `--re-compose` (user, their sbx): the session folder appeared in the new sandbox before the
  first prompt (parent folder's change time), and the agent knew the number: restore works.
- Confirmed by the user in their sbx sandbox: with both the session and the transcript deleted, a new conversation
  has no idea of the earlier number. So all of the earlier recall was the session or the transcript search; nothing
  server-side, no Cursor "memories".

### Tested for real, summary (2026-10-26)

Passed in the user's sandbox: tools and answers; pickers; Opus on free → note → Auto → selector set back at once; Stop
(Escape) kills the turn's tool child; a second prompt resumes the session; `--re-compose` restores the session; the
lost-session note shows when there is no session (and the agent can still search transcripts if they exist).
Not yet: `--turn-log` after a real failure (hide `~/.local/bin/cursor-agent` and `agent`, prompt, `vibe --turn-log`,
restore them); paid plans; slice 2.

Test 4 (user, 2026-10-26): with `cursor-agent` and `agent` moved away the page showed the real stderr in red
(`exit status 127: bash: line 1: agent: command not found`), and worked again once they were back. That message
said nothing of two names being looked for, so the prelude now says Cursor's agent isn't installed, names both, and
points at `vibe --info` (still exit 127). The `--turn-log` side of this test: awaiting the user's output.

Test 4 done (user, 2026-10-27): `--turn-log` shows the failed turn (`+0.3s  [failed: exit status 127: bash: line 1:
agent: command not found]`, with the session and model it asked for), the turns before and after it, and the earlier
`[stopped from the page]`.

Measured from that log: a refused named-model prompt cost 6.7–9.1 s before the fallback set the choice back to Auto
(six of them); and a turn that fails at once took 0.3 s start to end, sbx exec and login shell included, so the
4–6 s before a successful turn's first line is Cursor's own startup, not vibe's.

### Everything tested for real so far
Tools and answers; pickers; the Opus fallback (note, Auto, selector set back at once); Stop; session resume;
`--re-compose` restore; the lost-session note; `--turn-log` after a failure. Not tested: paid plans, slice 2.

### Transcripts are kept with the session (2026-10-27, user's decision)

The user's view: the session is the L1 cache and the transcripts the L2; the agent leans on them for what it can't
tell from the conversation (after compaction, say). So the kept copy now holds the session and the transcripts of
every chat of the sandbox (`projects/*/agent-transcripts`), up to 10 MB of them (past that the session is still
kept, the transcripts aren't), and a restore never writes over a file the rebuilt sandbox already has (a chat had in
its own terminal since is newer). `cursorFetchScript` and `cursorPutScript` are run for real in tests (Linux only: GNU
tar, `base64 -w0`).

`vibe --turn-log` keeps needing sbx: the user's call is that sbx is a bedrock dependency of vibe, so that is not a
loose end.
