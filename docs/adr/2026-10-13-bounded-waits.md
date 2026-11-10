# Every wait has a backstop, and what is left running can be stopped

- **Status:** accepted, built 2026-10-13; the sandbox wait was unified 2026-11-10 (see below).
- **Decided:** 2026-10-13, with the user, who approved every row and preferred **warn, don't stop** wherever stopping could do harm.
- **Came from:** `docs/plan-backstops.md`, now deleted: `git show 7700047:docs/plan-backstops.md`.

## Context

vibe waits on sbx, which is young and has known issues, and on its own younger corners. A wait that could hang gave the user a spinner
that never stopped, with nothing to say what was waited for or what to check.

## Decision

**The rule.** A backstop is not the mechanism; it catches what the mechanism missed. It is **generous** (many times the normal duration:
it fires only when something is really wrong), it is **never the mechanism** (the real signal, such as a "ready", a process exiting or the
page cancelling, decides the normal outcome), and it **says what happened in the user's terms**: what was waited for, for how long, and what
to check next (for example, that `sbx ls` shows the sandbox's state).

**What it came to, where a stuck sbx used to leave the user waiting for good:**

| Wait | Backstop | What it does |
|---|---|---|
| Admin actions (Start, Stop, rebuilds, Save memories) | 3 minutes (start, stop, memories); 15 (rebuilds) | **Warns** ("taking too long", and `sbx may be stuck … 'sbx ls' shows …" in the action's log) and keeps waiting: stopping sbx half-way through a rebuild could leave a broken sandbox |
| The Admin tab's status | 20 s deadline | The Sandbox card says sbx didn't answer and the state is unknown |
| Pushing instructions to sandboxes | 20 s on the `sbx ls` | The save stands; each sandbox is reported as not reached, with why (it used to be reported, wrongly, as "not running") |
| A conversation turn | 60 minutes **without output**, not of duration | "No output from the agent for N minutes: it may be stuck" in the pane's status line; the turn is not stopped; idle time between turns, and work left running after an answer, don't count |
| Optimise | 10 minutes | A message pointing at `sbx ls` and Stop and Start |
| `start-clipboard` | the server says "ready" on a pipe (`VIBE_CLIPBOARD_READY_FD`); a crash is seen at once; 30 s catches one that never listens | The clipboard notice shows the latest run's error |
| Stopped under a session | no timing at all: `vibe --stop` asks the attached vibe through a `.hold` file beside the folder lock | A stop through sbx counts only when the sandbox was reached during the session and sbx says it's stopped |

The calls these need take a context (`sbxrun.ListContext`, `ExecCaptureContext`, the `*Context` variants of start, stop, rm and create),
so they can be ended.

**Everything that warns can be stopped by the user.** The page has **Stop** (and Escape) on a turn and **Cancel** on an Admin action's
Last action card (which asks first for a rebuild, and says afterwards what state sbx reports and what to do). In a terminal,
`vibe --cancel [path]` asks the vibe running for that folder (a `.cancel` request beside its folder lock, answered in `.cancelled`) to do
both, with a 10-second answer wait that is itself a backstop. Cancelling is real: the sbx commands are killed, the short steps between
them check the context, and a cancelled memory backup puts the store back as it was.

**Fine as they were:** the network-rules call (10 s), git status for the header (5 s), the clock check (10 s), the GitHub token check
(15 s), the transcript copy (30 s each way), stopping another vibe (60 s, then kill, and it says so), applying instructions to one sandbox
(30 s), the release download (10 min).

**One wait for a sandbox to take a command (2026-11-10).** There were two near-copies: `WaitReachable`, which counted probes with limits
spelled `120` and `180`, and the loop at the end of `Start`, which watched the clock. They are now one (`waitUntilReachable`), bounded by a
**duration the caller passes in and names for what it waits for**: a memory restore waits 120 s (`reachableToRestore`), the start nudge
180 s (`reachableToNudge`), and `Start` 60 s (`startTimeout`). The times differ on purpose: a restore is of the user's data into a sandbox
just made, a start nudge has a session waiting, and `Start` is only reached when the user has already committed to the operation. A slow
probe no longer stretches a wait, a try is begun until the time is up and never after, and none is begun once the context has ended.

## Consequences

- A hang is a message with a next step, and the user can end it.
- Warn-not-stop means a stuck rebuild keeps occupying the page's one job at a time until it is cancelled.
- **`running.waitGone` stays a poll** (every 200 ms). The plan thought it could block on taking the lock, but the same tick reads the step file,
  which reports progress and pushes the deadline back on each new step; a blocking lock would still need a ticker for that.

## Not built, and why

- Nothing from the plan is outstanding. Its two optional items were dealt with as above: the limits are now one mechanism with named
  times (not one value), and the lock wait was looked at and left.
