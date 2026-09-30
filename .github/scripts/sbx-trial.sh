#!/usr/bin/env bash
# sbx-trial.sh — find out what a real sbx does on this machine, for the
# things vibe currently has to guess about. Every check is recorded (command,
# output, exit code) and none of them stops the run: the point is to learn
# as much as one run can. Run by .github/workflows/sbx-trial.yml, but it
# works anywhere sbx is installed.
#
#   ./sbx-trial.sh [report.md]      (default: sbx-trial-report.md)
#
# It creates two throwaway sandboxes, vibe-trial-shell and vibe-trial-claude,
# and removes them at the end.

set -u

REPORT="${1:-sbx-trial-report.md}"
WORK="$(mktemp -d)"
SHELL_SB=vibe-trial-shell
CLAUDE_SB=vibe-trial-claude
STEP_TIMEOUT="${STEP_TIMEOUT:-600}"

: >"$REPORT"
say() { printf '%s\n' "$*" >>"$REPORT"; }

# check TITLE -- COMMAND...: run COMMAND with a time limit and record it.
check() {
  local title="$1"
  shift 2
  local out code
  echo "::group::$title"
  out="$(timeout "$STEP_TIMEOUT" "$@" </dev/null 2>&1)"
  code=$?
  printf '%s\n' "$out"
  echo "::endgroup::"
  [[ $code -eq 124 ]] && out="$out"$'\n'"(timed out after $STEP_TIMEOUT s)"
  say "### $title"
  say ""
  say "\`$*\` -> exit $code"
  say ""
  say '```'
  say "$(printf '%s' "$out" | tail -n 60)"
  say '```'
  say ""
  return $code
}

# check_sh TITLE -- SCRIPT: as check, for a pipeline or redirection.
check_sh() {
  local title="$1"
  check "$title" -- bash -c "$3"
}

say "# sbx trial on $(uname -srm)"
say ""
say "Run at $(date -u +%Y-%m-%dT%H:%M:%SZ)."
say ""

say "## The machine"
check "KVM device" -- ls -l /dev/kvm
check "sbx version" -- sbx version
check "sbx diagnose" -- sbx diagnose
check "sbx ls" -- sbx ls

say "## A shell sandbox: exec and ports"
mkdir -p "$WORK/ws"
echo "trial workspace" >"$WORK/ws/README"
check "create a shell sandbox" -- sbx create --name "$SHELL_SB" shell "$WORK/ws"
check "ls --quiet (what vibe's Exists parses)" -- sbx ls --quiet

# vibe's Reachable: an exec of /bin/true.
check "exec /bin/true (vibe's reachability check)" -- sbx exec "$SHELL_SB" -- /bin/true
check "exec id (who an exec runs as)" -- sbx exec "$SHELL_SB" -- id

# If sbx joins its arguments into one command line, "a b" arrives split:
# <a> <b> <c> rather than <a b> <c>. The memory code is written around this.
check "exec argument joining: printf '<%s>' 'a b' c" -- sbx exec "$SHELL_SB" -- printf '<%s>\n' 'a b' c

# vibe's WriteFile pipes content in through exec -i.
check_sh "exec -i passes stdin through" -- "echo hello-from-stdin | sbx exec -i $SHELL_SB -- cat"
check_sh "exec -i writes a file the way WriteFile does" -- \
  "printf 'http://localhost:15391\n' | sbx exec -i $SHELL_SB -- cp /dev/stdin /tmp/trial-url && sbx exec $SHELL_SB -- cat /tmp/trial-url"

# vibe's on-start nudge uses exec -d, which the docs say isn't supported.
check "exec -d (detached)" -- sbx exec -d "$SHELL_SB" -- touch /tmp/trial-detached
sleep 3
check "did the detached exec run?" -- sbx exec "$SHELL_SB" -- ls -l /tmp/trial-detached

# The diffity URL code reads sbx ports --json, whose shape isn't documented.
check "publish 15391:5391" -- sbx ports "$SHELL_SB" --publish 15391:5391
check "publish 5392 to an ephemeral host port" -- sbx ports "$SHELL_SB" --publish 5392
check "ports (text)" -- sbx ports "$SHELL_SB"
check "ports --json" -- sbx ports "$SHELL_SB" --json

say "## A claude sandbox with a kit: who writes settings.json first"
# The kit ships a marker settings.json, deployed only if missing, the way
# vibe's defaults ship theirs; an install step records what ~/.claude held
# when it ran. Afterwards, the marker in settings.json means vibe's copy
# landed; sbx's keys without it mean sbx wrote the file first.
mkdir -p "$WORK/kit" "$WORK/ws2"
cat >"$WORK/kit/spec.yaml" <<'YAML'
schemaVersion: "2"
kind: mixin
name: vibe-trial
displayName: vibe sbx trial
description: Records the order sbx sets a sandbox up in
requires:
  agent: claude
setup:
  install:
    - command: |
        #!/bin/sh
        {
          echo "install step ran at $(date -Is) as $(id -un)"
          ls -la --time-style=full-iso /home/agent/.claude/ 2>&1
          echo "--- settings.json at install time:"
          cat /home/agent/.claude/settings.json 2>&1
        } >/tmp/trial-install-saw.txt
      user: "1000"
      description: record what ~/.claude holds while install runs
  files:
    - path: /home/agent/.claude/settings.json
      mode: "0644"
      onlyIfMissing: true
      description: trial marker settings.json
      content: |
        {"vibeTrialMarker": true}
YAML
check "create a claude sandbox with the kit" -- sbx create --name "$CLAUDE_SB" --kit "$WORK/kit" claude "$WORK/ws2"
check "start it with an exec (runs setup on first boot)" -- sbx exec "$CLAUDE_SB" -- /bin/true
sleep 10
check "what install saw" -- sbx exec "$CLAUDE_SB" -- cat /tmp/trial-install-saw.txt
check "settings.json afterwards" -- sbx exec "$CLAUDE_SB" -- cat /home/agent/.claude/settings.json
check "~/.claude afterwards" -- sbx exec "$CLAUDE_SB" -- ls -la --time-style=full-iso /home/agent/.claude/
check "~/.claude.json has a theme?" -- sbx exec "$CLAUDE_SB" -- grep -o '"theme"[^,]*' /home/agent/.claude.json

say "## Clean up"
check "remove the trial sandboxes" -- sbx rm --force "$SHELL_SB" "$CLAUDE_SB"
rm -rf "$WORK"

echo "report written to $REPORT"
exit 0
