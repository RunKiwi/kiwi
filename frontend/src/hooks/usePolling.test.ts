import { describe, it } from "node:test";
import assert from "node:assert";
import { pollIntervalFor } from "./usePolling.ts";

describe("pollIntervalFor", () => {
  it("uses the active interval while work can still change", () => {
    assert.strictEqual(pollIntervalFor(false, { activeIntervalMs: 2500, idleIntervalMs: 15000 }), 2500);
  });

  it("backs off to the idle interval once everything is terminal", () => {
    assert.strictEqual(pollIntervalFor(true, { activeIntervalMs: 2500, idleIntervalMs: 15000 }), 15000);
  });

  it("falls back to sane defaults when no options are supplied", () => {
    assert.strictEqual(pollIntervalFor(false), 2500);
    assert.strictEqual(pollIntervalFor(true), 15000);
  });

  it("honours a partial options object", () => {
    assert.strictEqual(pollIntervalFor(true, { idleIntervalMs: 30000 }), 30000);
    assert.strictEqual(pollIntervalFor(false, { idleIntervalMs: 30000 }), 2500);
  });

  it("always backs off rather than speeding up when idle", () => {
    assert.ok(
      pollIntervalFor(true) > pollIntervalFor(false),
      "idle interval must be longer than the active one",
    );
  });
});
