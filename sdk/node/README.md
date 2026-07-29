# @runkiwi/sdk

Node.js client for [Kiwi](https://runkiwi.dev) — coding agents that run in infrastructure you control.

You give Kiwi a task, a codebase and the test command that proves the task is done. Kiwi plans the work into a graph of scoped workers, runs each one in a sandbox with default-deny networking, and returns a pull request only once your test passes. Model-generated code never holds your provider key.

- **Docs:** https://docs.runkiwi.dev
- **Dashboard:** https://app.runkiwi.dev
- **Source:** https://github.com/RunKiwi/kiwi

## Install

```bash
npm install @runkiwi/sdk
```

## Usage

```js
const { KiwiClient } = require('@runkiwi/sdk');

const client = new KiwiClient('https://api.runkiwi.dev', process.env.KIWI_SERVER_TOKEN);

const result = await client.submitTask(
  'Fix the division by zero panic in Divide()',
  'math_utils.go',
  'go test ./...',
  './codebase.zip'
);

console.log(result);
```

### `new KiwiClient(server, token?)`

| Argument | Type | Notes |
| :--- | :--- | :--- |
| `server` | `string` | Base URL of the Control Plane. |
| `token` | `string` | Optional. Falls back to `KIWI_SERVER_TOKEN`. |

The constructor **throws** if you pass an `http://` URL for a non-local host, rather than sending your token in cleartext.

### `submitTask(task, file, testCmd, codebaseZipPath)`

Submits a task with a zipped codebase and resolves to the created job. The test command is the definition of done — the agent loop keeps going until it passes.

If the request fails, the `Authorization` header is stripped from the rethrown error before it propagates, so tokens don't end up in logs.

## Getting a token

Sign in at [app.runkiwi.dev](https://app.runkiwi.dev) and create an API key. The free tier runs on a Kiwi-operated shared fleet with no setup.

## License

MIT
