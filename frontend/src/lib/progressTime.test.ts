import { describe, it } from "node:test";
import assert from "node:assert";
import { elapsedSince } from "./progressTime.ts";

describe("elapsedSince", () => {
  it("returns null when there is no phase_since", () => {
    assert.strictEqual(elapsedSince(undefined), null);
  });

  it("returns null for an unparseable timestamp", () => {
    assert.strictEqual(elapsedSince("not-a-date"), null);
  });

  it("returns whole seconds elapsed since the timestamp", () => {
    const since = new Date(Date.now() - 90_000).toISOString();
    const got = elapsedSince(since);
    assert.ok(got !== null && got >= 89 && got <= 91, `got ${got}, want ~90`);
  });

  it("never returns negative — a clock skewed forward reads as just-started", () => {
    const future = new Date(Date.now() + 5_000).toISOString();
    assert.strictEqual(elapsedSince(future), 0);
  });
});
