import { describe, it } from "node:test";
import assert from "node:assert";
import { parseActionableError } from "./errors.ts";

describe("parseActionableError", () => {
  it("maps 402 / inactive org errors to /settings#activation", () => {
    const err = parseActionableError("HTTP 402 Payment Required: Organization is inactive");
    assert.strictEqual(err.code, "INACTIVE_ORG");
    assert.strictEqual(err.actionHref, "/settings#activation");
    assert.strictEqual(err.actionLabel, "Activate in Settings");
  });

  it("maps provider key / 401 errors to /integrations", () => {
    const err = parseActionableError("Anthropic rejected this credential: 401 Unauthorized");
    assert.strictEqual(err.code, "CREDENTIAL_ERROR");
    assert.strictEqual(err.actionHref, "/integrations");
    assert.strictEqual(err.actionLabel, "Manage integrations");
  });

  it("maps an exhausted agent-minute allowance to plan and usage", () => {
    const err = parseActionableError("compute_cap: Out of agent-minutes this month");
    assert.strictEqual(err.code, "COMPUTE_CAP");
    assert.strictEqual(err.actionHref, "/settings#plan");
    assert.strictEqual(err.actionLabel, "Review plan and usage");
  });

  it("returns default message when no specific pattern matches", () => {
    const err = parseActionableError("Internal database connection error");
    assert.strictEqual(err.message, "Internal database connection error");
    assert.strictEqual(err.actionHref, undefined);
    assert.strictEqual(err.actionLabel, undefined);
  });

  it("does not mistake a job id containing 402 for a payment error", () => {
    const err = parseActionableError("planner failed for job_402ab19c7d3e5f01: upstream timeout");
    assert.strictEqual(err.code, undefined);
    assert.strictEqual(err.actionHref, undefined);
    assert.match(err.message, /upstream timeout/);
  });

  it("does not tell a Free org to activate", () => {
    const raw = "402 payment required";
    assert.strictEqual(parseActionableError(raw, { plan: "free" }).code, undefined);
    assert.strictEqual(parseActionableError(raw, { plan: "pro" }).code, "INACTIVE_ORG");
  });

  it("treats a suspended org as suspended, not merely inactive", () => {
    const err = parseActionableError("organization is suspended");
    assert.strictEqual(err.code, "SUSPENDED");
  });

  it("separates the concurrency queue from the agent-minute wall", () => {
    assert.strictEqual(parseActionableError("concurrency_cap reached").code, "CONCURRENCY_CAP");
    assert.strictEqual(parseActionableError("compute_cap: out of agent-minutes").code, "COMPUTE_CAP");
  });

  it("does not send a queued task to the pricing page", () => {
    assert.strictEqual(parseActionableError("concurrency_cap reached").actionHref, undefined);
  });

  it("maps every flavour of missing runner to the fleets page", () => {
    for (const raw of ["no_runner", "runner offline", "provisioning failed", "runner failed to start"]) {
      const err = parseActionableError(raw);
      assert.strictEqual(err.code, "NO_RUNNER", `expected NO_RUNNER for "${raw}"`);
      assert.strictEqual(err.actionHref, "/fleet");
    }
  });

  it("maps an unreachable repository to integrations", () => {
    const err = parseActionableError("could not clone https://github.com/acme/private");
    assert.strictEqual(err.code, "REPO_UNREACHABLE");
    assert.strictEqual(err.actionHref, "/integrations");
  });

  it("preserves the server's own wording for credential failures", () => {
    const raw = "Anthropic rejected this credential";
    assert.strictEqual(parseActionableError(raw).message, raw);
  });

  it("accepts an Error instance and a null-ish value", () => {
    assert.match(parseActionableError(new Error("boom")).message, /boom/);
    assert.ok(parseActionableError(null).message.length > 0);
  });
});
