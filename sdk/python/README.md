# kiwi-sdk

Python client for [Kiwi](https://runkiwi.dev) — coding agents that run in infrastructure you control.

You give Kiwi a task, a repository and the test command that proves the task is done. Kiwi plans the work into a graph of scoped workers, runs each one in a sandbox with default-deny networking, and opens a pull request only once your test passes. Model-generated code never holds your provider key.

- **Docs:** https://docs.runkiwi.dev
- **Dashboard:** https://app.runkiwi.dev
- **Source:** https://github.com/RunKiwi/kiwi

## Install

```bash
pip install kiwi-sdk
```

## Usage

```python
import os
from kiwi import KiwiClient

client = KiwiClient("https://api.runkiwi.dev", os.environ["KIWI_SERVER_TOKEN"])

result = client.submit_task(
    task="Fix the division by zero panic in Divide()",
    repo_url="https://github.com/acme/service",
    ref="main",
    test_cmd="go test ./...",
)

# Workers run asynchronously — poll for the result.
job = client.get_job(result["job_id"])
for t in job["tasks"]:
    print(t["status"], t.get("result_url", ""))
```

## API

### `KiwiClient(server, token=None, timeout=30.0)`

| Argument | Type | Notes |
| :--- | :--- | :--- |
| `server` | `str` | Base URL of the Control Plane. |
| `token` | `str` | Optional. Falls back to `KIWI_SERVER_TOKEN`. |
| `timeout` | `float` | Per-request timeout in seconds. |

Raises `ValueError` if you pass an `http://` URL for a non-local host, rather than sending your token in cleartext.

### `submit_task(...)`

Plans the task into a DAG of workers and enqueues them. Returns as soon as the plan is accepted.

| Argument | Type | Notes |
| :--- | :--- | :--- |
| `task` | `str` | **Required.** The natural-language goal. |
| `repo_url` | `str` | Repository to work in. |
| `ref` | `str` | Branch or ref. |
| `file` / `files` | `str` / `list[str]` | Target file(s). Kiwi discovers them if omitted. |
| `test_cmd` | `str` | The command that defines "done". |
| `model` | `str` | Worker model, run on your provider key. |
| `max_workers` | `int` | Cap on the planned DAG width. |
| `idempotency_key` | `str` | Dedupes a retried submission. |

Returns a dict with `job_id`, `manifest_id`, `task_ids` and `summary`.

### `get_job(job_id)`

Returns `{"job_id": ..., "tasks": [{"id", "status", "result_url"?, "result_detail"?}]}`. A task is terminal when its status is no longer `QUEUED`, `LEASED` or `RUNNING`; `result_url` carries the pull request.

### `list_jobs()`

Lists jobs visible to the authenticated org.

## Errors

Non-2xx responses raise `KiwiError` with `.status` and the parsed `.body`. The exception never contains your request headers, so it can be logged safely.

```python
from kiwi import KiwiError

try:
    client.submit_task(task="…")
except KiwiError as err:
    print(err.status, err.body)
```

## Getting a token

Sign in at [app.runkiwi.dev](https://app.runkiwi.dev) and create an API key. The free tier runs on a Kiwi-operated shared fleet with no setup.

## License

MIT
