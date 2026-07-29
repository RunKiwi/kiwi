# @runkiwi/sdk

Node.js client for [Kiwi](https://runkiwi.dev) — coding agents that run in infrastructure you control.

You give Kiwi a task, a repository and the test command that proves the task is done. Kiwi plans the work into a graph of scoped workers, runs each one in a sandbox with default-deny networking, and opens a pull request only once your test passes. Model-generated code never holds your provider key.

- **Docs:** https://docs.runkiwi.dev
- **Dashboard:** https://app.runkiwi.dev
- **Source:** https://github.com/RunKiwi/kiwi

Zero dependencies. Requires Node 18+ (uses native `fetch`).

## Install

```bash
npm install @runkiwi/sdk
```

## Usage

```js
const { KiwiClient } = require('@runkiwi/sdk');

const client = new KiwiClient('https://api.runkiwi.dev', process.env.KIWI_SERVER_TOKEN);

const { job_id } = await client.submitTask({
  task: 'Fix the division by zero panic in Divide()',
  repoUrl: 'https://github.com/acme/service',
  ref: 'main',
  testCmd: 'go test ./...',
});

// Workers run asynchronously — poll for the result.
const job = await client.getJob(job_id);
console.log(job.tasks.map((t) => `${t.status} ${t.result_url ?? ''}`));
```

## API

### `new KiwiClient(server, token?)`

| Argument | Type | Notes |
| :--- | :--- | :--- |
| `server` | `string` | Base URL of the Control Plane. |
| `token` | `string` | Optional. Falls back to `KIWI_SERVER_TOKEN`. |

Throws if you pass an `http://` URL for a non-local host, rather than sending your token in cleartext.

### `submitTask(options)`

Plans the task into a DAG of workers and enqueues them. Resolves as soon as the plan is accepted.

| Option | Type | Notes |
| :--- | :--- | :--- |
| `task` | `string` | **Required.** The natural-language goal. |
| `repoUrl` | `string` | Repository to work in. |
| `ref` | `string` | Branch or ref. |
| `file` / `files` | `string` / `string[]` | Target file(s). Kiwi discovers them if omitted. |
| `testCmd` | `string` | The command that defines "done". |
| `model` | `string` | Worker model, run on your provider key. |
| `maxWorkers` | `number` | Cap on the planned DAG width. |
| `idempotencyKey` | `string` | Dedupes a retried submission. |

Returns `{ job_id, manifest_id, task_ids, summary }`.

### `getJob(jobId)`

Returns `{ job_id, tasks: [{ id, status, result_url?, result_detail? }] }`. A task is terminal when its status is no longer `QUEUED`, `LEASED` or `RUNNING`; `result_url` carries the pull request.

### `listJobs()`

Lists jobs visible to the authenticated org.

## Errors

Non-2xx responses throw a `KiwiError` with `status` and the parsed `body`. The error never contains your request headers, so a caught error can be logged safely.

```js
const { KiwiError } = require('@runkiwi/sdk');

try {
  await client.submitTask({ task: '…' });
} catch (err) {
  if (err instanceof KiwiError) console.error(err.status, err.body);
}
```

## Getting a token

Sign in at [app.runkiwi.dev](https://app.runkiwi.dev) and create an API key. The free tier runs on a Kiwi-operated shared fleet with no setup.

## License

MIT
