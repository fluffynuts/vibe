# Plan: backstops for every wait in vibe

Status: all built 2026-10-13 (uncommitted then); see "Built" at the end for how each row turned out.

## The rule

A backstop is not the mechanism; it catches what the mechanism missed. Every wait that could hang because
sbx (young, with known issues) or vibe (young, with unseen corners) misbehaves gets one, so a hang becomes
a clear failure instead of a spinner that never stops.

- **Generous:** many times the normal duration. It fires only when something is really wrong.
- **Never the mechanism:** the real signal (a "ready", a process exiting, the page cancelling) decides the
  normal outcome. The timeout only catches a signal that never comes.
- **Says what happened in the user's terms:** what was waited for, for how long, and what to check next
  (e.g. "`sbx ls` shows the sandbox's state").

## Already done (2026-10-13, uncommitted then)

1. **Stopped under the session:** no timing any more. `vibe --stop` tells the attached vibe through a
   `.hold` file beside the folder lock (`running.AskToHold`, taken back with `CancelHold` when the
   sandbox is still running). A stop through sbx itself counts only when the sandbox was reached during
   the session (reported by `nudgeOnStart`) and sbx says it's stopped.
2. **Optimise:** a 10-minute backstop (it was a 2-minute limit), with a message pointing at `sbx ls` and
   Stop and Start on the Admin tab.
3. **`start-clipboard`:** the server says "ready" on a pipe (`VIBE_CLIPBOARD_READY_FD`). A crash is
   reported at once (the pipe closes); a 30-second backstop catches one that never listens. The
   companion's clipboard notice shows the latest run's error from `/tmp/clipboard.log`.

## To decide: gaps where a stuck sbx leaves the user waiting forever

| Where | Now | Proposed backstop and message |
|---|---|---|
| Admin actions (Start, Stop, rebuild, Save memories) | `sbx start/stop/rm/create` have no limit; the page shows "working…" forever | 15 min for rebuilds, 3 min for start, stop and memories: "sbx didn't finish `<cmd>` within N min; `sbx ls` shows the sandbox's state" |
| Admin tab's status (`sbx ls` plus usage `exec`s) | no limit; the status hangs and the page's 15 s call fails silently | 20 s, then "sbx didn't answer — status unknown" in the Sandbox card |
| Pushing instructions to sandboxes | 30 s per sandbox, but the `sbx ls` first has no limit | `sbx ls` 20 s; report which sandboxes weren't reached |
| Conversation turns | no limit (long turns are legitimate) | 60 min, as a "something's wrong" signal: "the agent has been working for an hour with no reply — Stop it, or check `sbx ls`" |
| `sbx create` from the CLI | no limit, but its output is visible and Ctrl-C works | leave as is |

Most sbx calls in `internal/sbxrun` take no context (`Exists`, `List`, `Start`, `StopQuiet`, `Remove`,
`Create`, `ExecInput`, the secrets calls). Building the rows above means giving the ones the page uses a
`context` variant, as `ExecStdout` and `ExecInputStdout` already have.

## Fine as they are

The network rules calls (10 s), git status for the header (5 s), the clock check (10 s), the GitHub token
check (15 s), the transcript copy (30 s each way), stopping another vibe (60 s, then kill, and it says
so), applying instructions to one sandbox (30 s), the release download (10 min).

## Polling where a real signal exists (optional, later)

- `sbxrun.WaitReachable` probes with `sbx exec` every second, with three different limits (60 s in
  `Start`, 120 s in memory backup, 180 s in `nudgeOnStart`). sbx offers no ready event that we know of:
  unify the limits and their messages.
- Waiting for another vibe to exit (`running.waitGone`) polls its lock file every 200 ms; it could block
  on taking the lock instead.

## Built (2026-10-13)

The user approved every row, preferring **warn, don't stop** wherever stopping could do harm.

- **Admin actions:** a warning, not a kill. Past 3 minutes (start, stop, memories) or 15 minutes (the
  rebuilds), the action's status says "taking too long" and its log gets "sbx may be stuck … 'sbx ls'
  shows the sandbox's state". vibe keeps waiting, because stopping sbx half-way through a rebuild could
  leave a broken sandbox. (`adminSlow` in `internal/companion/admin.go`.)
- **Admin status:** a real 20-second deadline (`statusWithin`). `gatherInfo` takes a context, `sbx ls`
  has `ListContext`, and the usage reads use `ExecCaptureContext`; past it, the Sandbox card says sbx
  didn't answer and the state is unknown.
- **Instructions push:** the same 20-second deadline on `sbx ls`. When sbx can't say what's running,
  the save stands and each sandbox is reported as not reached, with why. It used to be reported, wrongly,
  as "not running". The list is injectable for tests (`companionAdmin.listSandboxes`).
- **Conversation turns:** silence, not duration. While the agent is answering, 60 minutes with no new
  output (`quietWarning`) shows "No output from the agent for N minutes — it may be stuck" in the pane's
  status line. The turn isn't stopped. Idle time between turns doesn't count, and nor does work left
  running after an answer.

## Stopping what was left running (2026-10-13)

Everything that warns instead of stopping can be stopped by the user, from the page and from a terminal:

- **Page:** the agent's turn has **Stop** (and Escape); an Admin action has **Cancel** on its Last
  action card, which asks first for a rebuild. A cancelled action's card says what state sbx reports
  the sandbox in, and what to do (Start, or Rebuild).
- **Terminal:** `vibe --cancel [path]` asks the vibe running for that folder (a `.cancel` request beside
  its folder lock, answered in `.cancelled`) to cancel both, and prints what was ended. Its 10-second
  answer wait is itself a backstop.
- **Cancelling is real:** the actions' sbx commands (`start`, `stop`, `rm`, `create`) take a context
  and are killed (`sbxrun.*Context`); the short steps between them check it. A cancelled memory backup
  puts the store back as it was, as a quit does.
