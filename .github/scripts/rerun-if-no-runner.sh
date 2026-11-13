#!/usr/bin/env bash
# Re-runs the jobs of a finished CI run that never got a runner.
#
# GitHub's hosted runners (macOS's most often) sometimes do not pick a job up: it waits, with no runner and no steps, until it is failed or
# cancelled, and a person re-runs it by hand until one comes. A job that ran and failed has a runner and steps, and is a result: it is left
# alone, and so is a run in which one did, so that a re-run never buries a real failure. A job cancelled soon after it was queued is
# someone's decision, and is left alone too: only one that waited MIN_QUEUED_SECONDS (600) counts as having been starved.
#
# Usage: rerun-if-no-runner.sh <run id> <attempt>
# Needs gh and jq, with GH_REPO and GH_TOKEN set. MAX_ATTEMPTS (10) stops it going on for ever: each retry waits at least MIN_QUEUED_SECONDS for a runner first, so nine
# of them are spaced out by the pool's own slowness, not made in a burst.
set -euo pipefail

run=${1:?usage: rerun-if-no-runner.sh <run id> <attempt>}
attempt=${2:?usage: rerun-if-no-runner.sh <run id> <attempt>}
max=${MAX_ATTEMPTS:-10}
min=${MIN_QUEUED_SECONDS:-600}

# Said in the log, and on the run's summary page (GITHUB_STEP_SUMMARY, when GitHub gives one), where it is seen without opening a log.
say() {
  echo "$1"
  if [ -n "${GITHUB_STEP_SUMMARY:-}" ]; then echo "$1" >> "$GITHUB_STEP_SUMMARY"; fi
}

if [ "$attempt" -ge "$max" ]; then
  say "Run $run, attempt $attempt of $max: not trying again."
  exit 0
fi

jobs=$(gh api "repos/$GH_REPO/actions/runs/$run/attempts/$attempt/jobs?per_page=100")

# The jobs to re-run, one to a line (id, name, minutes it waited): those of the run that never got a runner, if every job that did not
# succeed was one of them.
starved=$(jq -r --argjson min "$min" '
  def unsuccessful: .conclusion == "failure" or .conclusion == "cancelled" or .conclusion == "timed_out";
  def never_ran: (.runner_id // 0) == 0 and (.steps | length) == 0;
  def queued_for: (.completed_at | fromdateiso8601) - (.created_at | fromdateiso8601);
  [.jobs[] | select(unsuccessful)] as $bad
  | [$bad[] | select(never_ran and queued_for >= $min)] as $starved
  | if ($starved | length) > 0 and ($starved | length) == ($bad | length)
    then $starved[] | [.id, .name, (queued_for / 60 | floor)] | @tsv
    else empty end
' <<<"$jobs")

if [ -z "$starved" ]; then
  say "Run $run, attempt $attempt of $max: nothing to re-run (no job was left without a runner, or a job that ran did not succeed)."
  exit 0
fi

while IFS=$'\t' read -r id name minutes; do
  say "Run $run, attempt $attempt of $max: re-ran \"$name\" (job $id), which waited $minutes minutes and never got a runner."
  gh run rerun --job "$id"
done <<<"$starved"
