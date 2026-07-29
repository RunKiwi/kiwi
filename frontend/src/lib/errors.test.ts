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
    assert.strictEqual(err.actionLabel, "Manage Integrations");
  });

  it("maps compute cap and concurrency cap errors to /settings#plan", () => {
    const err = parseActionableError("compute_cap: Out of agent-minutes this month");
    assert.strictEqual(err.code, "CAP_EXCEEDED");
    assert.strictEqual(err.actionHref, "/settings#plan");
    assert.strictEqual(err.actionLabel, "Upgrade Plan");
  });

  it("returns default message when no specific pattern matches", () => {
    const err = parseActionableError("Internal database connection error");
    assert.strictEqual(err.message, "Internal database connection error");
    assert.strictEqual(err.actionHref, undefined);
    assert.strictEqual(err.actionLabel, undefined);
  });
});
