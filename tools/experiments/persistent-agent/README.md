# Persistent-agent experiments

Run by hand against a real `claude` (a few cents of the small model each; they are not part of `go test`). Each starts one
`claude -p --input-format stream-json --output-format stream-json` in a neutral folder (`EXP_CWD`, default `/tmp/exp-cwd`), drives it
through stdin, and prints every event with the second it arrived; the raw events go to `EXP_OUT` (default `/tmp/exp-out`) as JSON lines.
`EXP_MODEL` picks the model (default `claude-haiku-5-5`). Results, and what they mean for the design, are in
`docs/plan-persistent-agent.md`. Only the process each script starts is ever ended, by its own handle: never end `claude` by name.

| script | asks |
|---|---|
| `e1b.py` | a background task finishes: does the agent start a turn by itself, and what does the stream say? |
| `e2b.py` | an interrupt during a foreground tool: what ends, what survives (the background task), what the stream says |
| `e3.py` | a prompt sent while a turn runs: queued, folded in, or answered separately? What are the receipts? |
| `e4.py` | the same with `priority: "now"` |
| `e3later.py` | the same with `priority: "later"` |
| `e5.py` | `set_model` mid-conversation |
| `e6.py` | closing stdin with a background task running; resuming the session in a new process |
| `e7.py` | what an idle process costs over three minutes, and whether it ends itself |
| `e8.py`, `e9.py` | changing effort mid-conversation (`apply_flag_settings`, `/effort`) |
| `e10.py` | through `sbx exec -i`: start-up time, the time from writing a prompt to its `queued` frame, bursts between events, a 150 KB prompt arriving whole |
| `e11.py` | (sbx only) vibe dies while the agent runs: the sbx client is killed; does the agent in the sandbox end or carry on, and does vibe's hard stop still find it? |
| `e12.py` | (sbx only) vibe's own hard stop (`stopTurnScript`) on an agent with a background task: the whole tree, the background command included? |

## Through sbx, as vibe runs the agent

`run-through-sbx.sh <sandbox name> [folder inside it]` runs `e10 e1b e2b e3 e5 e11 e12` with `EXP_SBX` set, which makes `drive.py` start the
agent exactly as `conversationCommand` and `wrapTurn` do: `sbx exec -i <sandbox> -- sh -c <login-shell script> converse <pid-file wrapper>`.
Run it on the host (sbx is not in the sandbox). Results go to `tmp/persistent-agent-sbx/` (`run.log`, and the raw events), which is in the repo and
ignored by git. A dry run against a fake `sbx` that strips `exec -i NAME --` checks the scripts but not sbx's own stdin and stdout.
