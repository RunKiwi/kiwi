# Kiwi — File Discovery + Multi-File Loop Work Plan

> **Audience:** an implementation agent (the "worker"). Tasks are self-contained.
> Do them **in order**; respect `Depends on`. Open **one PR per phase**. A senior
> model verifies post-merge — leave clear PR descriptions and ship tests first.
> Obey `CLAUDE.md` (pre-commit checks, no literal "Open"+"AI" string, org-scoping,
> BYOC-only zero-knowledge claims).

---

## 0. Orientation (READ FIRST — cold-start brief)

**What Kiwi is.** A BYOC agentic execution platform. The Control Plane (`cmd/kiwid`,
`pkg/orchestrator`) plans a task into worker specs and hands them out via a Postgres
lease queue (`pkg/store/queue.go`). A Data Plane daemon (`cmd/kiwidaemon`, `pkg/daemon`)
leases a task, clones the repo, runs an **Actor–Critic loop** (`pkg/loop`) in a sandbox
until a test command passes, then opens a GitHub PR. The full seam works end-to-end
today (a task can reach a real PR — see `deploy/` and `make local`).

**The problem this plan solves.** The loop is **single-file by construction**:
`loop.Task` carries one `FilePath`, and `provider.Provider` has a single method
`GetCodeEdit(...)` that returns one corrected file. So a task **must** name the exact
file to edit. That is wrong for real bugs: a user who reports *"/api/test doesn't return
the test data, fix it"* does not know which file(s) to touch, and the fix often spans
several files. Today, submitting without a `file` fails with
*"no target file for this task"* (an honest failure added in the provider-robustness PR).

**The goal.** Make `file` optional by (1) **discovering** the target file(s) from the
task description + repo, and (2) letting the loop **edit multiple files** until the test
passes. `test_cmd` is already inferred when omitted (`pkg/daemon/infer.go`), so after this
plan a user can submit **just a task description + repo** and get a PR.

**What already works — do NOT rebuild:**
- **Delivery is already multi-file.** `publishResult` in `pkg/daemon/delivery.go` runs
  `git add -A`, so any number of changed files are committed and pushed. No delivery
  changes are needed.
- **test_cmd inference** exists (`pkg/daemon/infer.go: inferTestCmd`). Reuse it.
- **Honest failures + error classification** exist (`pkg/provider/errors.go: Classify`,
  and the actor==nil / missing-file failures in `pkg/daemon/daemon.go: executeTask`).
- **Budget, stall detection, DAG, lease renewal** — all in `pkg/loop` and `pkg/store`.
  Do not touch their semantics; extend around them.

**Key files to read before starting:**
- `pkg/loop/loop.go` — the Actor–Critic loop (`Runner`, `Task`, `Run`).
- `pkg/provider/mock.go` (the `Provider` interface + `MockProvider`),
  `pkg/provider/llm.go` (Anthropic), `pkg/provider/gemini.go` (Gemini),
  `pkg/provider/critic.go` (`Critic`, `UsageReporter`).
- `pkg/daemon/daemon.go` — `executeTask` (worktree → loop → publish → report),
  `defaultProvider`, `providerNameForModel`; `pkg/daemon/infer.go`.
- `pkg/agent/agent.go` — `WorkerSpec` (has single `File string`).
- `pkg/planner/planner.go`, `pkg/planner/service.go` — `PlanRequest.File` → worker.

**Pre-commit checks (CLAUDE.md §2) — run before EVERY commit, all must pass:**
```bash
gofmt -l cmd/ pkg/                 # must print nothing
CGO_ENABLED=0 go vet ./...         # clean
CGO_ENABLED=0 go test ./pkg/...    # pass
CGO_ENABLED=0 go build ./...       # builds
```
Frontend changes also need `cd frontend && npm run lint && npm run build`.
Label each PR `skip-readme-check` unless it edits the root `README.md`.

**Design invariants (hold these across all phases):**
- **Backward compatible.** Single-file tasks (`spec.File` set) must keep working exactly
  as today. Multi-file is additive.
- **Path safety.** Every discovered or edited path must be validated with
  `filepath.IsLocal` and confirmed to live inside the worktree — reject anything else
  (reuse the check already in `executeTask`). Never write outside the worktree.
- **Bounded.** Cap discovered files (default **6**), per-file size (default **256 KB**),
  and the repo tree sent to the model (default **2000** paths). A runaway must hit the
  loop's existing budget/step caps.
- **Honest failures.** If discovery finds nothing, or an edit response can't be parsed,
  fail with a clear `result_detail` — never a fake success.
- **Secrets.** LLM keys never enter the sandbox (see `isLLMKey`). Discovery/edit calls
  run daemon-side, same as the existing Actor.

---

## Phase 1 — Add a `Complete` primitive to the provider interface

**Depends on:** nothing. **PR:** `feat(provider): generic Complete primitive`.

The loop only has `GetCodeEdit` (one file in, one file out). Discovery and multi-file
editing need a general text completion. Add one method and implement it everywhere.

**Do:**
1. In `pkg/provider/mock.go`, add to the `Provider` interface:
   ```go
   // Complete is a general single-shot completion: given a system and user
   // prompt, return the model's text response. Used for repo exploration and
   // multi-file edits, which are not shaped like GetCodeEdit's single-file fix.
   Complete(ctx context.Context, system, user string) (string, error)
   ```
2. Implement `Complete` on `AnthropicProvider` (`pkg/provider/llm.go`) and
   `GeminiProvider` (`pkg/provider/gemini.go`). Reuse their existing request plumbing
   (`p.actorModel`, `MaxTokens`, cost recording via `recordCost`). Return the collected
   text. Respect the same refusal/error handling as `GetCodeEdit`.
3. Implement `Complete` on `MockProvider` (`pkg/provider/mock.go`). Add a settable field
   `CompleteFunc func(system, user string) (string, error)` so tests drive it
   deterministically; default returns `"", nil` (or a fixed string) when unset.
4. Confirm cost still flows through `UsageReporter.LastCostUSD()` after a `Complete` call.

**Acceptance:** `go build ./...` green; every `Provider` implementation satisfies the new
interface. `Complete` records cost like `GetCodeEdit`.

**Tests:** `pkg/provider` — a test that `MockProvider.Complete` invokes `CompleteFunc`;
table test that the Anthropic/Gemini request builders target the right endpoint (mirror
the existing `gemini_test.go` httptest pattern; do NOT call real APIs).

---

## Phase 2 — Repo tree + file discovery (daemon-side)

**Depends on:** Phase 1. **PR:** `feat(daemon): discover target files from the task`.

**Do:**
1. New file `pkg/daemon/discovery.go`:
   - `func repoTree(worktreePath string) ([]string, error)` — list candidate files via
     `git ls-files` run in the worktree (fall back to a bounded `filepath.WalkDir` if not
     a git repo). Skip `vendor/`, `node_modules/`, `.git/`, and obviously-binary files
     (by extension). Cap at the tree limit (2000). Return repo-relative paths.
   - `func discoverTargetFiles(ctx context.Context, actor provider.Provider, task string, tree []string) ([]string, error)` —
     build a prompt listing `tree`, ask the model to return **only** a JSON array of the
     most relevant repo-relative paths to edit (most-likely first). Parse the JSON
     (tolerate code fences / surrounding prose — extract the first JSON array). Keep only
     paths that (a) appear in `tree`, (b) pass `filepath.IsLocal`. Cap to the file limit
     (6). Return the list; empty slice is a valid "found nothing" result, not an error.
2. Wire into `executeTask` (`pkg/daemon/daemon.go`): after the worktree exists and the
   provider is built, if the spec has no explicit target (`spec.File == "" && len(spec.Files) == 0`),
   call `repoTree` + `discoverTargetFiles`. If it returns empty, fail with a clear reason:
   *"could not identify a file to change from the task description — set one under Advanced
   options"*. Otherwise use the discovered set as the loop's target files (Phase 3).
   (Note: `spec.Files` is introduced in Phase 4; until then, discovery can populate a local
   `[]string`. Structure the code so Phase 4 slots in cleanly.)

**Acceptance:** given a repo and a task, `discoverTargetFiles` returns a bounded, validated
list; malformed/again-out-of-tree paths are dropped; nothing escapes the worktree.

**Tests:** `pkg/daemon/discovery_test.go` — a temp repo with a few files + a `MockProvider`
whose `CompleteFunc` returns a JSON array (including one path NOT in the tree and one
`../escape` path); assert those are filtered out and the result is capped. Test the "empty
result → honest failure" path through `executeTask` with the mock.

---

## Phase 3 — Multi-file editing in the loop

**Depends on:** Phase 1. **PR:** `feat(loop): multi-file Actor edits`.

Extend `pkg/loop` to edit a set of files, keeping the single-file path unchanged.

**Do:**
1. In `pkg/loop/loop.go`, add `Files []string` to `Task` (absolute paths). Semantics:
   if `len(Files) > 0`, run the **multi-file** path; else keep the existing single-file
   `FilePath` path verbatim (back-compat).
2. Multi-file step (new unexported helper, e.g. `proposeMultiFileEdit`):
   - Read all `Files` (skip any over the size cap; error if none readable).
   - Call `Provider.Complete` with a structured prompt: the task, each file's path +
     contents, and the last test output; instruct the model to return **only** JSON:
     `{"files":[{"path":"<relative-or-absolute-matching-input>","content":"<full new file>"}]}`.
     Only files that need changes need be returned.
   - Parse tolerantly (strip fences/prose, decode the JSON object). For each returned
     file: map it back to one of the input `Files` (reject unknown paths and anything not
     `filepath.IsLocal` relative to the worktree root — pass the root into the loop or
     validate in the daemon). Write the new content.
   - Re-run `runTest`; on pass, succeed. Reuse the existing budget accounting
     (`callCost`), step cap, and duplicate-output stall detection.
   - Optional Critic: if set, review each changed file (or the change set) before writing,
     mirroring the single-file Critic gate. Keeping the Critic single-file for now is
     acceptable — document the choice in the PR.
3. Do not change the single-file code path's behavior or its tests.

**Acceptance:** a multi-file `Task` converges to a passing test with a `MockProvider`;
budget/step/stall caps still fire; single-file tests unchanged and green.

**Tests:** `pkg/loop` — a multi-file case where two files must both change (mock returns
both), asserting both are written and the loop reports success; a case where the mock
returns an unknown/escaping path (asserting it's rejected, not written); a stall case.

---

## Phase 4 — Thread a file SET through spec / plan / API / UI

**Depends on:** Phases 2–3. **PR:** `feat: optional multi-file target through the plan`.

**Do:**
1. `pkg/agent/agent.go`: add `Files []string json:"files,omitempty"` to `WorkerSpec`
   (keep `File` for back-compat). Normalize: if `File != ""` and `Files` empty, treat as
   `Files = [File]`.
2. `pkg/planner`: add `Files []string` to `PlanRequest` and thread onto the worker spec
   (`planner.go`, `service.go`), alongside the existing `File`. Keep single `File` working.
3. `pkg/daemon/daemon.go: executeTask`: prefer `spec.Files`; else `spec.File`; else run
   Phase-2 discovery. Validate every path (existing `filepath.IsLocal` check, extended to
   the set). Drive the Phase-3 multi-file loop.
4. Frontend (`frontend/src/lib/api.ts` + task form): `file` is already optional; add an
   optional multi-file affordance only if trivial (a comma-separated "files" advanced
   field). Not required — the headline is that omitting `file` now works via discovery.

**Acceptance:** submitting a plan with no `file` runs discovery; with one `file` behaves as
before; with `files[]` edits that set. All existing planner/daemon tests pass.

**Tests:** planner test that `Files` threads onto the spec; daemon test that `executeTask`
prefers `Files` → `File` → discovery in that order.

---

## Phase 5 — End-to-end verification

**Depends on:** Phases 1–4. **PR:** none (verification; attach results to the Phase 4 PR
or a short `docs/` note).

**Do:** using `make local` (see the Makefile), seed a Gemini key (the Anthropic account is
out of credits — use `gemini-flash-latest`), and submit a task against a repo with a
**multi-file** bug and **no `file` and no `test_cmd`**. Confirm: discovery picks the right
files, `test_cmd` is inferred, the loop edits multiple files, the test passes, and a PR is
opened. Capture the job's `result_detail` and PR URL.

**Acceptance:** one real task with only `{task, repo_url}` reaches a green PR that changes
more than one file.

---

## Guardrails / gotchas

- **Do not call real LLM/GitHub APIs in tests.** Use `MockProvider` and `httptest`.
- **macOS build:** binaries need `-ldflags="-linkmode=external"` + `codesign -s -` (CLAUDE.md §4).
- **Keep the diff surgical.** No refactors of the lease queue, crypto, or delivery.
- **JSON from models is messy** — always parse tolerantly (extract the JSON substring) and
  fail honestly when you can't, with a reason in `result_detail`.
- **One PR per phase**, tests first, all four pre-commit checks green, `skip-readme-check`
  label unless you touch the root README.
