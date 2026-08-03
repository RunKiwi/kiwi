# Loop comparison harness

`docs/rfc-session-loop.md` §11 gates changing any default on evidence: session
vs `file_loop` over a fixed task set, compared on PR-opened rate, human-accept
rate, cost and wall clock. This directory is that comparison, made repeatable.

```bash
KIWI_API_KEY=... ./evals/run.sh                 # both loops, every task
KIWI_API_KEY=... ./evals/run.sh -m session      # one loop
KIWI_API_KEY=... ./evals/run.sh -t t05-cross-package
```

Results are appended to `evals/results/<timestamp>.tsv` and summarised on stdout.

## What it measures, and what it cannot

| Metric | Where it comes from |
| --- | --- |
| PR-opened rate | `result_url` on the finished task |
| Terminal status | the jobs API |
| Wall clock | measured client-side, submit to terminal |
| Cost | **not in the API** — see the query below |
| Human-accept rate | **not measurable here** |

**Human-accept is the metric that matters most and the one a script cannot
produce.** A loop that opens a PR has not necessarily done the work: the fixture
suite passes on `main`, so a no-op also goes green. The PRs are the artifact and
someone has to read them. Treat PR-opened as necessary, not sufficient.

Cost, joined back on by job id:

```sql
SELECT j.id, j.cost_usd, s.round, s.rejections, s.tokens_in, s.tokens_out
FROM jobs j
LEFT JOIN agent_sessions s ON s.job_id = j.id
WHERE j.id = ANY($1);
```

## Why the runs are sequential

A Free-tier org has `MaxConcurrentJobs: 1` (`pkg/auth/limits.go`). Submitting in
parallel would queue behind itself and make the wall-clock column measure the
queue rather than the loop. It also keeps the run inside the 500 agent-minutes a
month the tier allows — a full two-loop pass over this task set is roughly 40
agent-minutes.

Two other tier limits shape the results and should be read alongside them:
`TaskTimeoutSeconds: 600` kills a long run, and it is the *session* loop that
will hit that first.

## The task set

`tasks.json`, ordered roughly by how much each should favour session mode. The
first three are single-file work that `file_loop` was built for; `t04` withholds
the target file so the loop has to find it; `t05` and `t06` need reading one
package to change another, which `file_loop` has no way to express.

Every task is **additive** and the fixture repo's suite passes on `main`. That is
deliberate — per `CLAUDE.md`, the test command is a guard, not the definition of
done, and a loop that returns early on a green suite must be *seen* to no-op
rather than quietly reported as a pass.

The fixture is [`RunKiwi/kiwi-eval`](https://github.com/RunKiwi/kiwi-eval):
small, but genuinely multi-package, so a cross-package task is real rather than
staged.

## Reading a result honestly

- **Sample size.** Six tasks is a pilot, not the ~20 the RFC asks for. One task
  either way moves a rate by 17 points.
- **One sample per cell.** These loops are stochastic; a single run per
  (task, mode) cannot separate a loop being better from a model having a good
  day. Repeat runs before believing a small gap.
- **The fixture is easy.** Everything here is a small, well-specified Go change.
  It probes whether a loop *can* do the work, not how either behaves on a large
  unfamiliar codebase — which is where the two should differ most.
