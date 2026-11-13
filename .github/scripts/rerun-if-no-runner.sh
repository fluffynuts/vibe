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

if [ "$attempt" -ge "$max" ]; then
  echo "attempt $attempt of at most $max: not trying again"
  exit 0
fi

jobs=$(gh api "repos/$GH_REPO/actions/runs/$run/attempts/$attempt/jobs?per_page=100")

# The ids of the jobs to re-run: those of the run that never got a runner, if every job that did not succeed was one of them.
ids=$(jq -r --argjson min "$min" '
  def unsuccessful: .conclusion == "failure" or .conclusion == "cancelled" or .conclusion == "timed_out";
  def never_ran: (.runner_id // 0) == 0 and (.steps | length) == 0;
  def queued_for: (.completed_at | fromdateiso8601) - (.created_at | fromdateiso8601);
  [.jobs[] | select(unsuccessful)] as $bad
  | [$bad[] | select(never_ran and queued_for >= $min)] as $starved
  | if ($starved | length) > 0 and ($starved | length) == ($bad | length) then $starved[].id else empty end
' <<<"$jobs")

if [ -z "$ids" ]; then
  echo "run $run, attempt $attempt: no job of it was left without a runner, or a job that ran did not succeed: nothing to re-run"
  exit 0
fi

for id in $ids; do
  echo "run $run, attempt $attempt: job $id never got a runner: re-running it"
  gh run rerun --job "$id"
done
