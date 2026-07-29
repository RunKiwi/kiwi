'use strict';

/**
 * Node client for Kiwi.
 *
 * Submits to /api/v1/planner/plan — the same path `kiwi submit` uses, and the
 * one that feeds the daemon lease queue. The older /tasks endpoint runs the
 * loop control-plane-side, never hands work to a daemon, and gates on
 * org.CanRun(), which returns 402 for every free-tier org.
 */

class KiwiError extends Error {
  constructor(message, status, body) {
    super(message);
    this.name = 'KiwiError';
    this.status = status;
    this.body = body;
  }
}

class KiwiClient {
  /**
   * @param {string} server Base URL of the Control Plane.
   * @param {string} [token] Falls back to KIWI_SERVER_TOKEN.
   */
  constructor(server, token) {
    if (!server) throw new Error('server is required');
    if (server.startsWith('http://') && !server.includes('localhost') && !server.includes('127.0.0.1')) {
      throw new Error('Refusing to send token over cleartext HTTP to remote server. Use HTTPS.');
    }
    this.server = server.replace(/\/+$/, '');
    this.token = token || process.env.KIWI_SERVER_TOKEN;
  }

  async #request(method, path, { body, idempotencyKey } = {}) {
    const headers = { Authorization: `Bearer ${this.token}` };
    if (body !== undefined) headers['Content-Type'] = 'application/json';
    if (idempotencyKey) headers['Idempotency-Key'] = idempotencyKey;

    const res = await fetch(`${this.server}${path}`, {
      method,
      headers,
      body: body === undefined ? undefined : JSON.stringify(body),
    });

    const text = await res.text();
    let parsed;
    try {
      parsed = text ? JSON.parse(text) : null;
    } catch {
      parsed = text;
    }

    if (!res.ok) {
      // The message carries the server's text but never the request headers,
      // so a thrown error can't leak the token into a log.
      throw new KiwiError(`Kiwi ${method} ${path} failed: ${res.status}`, res.status, parsed);
    }
    return parsed;
  }

  /**
   * Plan and enqueue a task. Resolves once the plan is accepted — the workers
   * run asynchronously, so poll getJob() for the result.
   *
   * @param {object} opts
   * @param {string} opts.task Natural-language goal. Required.
   * @param {string} [opts.repoUrl] Repository to work in.
   * @param {string} [opts.ref] Branch or ref. Defaults server-side.
   * @param {string} [opts.file] Single target file.
   * @param {string[]} [opts.files] Multiple target files.
   * @param {string} [opts.testCmd] The command that defines "done".
   * @param {string} [opts.model] Worker model, run on your provider key.
   * @param {number} [opts.maxWorkers] Cap on the planned DAG width.
   * @param {string} [opts.idempotencyKey] Dedupes a retried submission.
   * @returns {Promise<{job_id: string, manifest_id: string, task_ids: string[], summary: string}>}
   */
  async submitTask(opts = {}) {
    if (!opts.task) throw new Error('task is required');

    const body = {
      task: opts.task,
      repo_url: opts.repoUrl,
      ref: opts.ref,
      file: opts.file,
      files: opts.files,
      test_cmd: opts.testCmd,
      model: opts.model,
      max_workers: opts.maxWorkers,
    };
    for (const k of Object.keys(body)) {
      if (body[k] === undefined) delete body[k];
    }

    return this.#request('POST', '/api/v1/planner/plan', {
      body,
      idempotencyKey: opts.idempotencyKey,
    });
  }

  /**
   * Fetch a job and its per-worker task states.
   * @param {string} jobId
   * @returns {Promise<{job_id: string, tasks: Array<{id: string, status: string, result_url?: string}>}>}
   */
  async getJob(jobId) {
    if (!jobId) throw new Error('jobId is required');
    return this.#request('GET', `/api/v1/jobs/${encodeURIComponent(jobId)}`);
  }

  /** List jobs visible to the authenticated org. */
  async listJobs() {
    return this.#request('GET', '/api/v1/jobs');
  }
}

module.exports = { KiwiClient, KiwiError };
