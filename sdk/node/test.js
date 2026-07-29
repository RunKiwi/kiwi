'use strict';

const assert = require('node:assert');
const http = require('node:http');
const { test } = require('node:test');

const { KiwiClient, KiwiError } = require('./index.js');

/** Runs fn against a stub server, capturing the request it received. */
async function withServer(handler, fn) {
  const seen = {};
  const server = http.createServer((req, res) => {
    let body = '';
    req.on('data', (c) => (body += c));
    req.on('end', () => {
      seen.method = req.method;
      seen.url = req.url;
      seen.headers = req.headers;
      seen.body = body ? JSON.parse(body) : null;
      handler(req, res);
    });
  });
  await new Promise((r) => server.listen(0, '127.0.0.1', r));
  try {
    return await fn(`http://127.0.0.1:${server.address().port}`, seen);
  } finally {
    server.close();
  }
}

const ok = (payload) => (_req, res) => {
  res.writeHead(202, { 'Content-Type': 'application/json' });
  res.end(JSON.stringify(payload));
};

// The bug this guards: /tasks runs the loop control-plane-side and gates on
// org.CanRun(), so it returns 402 for every free-tier org. Submissions must go
// to the planner path that feeds the daemon lease queue.
test('submitTask posts to the planner path', async () => {
  await withServer(ok({ job_id: 'job_1' }), async (base, seen) => {
    const client = new KiwiClient(base, 't');
    const res = await client.submitTask({ task: 'fix Divide()' });
    assert.strictEqual(seen.url, '/api/v1/planner/plan');
    assert.strictEqual(seen.method, 'POST');
    assert.strictEqual(res.job_id, 'job_1');
  });
});

test('submitTask maps camelCase options to the wire format', async () => {
  await withServer(ok({ job_id: 'j' }), async (base, seen) => {
    const client = new KiwiClient(base, 't');
    await client.submitTask({
      task: 'add logging',
      repoUrl: 'https://github.com/x/y',
      ref: 'main',
      testCmd: 'go test ./...',
      maxWorkers: 3,
    });
    assert.deepStrictEqual(seen.body, {
      task: 'add logging',
      repo_url: 'https://github.com/x/y',
      ref: 'main',
      test_cmd: 'go test ./...',
      max_workers: 3,
    });
  });
});

test('unset options are omitted rather than sent as null', async () => {
  await withServer(ok({ job_id: 'j' }), async (base, seen) => {
    await new KiwiClient(base, 't').submitTask({ task: 'only a task' });
    assert.deepStrictEqual(Object.keys(seen.body), ['task']);
  });
});

test('idempotency key is forwarded as a header', async () => {
  await withServer(ok({ job_id: 'j' }), async (base, seen) => {
    await new KiwiClient(base, 't').submitTask({ task: 't', idempotencyKey: 'abc-123' });
    assert.strictEqual(seen.headers['idempotency-key'], 'abc-123');
  });
});

test('bearer token is sent', async () => {
  await withServer(ok({ job_id: 'j' }), async (base, seen) => {
    await new KiwiClient(base, 'secret-token').submitTask({ task: 't' });
    assert.strictEqual(seen.headers.authorization, 'Bearer secret-token');
  });
});

test('getJob reads the job status endpoint', async () => {
  const handler = (_req, res) => {
    res.writeHead(200, { 'Content-Type': 'application/json' });
    res.end(JSON.stringify({ job_id: 'job_1', tasks: [{ id: 't1', status: 'SUCCEEDED' }] }));
  };
  await withServer(handler, async (base, seen) => {
    const job = await new KiwiClient(base, 't').getJob('job_1');
    assert.strictEqual(seen.url, '/api/v1/jobs/job_1');
    assert.strictEqual(job.tasks[0].status, 'SUCCEEDED');
  });
});

test('an error carries the status and never the request headers', async () => {
  const handler = (_req, res) => {
    res.writeHead(402, { 'Content-Type': 'application/json' });
    res.end(JSON.stringify({ error: 'payment required' }));
  };
  await withServer(handler, async (base) => {
    const client = new KiwiClient(base, 'secret-token');
    await assert.rejects(
      () => client.submitTask({ task: 't' }),
      (err) => {
        assert.ok(err instanceof KiwiError);
        assert.strictEqual(err.status, 402);
        assert.deepStrictEqual(err.body, { error: 'payment required' });
        // A token in a thrown error ends up in logs.
        assert.ok(!JSON.stringify(err.body).includes('secret-token'));
        assert.ok(!err.message.includes('secret-token'));
        return true;
      }
    );
  });
});

test('refuses cleartext HTTP to a remote host', () => {
  assert.throws(() => new KiwiClient('http://api.example.com', 't'), /cleartext HTTP/);
  assert.doesNotThrow(() => new KiwiClient('http://localhost:8080', 't'));
  assert.doesNotThrow(() => new KiwiClient('https://api.example.com', 't'));
});

test('task is required', async () => {
  await assert.rejects(() => new KiwiClient('https://x.dev', 't').submitTask({}), /task is required/);
});

test('a trailing slash on the server URL does not double up', async () => {
  await withServer(ok({ job_id: 'j' }), async (base, seen) => {
    await new KiwiClient(`${base}/`, 't').submitTask({ task: 't' });
    assert.strictEqual(seen.url, '/api/v1/planner/plan');
  });
});
