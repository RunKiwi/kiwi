import { describe, it } from "node:test";
import assert from "node:assert";
import { isNotificationEnabled, setNotificationEnabled } from "./notifications.ts";

describe("notifications helper", () => {
  it("defaults to disabled when no window or permission", () => {
    assert.strictEqual(isNotificationEnabled(), false);
  });

  it("saves preference to storage", () => {
    const storage: Record<string, string> = {};
    globalThis.localStorage = {
      getItem: (k: string) => storage[k] ?? null,
      setItem: (k: string, v: string) => { storage[k] = v; },
      removeItem: (k: string) => { delete storage[k]; },
      clear: () => {},
      length: 0,
      key: () => null,
    };
    (globalThis as unknown as { Notification: unknown }).Notification = { permission: "granted" };

    setNotificationEnabled(true);
    assert.strictEqual(storage["kiwi_notifications_enabled"], "true");
    assert.strictEqual(isNotificationEnabled(), true);

    setNotificationEnabled(false);
    assert.strictEqual(storage["kiwi_notifications_enabled"], "false");
    assert.strictEqual(isNotificationEnabled(), false);
  });
});
