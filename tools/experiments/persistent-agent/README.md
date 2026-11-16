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
| `e5.py` | `set_model` mid-conversation |
| `e6.py` | closing stdin with a background task running; resuming the session in a new process |
| `e7.py` | what an idle process costs over three minutes, and whether it ends itself |
| `e8.py`, `e9.py` | changing effort mid-conversation (`apply_flag_settings`, `/effort`) |
