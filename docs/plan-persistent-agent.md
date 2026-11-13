# One agent process for the whole conversation, so background work can call back (planned, not built)

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

## Design (to be settled by trying; not decided)

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

When this plan is built the dialog and most of the instruction go: a prompt no longer ends anything. The detached form stays useful for
work that should outlive the conversation's process.

## Tests

A fake streaming agent (a script that reads `user` lines and writes events, with a background timer that emits a notification and a turn by
itself) drives: a call-back with no prompt shown as the agent's; a prompt during background work answered and the work still going; Stop
interrupting a turn and leaving the process; a model change restarting with `--resume`; vibe stopping and the next start resuming. At least
one test against a real `claude` (skipped unless asked for), since the stream's shape is the contract: the experiment above, kept as a test.

## Not decided

Whether the persistent process is also what makes "the agent is still working" visible without the light (an agent that keeps a task going
has a live process; a user could be shown its tasks, the console's `/tasks`). Whether a conversation's process should end after a quiet
spell. Neither blocks the plan.
