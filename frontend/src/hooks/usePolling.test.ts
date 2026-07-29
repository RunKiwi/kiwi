import { describe, it } from "node:test";
import assert from "node:assert";

describe("usePolling logic tests", () => {
  it("determines active vs idle interval correctly", () => {
    const activeIntervalMs = 2500;
    const idleIntervalMs = 15000;

    const getInterval = (isIdle: boolean) => (isIdle ? idleIntervalMs : activeIntervalMs);

    assert.strictEqual(getInterval(false), 2500);
    assert.strictEqual(getInterval(true), 15000);
  });

  it("evaluates visibility pause condition", () => {
    const shouldPoll = (hidden: boolean) => !hidden;

    assert.strictEqual(shouldPoll(false), true);
    assert.strictEqual(shouldPoll(true), false);
  });
});
