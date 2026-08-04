#!/usr/bin/env bash
#
# Compare Kiwi's two execution loops on the same task set.
#
# Changing the default loop is gated on this comparison: session vs file_loop
# over a fixed set of tasks, measuring PR-opened rate, cost, and wall clock.
# Human-accept rate is deliberately NOT measured here —
# it cannot be, by a script. The PRs are the artifact; someone has to read them.
#
# Runs are strictly SEQUENTIAL. A Free-tier org has MaxConcurrentJobs=1
# (pkg/auth/limits.go), so submitting in parallel just queues behind itself and
# makes the wall-clock column meaningless.
#
#   KIWI_API_KEY=... ./evals/run.sh                    # both loops, all tasks
#   KIWI_API_KEY=... ./evals/run.sh -m session         # one loop
#   KIWI_API_KEY=... ./evals/run.sh -t t05-cross-package
#
# Results land in evals/results/<timestamp>.tsv. Cost is not in the API; see the
# README for the query that joins it back on.
set -euo pipefail

API="${KIWI_API_URL:-https://api.runkiwi.dev}"
KEY="${KIWI_API_KEY:-}"
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
TASKS="$HERE/tasks.json"
MODES="file_loop session"
ONLY=""
WORKER="${KIWI_EVAL_WORKER_MODEL:-claude-haiku-4-5-20251001}"
ARCHITECT="${KIWI_EVAL_ARCHITECT_MODEL:-claude-sonnet-5}"
# A Free-tier task is killed at 600s (TaskTimeoutSeconds), so waiting much past
# that means the harness is wedged, not the task.
DEADLINE="${KIWI_EVAL_DEADLINE:-780}"

while getopts "m:t:f:" opt; do
  case "$opt" in
    m) MODES="$OPTARG" ;;
    t) ONLY="$OPTARG" ;;
    f) TASKS="$OPTARG" ;;
    *) exit 2 ;;
  esac
done

[ -n "$KEY" ] || { echo "set KIWI_API_KEY" >&2; exit 2; }
command -v jq >/dev/null || { echo "jq is required" >&2; exit 2; }

REPO=$(jq -r .repo "$TASKS")
REF=$(jq -r .ref "$TASKS")
TEST_CMD=$(jq -r .test_cmd "$TASKS")

mkdir -p "$HERE/results"
OUT="$HERE/results/$(date -u +%Y%m%dT%H%M%SZ).tsv"
printf 'task_id\tmode\tjob_id\tstatus\twall_s\tpr_url\tdetail\n' > "$OUT"
echo "writing $OUT"

submit() { # task_id mode task file -> job_id
  local body
  body=$(jq -n \
    --arg task "$3" --arg repo "$REPO" --arg ref "$REF" \
    --arg file "$4" --arg test "$TEST_CMD" --arg model "$WORKER" \
    --arg mode "$2" --arg arch "$ARCHITECT" \
    '{task:$task, repo_url:$repo, ref:$ref, test_cmd:$test, model:$model, max_workers:1}
     + (if $file != "" then {file:$file} else {} end)
     + (if $mode == "session" then {mode:"session", architect_model:$arch} else {} end)')

  curl -sS -X POST "$API/api/v1/planner/plan" \
    -H "Authorization: Bearer $KEY" -H "Content-Type: application/json" \
    -d "$body" | jq -r '.job_id // empty'
}

# wait_job prints "<status>\t<pr_url>\t<detail>". A job is done when every task
# in it is terminal.
wait_job() {
  local job="$1" waited=0 resp done status pr detail
  while :; do
    resp=$(curl -sS "$API/api/v1/jobs/$job" -H "Authorization: Bearer $KEY" || echo '{}')
    done=$(jq -r '[.tasks[]?.status] | length > 0 and (map(. == "SUCCEEDED" or . == "FAILED" or . == "CANCELLED") | all)' <<<"$resp")
    if [ "$done" = "true" ]; then
      status=$(jq -r 'if ([.tasks[].status] | all(. == "SUCCEEDED")) then "SUCCEEDED" else "FAILED" end' <<<"$resp")
      pr=$(jq -r '[.tasks[].result_url // empty] | first // ""' <<<"$resp")
      detail=$(jq -r '[.tasks[].result_detail // empty] | first // ""' <<<"$resp" | tr '\n\t' '  ' | cut -c1-160)
      printf '%s\t%s\t%s\n' "$status" "$pr" "$detail"
      return
    fi
    if [ "$waited" -ge "$DEADLINE" ]; then
      printf 'TIMEOUT\t\tharness gave up after %ss\n' "$DEADLINE"
      return
    fi
    sleep 10; waited=$((waited + 10))
  done
}

n=$(jq '.tasks | length' "$TASKS")
for mode in $MODES; do
  for i in $(seq 0 $((n - 1))); do
    id=$(jq -r ".tasks[$i].id" "$TASKS")
    [ -z "$ONLY" ] || [ "$ONLY" = "$id" ] || continue
    task=$(jq -r ".tasks[$i].task" "$TASKS")
    file=$(jq -r ".tasks[$i].file" "$TASKS")

    printf '%-22s %-10s ' "$id" "$mode"
    start=$(date +%s)
    job=$(submit "$id" "$mode" "$task" "$file" || true)
    if [ -z "$job" ]; then
      printf 'SUBMIT_FAILED\n'
      printf '%s\t%s\t\tSUBMIT_FAILED\t0\t\t\n' "$id" "$mode" >> "$OUT"
      continue
    fi
    IFS=$'\t' read -r status pr detail < <(wait_job "$job")
    wall=$(( $(date +%s) - start ))
    printf '%-10s %4ss %s\n' "$status" "$wall" "$pr"
    printf '%s\t%s\t%s\t%s\t%s\t%s\t%s\n' "$id" "$mode" "$job" "$status" "$wall" "$pr" "$detail" >> "$OUT"
  done
done

echo
echo "=== summary ==="
awk -F'\t' 'NR>1 {
  n[$2]++; wall[$2]+=$5
  if ($4=="SUCCEEDED") ok[$2]++
  if ($6!="") pr[$2]++
}
END {
  printf "%-10s %5s %10s %10s %10s\n", "mode", "runs", "succeeded", "pr_opened", "avg_wall_s"
  for (m in n) printf "%-10s %5d %10d %10d %10.0f\n", m, n[m], ok[m], pr[m], wall[m]/n[m]
}' "$OUT"
