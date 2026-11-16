# One agent process for the whole conversation, so background work can call back (built 2026-11-16 on `feat/persistent-agent`; tried by the user the same day)

Raised 2026-11-13 by the user, who lost an agent's background work twice by sending a prompt while the conversation looked idle, and who
remembers the console app letting an agent get the call-back later while still being talked to. The next highest-order priority after the
`fix/block-unblocked-host` work: the current behaviour is confusing, and the guard built meanwhile (below) ties the user up when they
could be planning something with the agent.

## What is wanted

An agent that starts something in the background gets told when it finishes, and sees the result, **while the user can go on talking to
it**. A new prompt must never end work the agent left running.

## What is so today (read from the code, and tried)

- Each prompt is its own `claude -p` run: `conversationCommand` builds it, `converse` runs it with `sbx exec -i`, `wrapTurn` records its
  shell's pid in `/tmp/vibe-turn-<id>.pid`, and the run is `--resume`d from the session id the last one reported.
- After its answer (`result`) the process may live on while it has background work: the conversation is then `running && answered`, which
  the page shows as a slow-blinking light and a status line ("The agent has answered, and has something still running. Sending a prompt ends
  it.").
- A prompt in that state calls `conversation.supersede()`: it cancels the turn's context, `converse` runs `stopTurn`, which TERMs (then KILLs)
  the whole tree under the turn's shell. **That ends the background work, by design.** Tried 2026-11-13: a harness background loop died at the
  prompt; a `setsid nohup` loop started the same turn survived it.
- The same ending happens to a run that was `--resume`d later: the next process was told, in its transcript, of a task "that didn't finish
  before the previous session ended", and nothing more.

## What was tried (2026-11-13, a small model, outside vibe)

One `claude -p --input-format stream-json --output-format stream-json --verbose --permission-mode bypassPermissions`, stdin kept open:

- A prompt asked for a 20 s background `sleep`; the agent started it and answered (`result`) at 3 s.
- At 22 s, with **no prompt**, the harness emitted `task_notification` and the agent began a new turn by itself, read the output, and
  answered (`result`) at 25 s.
- A prompt sent at 45 s was answered at once; the process was still alive afterwards.

So the console's behaviour is available headless: one long-lived process, prompts written to its stdin, unprompted turns on the same stream.

## Experiments (2026-11-16: `claude` 2.1.323, `claude-haiku-5-5`, outside vibe, in a neutral folder)

Scripts are in `tools/experiments/persistent-agent/` (see its README); each prints every event with the second it arrived. Raw event logs
were not kept. All as `claude -p --input-format stream-json --output-format stream-json --verbose --permission-mode bypassPermissions`.
The CLI also describes its own protocol in its binary (strings in `claude`): what is marked "stated, not tried" below comes from there.

1. **An unprompted turn** (`e1b`). A prompt starts `sleep 12` in the background and the agent answers (`result` at 2.6 s). At 13.9 s, with
   no prompt: `system/task_notification {status: completed}`, then `system/init`, the agent's text, `result` (15.2 s). No `user` event and
   no `command_lifecycle` frame (those come only for a message that carried a `uuid`, see 3). So a turn is the agent's own when a
   `task_notification` came and none of our prompts has `started`.
2. **Stop** (`e2b`). A foreground tool running, a background task already going, then
   `{"type":"control_request","request_id":…,"request":{"subtype":"interrupt"}}`: at once, `control_response success {still_queued: []}`,
   the foreground tool's `task_notification {status: stopped}`, its `tool_result` "The user doesn't want to proceed", and
   `result subtype=error_during_execution is_error=true` with no text. The process stayed alive (274 MB), and **the background task
   survived and called back 33 s later**, as an unprompted turn in which the agent mentioned the interrupt. An interrupt with nothing
   running answers `success` and does nothing. Stated, not tried: `spare_background_tasks: true` also spares background *agents and
   workflows* (a Stop ends those by default); `send_now`, `for_user_message_uuids`, and `cancel_queued` exist.
3. **A prompt while a turn runs** (`e3`, with a `uuid` on each prompt). `command_lifecycle` frames, keyed by the `uuid` we sent
   (`command_uuid`): `queued` at once, `started` at the next safe point (here right after the tool finished), `completed` (or
   `cancelled`). The second prompt was **folded into the running turn**: one `result` (`turns=2`) answered the second prompt only
   ("second-done"), the first prompt's own request ("reply first-done") was never done, and both commands `completed` together. So
   **one `result` is not one prompt**: receipts must come from `command_lifecycle`, not from results.
4. **`priority: "now"` on a user message** (`e4`) is not an interrupt: the message waited ~10 s for the running tool to end (it was not
   killed), the first turn then ended `terminal_reason: aborted_tools` with no text and its command `cancelled`, and the second
   prompt started and was answered. Stop (2) is the immediate way; `priority: now` is "next boundary, and drop what the turn was doing".
5. **Model** (`e5`). `{"subtype":"set_model","model":"claude-sonnet-5-5"}` → `control_response success {model}`; the next turn's
   `system/init` and assistant message carry the new model; and back. No restart.
6. **Effort** (`e8`, `e9`). A user message `/effort high` is answered by the CLI itself as a `result` (`turns=0`, text "Set effort level to
   high (this session only)…"); `apply_flag_settings {settings:{effortLevel}}` is also accepted (`success`, no payload). The CLI says an
   effort change this way keeps the prompt cache (`init.per_turn_effort_active`). No restart.
7. **An idle process** (`e7`). 274 MB resident after the first turn, 249 MB three minutes later, 2 s of CPU in all; still alive at
   three minutes (no self-exit seen). (No MCP servers, the small model; the real figure is the user's to measure with their sandbox.)
8. **Ending it** (`e1b`, `e6`). Closing stdin with nothing running: exit 0 within a second. With a background task running, the
   process was still alive 20 s after stdin closed (it waits for the task); SIGTERM ended it.
9. **Resuming** (`e6`). A new process with `--resume <session id>` starts with `task_notification {status: stopped, summary:
   "Background shell command didn't finish before the previous session ended"}`, an `init`, and **an empty `result` (`turns=0`)
   before the first prompt's own `result`**; the resumed agent knew of the task from its transcript. (My test left the first process
   running on the same session: don't.)
10. **`system/init` is not "the process started"**: it is sent at the start of every turn, prompted or not. The first `init` also lists
    `capabilities` (`interrupt_receipt_v1`, `interrupt_cancel_queued_v1`, `interrupt_send_now_v1`, `msg_lifecycle_v1`, …): feature-detect on them.

### Through sbx, as vibe runs the agent (2026-11-16, the user's host, sandbox `vibe`)

`run-through-sbx.sh vibe /tmp`: the agent started exactly as `conversationCommand` and `wrapTurn` start one (`sbx exec -i` → the login-shell
script → the pid-file wrapper), vibe's flags, no `--strict-mcp-config`. Log and raw events in `tmp/persistent-agent-sbx/` (not kept in git).

- **Nothing is held back by sbx** (`e10`). A prompt's `queued` frame came back 1–5 ms after it was written, for each of six; a 152,103-byte
  prompt arrived whole (answered `ENDMARK`, the word at its very end). The first `init` came 1.6 s after the first prompt (0.4 s
  without sbx): about a second more to start, once per process. Resident size 283–290 MB.
- **Everything above holds unchanged through sbx**: the unprompted turn (`e1b`), the interrupt with the background task surviving and calling
  back (`e2b`), the mid-turn prompt folded in (`e3`), `set_model` (`e5`).
- **vibe dying leaves the agent running** (`e11`). The `sbx exec -i` client killed: 5 s later the agent's wrapper was still alive in the
  sandbox, with its background task, its output going nowhere. vibe's hard stop (`stopTurnScript`, by the pid file) ended it, the
  background command too. So a restarted vibe must find and end what the last one left (or it runs on, unseen, beside the new one).
- **vibe's hard stop ends a persistent agent and its background work** (`e12`): the whole tree, the background `sleep` too, in 3.6 s; the
  agent said `task_notification {status: stopped}` and exited by SIGTERM (143). Today's Stop script is fit for "end the conversation".

### One more: a prompt that should be its own turn (`e3later`, outside vibe)

The same as `e3` with `"priority": "later"` on the second prompt: it waited, `queued`, until the running turn ended (`result
"first-done"`, first command `completed`), then `started` as **its own turn**, answered `second-done`. So the page can choose: by default a
prompt sent mid-turn is folded into it (the console's behaviour: the agent sees it at the next step, and may answer only it); with `later`,
every prompt gets its own turn and its own answer, in order.

## What the experiments settle, and what they leave (against the numbered list below)

- **1, unprompted turns:** settled (1 above). The page treats a turn started after a `task_notification`, with none of our prompts
  `started`, as the agent's own; the light and the typing bubble follow `init`…`result`, not prompts sent.
- **2, Stop:** settled: the interrupt control request (2 above). Today's kill of the tree stays for a stuck process and for ending the
  conversation. Open: `spare_background_tasks` for background agents (not tried), and whether the page wants a second, harder "end its
  background work too" affordance.
- **3, queued prompts:** settled in kind: the agent's queue folds prompts into the running turn (3), and `priority: now` is different (4).
  The page's own queue and "Waiting for the agent to stop…" state go; a sent prompt is shown queued/started/completed from
  `command_lifecycle`, and the page may need to say when a prompt was folded into a turn that answered the next one.
- **4, model and effort:** settled, and simpler than planned: neither needs a restart (5, 6). Send `set_model` / `/effort <level>` as
  the choice changes, before the next prompt. `turnChoice` stops being a command-line flag set per turn.
- **5, review turns:** unchanged: their own short-lived processes.
- **6, a vibe that stops and starts:** the resume behaviour is known (9): the page should tolerate the empty `result` and say once that
  background work did not survive. Open: how the process is ended when vibe ends (stdin close waits on tasks; TERM ends it).
- **7, cost and idle:** measured once (7): ~250 MB resident per conversation, idle. To decide: whether that is acceptable always, or
  the process is started with the first prompt and ended after a long quiet (it does not end itself).
- **8, other agents:** not looked at.

**Settled since:** the stream through `sbx exec -i` and the login-shell wrapper (no buffering; see "Through sbx"); what vibe dying
leaves (the agent runs on: find it by its pid file and end it at the next start, which needs a pid file per *conversation*, not per turn as
today); ending it (today's hard stop works).

**Still to find out, while building (none blocks starting):** what `cancel_queued` / `send_now` give a page that lets a queued prompt be
taken back or sent at once; `spare_background_tasks` with a background *agent* (a sub-agent, not a shell command); context compaction in a
long-lived process; a larger model over a long conversation; and the OpenCode path. A prompt without a `uuid` gets no lifecycle frames:
vibe always sends one.

## Decided with the user (2026-11-16)

The point of this work is that **background work is not lost when the user prompts while it runs**, and that it can call the agent back.
Not keeping the agent quick to answer: the process lives for as long as there is work that can call back, and no longer.

1. **A prompt sent while the agent works is folded into the running turn**, as the console does (the CLI's default: the agent takes it in at
   its next step). The page shows it as taken in; its receipts come from `command_lifecycle`. Not `priority: later`.
2. **Stop interrupts the turn at once and leaves background work running** (the interrupt carries `spare_background_tasks: true`, so
   background agents are spared too, not only shell commands). Then, only if the stream's `background_tasks_changed` lists any, the page
   says so inline, after the stop, and lets the user choose: "Stopped. The agent still has 2 tasks running in the background: "Run the test
   suite", "Watch the build". **Stop them too** / **Leave them running**". Stop them too sends `stop_task` for each (`e13`). An agent that
   gives no `result` within a few seconds of the interrupt is offered a force stop: today's hard kill, which ends everything.
3. **The agent ends 30 s after it is idle**: its turn over and no background task running (the stream says both). vibe closes its stdin
   and it exits by itself (`e6`). So the sandbox sleeps as it does today: an open `sbx exec -i` keeps a sandbox awake (tried by the user
   2026-11-16 with `sbx exec -i vibe -- sleep 600`), and an idle agent must not. While the agent has work (a turn, or a background task),
   vibe keeps the sandbox awake, as it does for a turn today, or sbx stopping it would end that work.
4. **The next prompt after that starts a new process** that `--resume`s the conversation (a new pid; the same session): the start a prompt
   pays today (about 1.6 s through sbx). A wakeup the agent scheduled with nothing running in the background dies with the idle agent, as
   it does today.
5. **One pid file per conversation**, not per turn, so a vibe started after one that died finds the agent it left (`e11`: it runs on) and
   ends it with the hard stop before starting its own.

## Built (2026-11-16, `feat/persistent-agent`)

As decided above. Commits `54cf4c3` (`sbxrun.ExecSession`: a command in the sandbox whose input stays open), `ad406e7` (the conversation
and the host side), and the page and instructions after them.

- **Host** (`src/vibe/conversation.go`): `startAgent` ends a leftover by the conversation's pid file (`/tmp/vibe-conversation.pid`), then
  starts `claude -p --input-format stream-json --output-format stream-json`, `--resume`ing the session when there is one. `claudeAgent`
  writes each prompt as a `user` line with a uuid, after `set_model` and `apply_flag_settings` (effort) for the choice on the page, so a
  change of model or effort needs no new process. Interrupt (`spare_background_tasks`), `stop_task`, End (closes stdin) and Kill (the
  hard stop, confirmed within 10 s or reported as unconfirmed).
- **Conversation** (`internal/companion/conversation.go`): `Agent` and `StartAgent` replace `Converser`. A prompt is always accepted: it
  starts the agent if there is none (prompts sent while it starts wait for it), or is written to it. Receipts follow `command_lifecycle`;
  the poll carries one per prompt in flight (`receipts`) and the background tasks (`tasks`). A `system/init` with no prompt in flight is an
  unprompted turn (a call-back). The agent is ended 30 s after it has no turn and no tasks, and killed if it has not gone 30 s later.
  The review's turns (start, ask, submit) are prompts to the same agent, as they were turns of the same conversation before, and are
  still refused while it is busy.
- **Page**: Send is never locked by the agent; the background-work dialog and the "waiting for the agent to stop" state are gone; the
  status names the background tasks; a stop that has taken effect offers **Stop them too** / **Leave them running** when tasks remain;
  a stop that has not taken effect within 8 s turns Stop into **Force stop**.
- **Agents are told** (`library/base/background-work`, and the short form every agent loads) that background work carries on and calls
  them back, what a stop does, that the run ends once it has no work (taking scheduled wake-ups with it), and the detached form for work
  that must outlive the run.

Not done: a test against a real `claude` (the experiments in `tools/experiments/persistent-agent/` are the record of the stream's shape),
and other agents: OpenCode's conversation is on the unmerged `feat/opencode` branch, written against `Converser`, and will need this shape when it is merged. Tried by hand by the user (2026-11-16), through sbx: prompts sent mid-turn taken into the turn, Send open and the
receipts throughout (recorded); background work going on through a prompt and calling the agent back; Stop leaving it running
and the offer naming it, both ways (leave, and stop them too); the agent ended when idle and the next prompt resuming the
conversation. Found and fixed on the way: a stopped turn shown as the agent failing, with a ✕ on the prompt it had taken in.
Not tried by hand: the force stop (a real agent stops too quickly to need it), vibe dying with work running (the leftover ended
at the next start), and the sandbox going to sleep once the agent has ended.

## Design (settled where the experiments above say so; the rest to be tried)

**One streaming `claude` per conversation**, started on the first prompt (`--input-format stream-json`), kept for as long as the conversation
lives; every prompt is a `user` message written to its stdin. The conversation's "running" becomes "a turn is in progress" (from a `user`
message to its `result`), not "a process exists". `supersede()` and the kill at a new prompt go; a prompt is simply written, and the agent
queues it behind what it is doing, as the console does.

What changes, and what has to be found out first:

1. **Events with no prompt.** A turn can now start by itself (a call-back). The page already polls events with `since`; it must show such a
   turn as the agent's, with no prompt above it, and set the light and the typing bubble from the stream, not from a prompt having been sent.
   Find out: what the stream says at the start and end of an unprompted turn, and how to tell it from an answer to the last prompt (the
   receipts: sent, delivered, answered, are per prompt).
2. **Stop.** Today Stop kills the tree. With one process, Stop should be the console's **interrupt** of the current turn, leaving the
   process and its background work alone. Find out: whether stream-json input takes an interrupt (a control message), and what the stream
   says after one. A hard kill (today's `stopTurnScript`) stays for a process that is stuck, and for ending the conversation.
3. **Queued prompts.** Today one prompt may wait behind a stop (`queue`, `startQueued`, the "Waiting for the agent to stop…" state).
   Writing to stdin makes that the agent's queue; the page's own queue and its states probably simplify away. Find out: whether the agent
   takes a message mid-turn and when it is answered; whether a message during an unprompted turn interleaves sensibly.
4. **Model and effort.** They are flags of the process (`turnChoice`, read for each turn). Changed mid-conversation they need a new
   process: `--resume` it, which today's code already does. Find out: whether a switch can be done while background work runs (it would end
   it: say so), or whether the stream has a way to switch the model (the console's `/model`).
5. **The review's turns** (start, ask, submit) run the agent with their own instructions and are refused while a stop is under way. Keep
   them on their own short-lived processes, as now, unless trying shows the persistent one can carry them; they must not be given the
   conversation's process, or the review's output is mixed into it.
6. **A vibe that stops and starts again.** The process lives under `sbx exec`, which ends with vibe. On the next start the conversation is
   `--resume`d, as today; background work did not survive vibe stopping, and that is said (on the page, once) rather than found out.
   A process that outlives vibe (detached in the sandbox) is possible, but then vibe must find it again, which the pid file already half
   does: a choice for later.
7. **Cost and idle.** A live process per conversation, idle most of the time. Find out what an idle one costs (memory) and whether the
   agent ends itself after a long quiet; either way the next prompt starts it again.
8. **Other agents** (the OpenCode path) have their own way of being run: check what is claude-only here before changing it.

## Touch points (what a build would change)

`conversationCommand`, `wrapTurn`, `stopTurnScript` and `converse` (`src/vibe/conversation.go`); `Converser`'s shape (a turn is no longer one
call that returns when the process does); `conversation.run`, `finish`, `supersede`, `stop`, `queue`, `startQueued`, `answered`/`running`
(`internal/companion/conversation.go`); `since` and the poll's `running`/`background`; the page's `setConvoRunning`, `drawAgentLight`, the
stop and queue states, and `sendPrompt`'s guard (below); `docs/adr/2026-11-12-stop-feedback.md` (rewritten for the new Stop).

## Built meanwhile, and what happens to it

On `fix/block-unblocked-host`, 2026-11-13, because a new prompt ends background work today:

- **The page asks before a prompt that would end running work** ("End the agent's background work?", Cancel keeps the prompt and the work,
  **Send and end it** goes ahead: the user is never tied up), and the status line about it is in the warning colour.
- **Agents are told** (feature `library/base/background-work`, and a short form in what they always load) that a new prompt ends background
  work, not to end a turn with some pending, and how to start work detached (`setsid nohup`) so that it survives.

Both are replaced now (see "Built"): the dialog is gone, and the instruction says what becomes of background work instead. The detached
form stays, for work that should outlive the conversation's process.

## Tests

A fake streaming agent (a script that reads `user` lines and writes events, with a background timer that emits a notification and a turn by
itself) drives: a call-back with no prompt shown as the agent's; a prompt during background work answered and the work still going; Stop
interrupting a turn and leaving the process; a model change restarting with `--resume`; vibe stopping and the next start resuming. At least
one test against a real `claude` (skipped unless asked for), since the stream's shape is the contract: the experiment above, kept as a test.

## Not decided

Whether the persistent process is also what makes "the agent is still working" visible without the light (an agent that keeps a task going
has a live process; a user could be shown its tasks, the console's `/tasks`). Whether a conversation's process should end after a quiet
spell. Neither blocks the plan.
