#!/bin/bash
# Runs the persistent-agent experiments the way vibe runs the agent: through `sbx exec -i` into a sandbox, with its login-shell script
# and pid-file wrapper. On the host, where sbx is. Needs python3 and a running sandbox. The raw events and the printed log go to
# EXP_OUT (default: tmp/persistent-agent-sbx in the repo, which the sandbox's agent can read).
#   tools/experiments/persistent-agent/run-through-sbx.sh <sandbox name> [folder inside it, default /tmp]
set -u
SANDBOX="${1:?usage: run-through-sbx.sh <sandbox name> [folder inside the sandbox]}"
HERE="$(cd "$(dirname "$0")" && pwd)"
export EXP_SBX="$SANDBOX" EXP_WORKSPACE="${2:-/tmp}"
export EXP_OUT="${EXP_OUT:-$HERE/../../../tmp/persistent-agent-sbx}"
mkdir -p "$EXP_OUT"
LOG="$EXP_OUT/run.log"
: > "$LOG"
echo "sandbox $SANDBOX, folder $EXP_WORKSPACE, results in $EXP_OUT" | tee -a "$LOG"
sbx exec "$SANDBOX" -- sh -c 'echo "sandbox reachable as $(id -un); claude is $(command -v claude || echo MISSING)"' 2>&1 | tee -a "$LOG"
for s in e10 e1b e2b e3 e5 e11 e12; do
  echo "=== $s $(date +%T)" | tee -a "$LOG"
  # --foreground: a command `timeout` starts in the background is stopped the moment it reads this terminal. -u and --line-buffered: output as it happens.
  timeout --foreground 300 python3 -u "$HERE/$s.py" < /dev/null 2>&1 | grep --line-buffered -v "thinking_tokens\|rate_limit\|commands_changed" | tee -a "$LOG"
done
echo "=== done $(date +%T)" | tee -a "$LOG"
