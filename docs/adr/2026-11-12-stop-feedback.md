# Stopping the agent says so at once, and a prompt sent meanwhile waits for the stop

- **Status:** accepted, built 2026-11-12 on `fix/stop-feedback`, merged as PR #22 (`bccd23b`); the commits are `a06f88a` and `fd0a976`. The same PR also carried
  the Network log's background read (`703995c`, in `docs/adr/2026-11-12-network-block-and-filter.md`) and the page-wide "a poll that brings nothing new changes
  nothing" rule (`6cad1fe`, `8d02d51`, same ADR).
- **Decided:** 2026-11-12, with the user, after a first attempt was thrown away (below).
- **Came from:** no plan file: the problem was reported as "I pressed Escape, then the Stop button, and nothing visibly happened", and the design was settled
  in the conversation that followed. The discarded attempt is `e643be8` (never pushed; reachable only through the reflog).

## Context

Stopping a normal (not review-writing) turn only cancelled the turn's context. The turn stayed `running` until the agent had been stopped **inside the
sandbox**: the `sbx exec` carrying the turn is killed (up to 1 second of `WaitDelay`), a second `sbx exec` reaches into the sandbox, and its script sends TERM and
waits up to 5 seconds before KILL. Throughout, the page showed nothing (`stopAgent()` shows nothing on success), Stop stayed visible, and a new prompt was refused
with a 409.

**The first diagnosis was wrong.** The report had no dialog in it, but the first attempt assumed one: it made a second Escape *confirm* the "Stop the agent?"
dialog (shown only while the agent writes a review). The user tried it and pressed Escape meaning "dismiss". That behaviour, Escape opens the dialog, the
browser's own Escape closes it as a cancel, only the button stops the agent, was already right and is **unchanged**: Escape never confirms anything.

## Decision

### 1. The page is told a stop is under way

The conversation has a `stopping` flag, set by `stop()` and cleared when the turn ends, and the poll carries it as `stopState`: `"stopping"`, `"waiting"` (below), or
`""`. The page says **"Stopping the agent…"** in the conversation's status line at once, disables Stop, and Escape no longer fires again. It leaves Send open.

### 2. One prompt may wait for the stop; it never runs beside the old turn

A prompt sent while the agent is being stopped is taken (`queue`), shown in the conversation at once with a `…` tick ("Waiting for the agent to stop"), and given to
the agent only when the old turn has ended (`finish` hands it to `startQueued`, which the server sets in `New`). The status line then says **"Waiting for the agent
to stop…"**: the label is about what the user is now waiting for, not the stop they asked for.

**Why not just let it through:** two agents on one conversation's session is the bug `wrapTurn` exists to prevent, and the conversation has one `running`, `cancel`
and `ended`, with no per-turn identity: the old turn's late `finish()` would set `running = false`, clear `cancel` and close `ended` while the new turn is live,
leaving it unstoppable and looking finished. Queueing keeps the order and nothing concurrent.

- Only **one** prompt waits: the UI keeps Send disabled once one does, and the server answers a second with a 409 as a backstop. A prompt during a turn that is not
  being stopped is still refused as before.
- The queued prompt is moved **below the "Stopped." marker** when it is given to the agent (`moveBelowEnd`: the early copy is retracted and the prompt added again),
  so "Stopped." does not read as the end of the prompt. User messages therefore carry `data-seq`, which the page's existing `retract` handling needs.
- Review actions (start, ask, submit) are **not** queued: they are refused while a stop is under way. Only plain prompts wait.
- A queued prompt kept in the saved conversation when vibe stops is shown as "Not given to the agent" on the next start.

### 3. Two gaps in stopping itself, closed

- **A stop that beats the turn's shell.** If the stop reached the sandbox before the turn's shell had written its pid, there was nothing to kill and the agent
  started anyway, unseen. `stopTurnScript` now leaves `<pidfile>.stop` **before** it looks for the pid, and `wrapTurn` looks for that file right **after** it writes
  the pid: so one of them always sees the other, and the agent is never started.
- **A stop that could not be confirmed.** When the `sbx exec` that stops the agent fails or times out, that was a warning on the terminal only, while the page said
  "Stopped". `converse` now returns `companion.ErrStopUnconfirmed`, and the conversation shows an error, saying the agent may still be running, before the stop
  marker.

## Consequences

- The wait after Stop is the same length; the user is told, and need not wait to type. Sending during it works.
- The review's "Stop the agent?" dialog and Escape in the prompt box behave as before.

## Not built, on purpose

- Processes the agent daemonises **out of its process tree** survive a stop: the script kills the tree found with `pgrep -P`.
- The `.stop` marker is left in the sandbox's `/tmp` if the turn's shell never runs at all (tiny).
- Queueing review actions.
- An end-to-end check, from inside the sandbox, that the agent is gone: the host-side stop script is trusted.

## Not verified

- **The real length of the delay** was never measured: it is inferred from the code and the 5-second budget (there was no `sbx` in the sandbox the work was done in).
- One **transient error** on the first run after restarting from an older vibe to this one was not reproduced and is unexplained.
- Tried by hand by the user in the real page: Stopping shown and the turn ending quickly; a prompt sent meanwhile queued, then ran, below "Stopped."; mashing Escape broke
  nothing; the sandbox's process listing was the same before and after a quick interruption. That last one does **not** prove the early-stop race closed (it is
  narrow); `TestATurnStoppedBeforeItsShellRecordedItselfNeverStartsTheAgent` does, and fails without the change.
