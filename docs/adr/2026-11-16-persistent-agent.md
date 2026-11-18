# One agent process for the conversation, so background work survives a prompt and can call back

- **Status:** accepted, built 2026-11-16 on `feat/persistent-agent`, merged (PR #28). Tried by hand by the user through sbx the same day,
  and over the following day.
- **Decided:** 2026-11-16, with the user, after experiments with `claude` 2.1.323 outside vibe and through sbx.
- **Came from:** `docs/plan-persistent-agent.md`, now deleted: `git show 7b56280:docs/plan-persistent-agent.md`. It holds the experiments
  (what the stream-json protocol sends, step by step) in full; their scripts are in `tools/experiments/persistent-agent/`.

## Context

Each prompt was its own `claude -p` run, `--resume`d from the last. After answering, a run could live on with background work, and a prompt
sent then superseded it: the whole tree under it was stopped, background work included. The user lost an agent's background work twice that
way, and the guard built meanwhile (a dialog asking first) tied them up when they could have been planning with the agent.

The experiments showed the console's behaviour is there headless: one long-lived
`claude -p --input-format stream-json --output-format stream-json` process, prompts written to its stdin, and **unprompted turns** on the same
stream when background work finishes (`task_notification`, then a turn the agent starts itself). Receipts come from `command_lifecycle`
frames keyed by each prompt's `uuid`, not from results: one `result` can answer several prompts folded into one turn. Model and effort
change without a restart (`set_model`, `apply_flag_settings`).

## Decision

The point is that **background work is not lost when the user prompts while it runs**, and that it can call the agent back. Not keeping the
agent quick to answer: the process lives while there is work that can call back, and no longer.

1. **A prompt sent while the agent works is folded into the running turn**, as the console does. The page shows it as taken in, from
   `command_lifecycle`.
2. **Stop interrupts the turn at once and leaves background work running** (`interrupt` with `spare_background_tasks: true`). If tasks remain,
   the page says so after the stop and offers **Stop them too** (a `stop_task` for each) or **Leave them running**. A stop not taken within
   8 s turns Stop into **Force stop**: the hard stop, which ends everything.
3. **The agent ends 30 s after it is idle** (no turn, no background task): vibe closes its stdin and it exits (killed if it hasn't 30 s later).
   An open `sbx exec -i` keeps a sandbox awake, so an idle agent must not hold one; while it has work, vibe keeps the sandbox awake.
4. **The next prompt after that starts a new process** that `--resume`s the conversation. A wake-up the agent scheduled with nothing running
   dies with the idle agent.
5. **One pid file per conversation** (`/tmp/vibe-conversation.pid`), not per turn: a vibe started after one that died finds the agent it left
   and ends it, by the same hard stop, before starting its own.

Built as: `sbxrun.ExecSession` (a command in the sandbox whose input stays open); `src/vibe/conversation.go` (`startAgent`, `claudeAgent`:
prompts as `user` lines with a uuid, after `set_model` and the effort for the page's choice; interrupt, `stop_task`, end, kill);
`internal/companion/conversation.go` (`Agent` and `StartAgent` replace `Converser`; a prompt is always accepted; receipts and background
tasks in the poll; a `system/init` with no prompt in flight is a call-back; the review's turns are prompts to the same agent, still refused
while it is busy). On the page, Send is never locked by the agent, the background-work dialog is gone, and the status names the background
tasks. Agents are told (`library/base/background-work`, and the short form every agent loads) that background work carries on and calls
them back, what a stop does, that the run ends once it has no work, and the detached form for work that must outlive the run.

## Consequences

- The first prompt after an idle spell pays a start (about 1.6 s through sbx), as every prompt used to.
- The sandbox still sleeps when nothing is happening: tried by the user over a day and overnight.
- Work the agent detaches itself (`setsid nohup`) is outside all of this, as before: it survives a stop and an idle end.

## Not built

Other agents: OpenCode's conversation, on the unmerged `feat/opencode` branch, is written against `Converser` and will need this shape when
it is merged (parked until a tester signs up: `docs/plan-conversation-other-agents.md`).

Not decided: whether the live process should also show the agent's tasks on demand (the console's `/tasks`), and whether it should end after
a quiet spell even with a task running.

## Not verified

No test runs a real `claude`: the experiments are the record of the stream's shape, and a CLI that changes it would be caught by hand.
Tried by hand by the user: prompts mid-turn taken in, background work surviving a prompt and calling back, Stop leaving tasks running and
the offer both ways, the idle end and the resumed conversation, vibe killed with SIGKILL while work ran (the next vibe ended the leftover).
Not tried by hand: the page's own Force stop, as a real agent stops well within 8 s (it has unit and page tests).
