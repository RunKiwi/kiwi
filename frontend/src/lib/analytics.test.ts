import { describe, it, beforeEach } from "node:test";
import assert from "node:assert";
import {
  capture,
  identify,
  isEnabled,
  markOnce,
  pagePath,
  resetIdentity,
  sanitize,
  setAnalyticsClient,
  type AnalyticsClient,
  type KeyValueStore,
} from "./analytics.ts";

interface Captured {
  event: string;
  props?: Record<string, unknown>;
}

function fakeClient() {
  const events: Captured[] = [];
  const identities: { id: string; props?: Record<string, unknown> }[] = [];
  let resets = 0;
  const client: AnalyticsClient = {
    capture: (event, props) => { events.push({ event, props }); },
    identify: (id, props) => { identities.push({ id, props }); },
    reset: () => { resets += 1; },
  };
  return { client, events, identities, resetCount: () => resets };
}

function fakeStore(): KeyValueStore {
  const map = new Map<string, string>();
  return {
    getItem: (k) => map.get(k) ?? null,
    setItem: (k, v) => { map.set(k, v); },
  };
}

describe("analytics client installation", () => {
  beforeEach(() => setAnalyticsClient(null));

  it("is a no-op when no client is installed", () => {
    assert.strictEqual(isEnabled(), false);
    // The point of the whole design: an unconfigured build must not throw.
    assert.doesNotThrow(() => capture("signup_started", { method: "github" }));
    assert.doesNotThrow(() => identify("u_1", { org_id: "o_1", plan: "free", activation_state: "active" }));
    assert.doesNotThrow(() => resetIdentity());
  });

  it("forwards events to an installed client", () => {
    const { client, events } = fakeClient();
    setAnalyticsClient(client);

    capture("task_submitted", {
      architect_model: "claude-opus-4-8",
      worker_model: "claude-haiku-4-5-20251001",
      max_workers: 3,
      has_test_cmd: true,
      from_starter: false,
    });

    assert.strictEqual(events.length, 1);
    assert.strictEqual(events[0].event, "task_submitted");
    assert.strictEqual(events[0].props?.architect_model, "claude-opus-4-8");
    assert.strictEqual(events[0].props?.max_workers, 3);
  });

  it("identifies with plan traits but never an email or name", () => {
    const { client, identities } = fakeClient();
    setAnalyticsClient(client);

    identify("u_42", { org_id: "o_7", plan: "free", activation_state: "active" });

    assert.strictEqual(identities.length, 1);
    assert.strictEqual(identities[0].id, "u_42");
    assert.deepStrictEqual(Object.keys(identities[0].props ?? {}).sort(), [
      "activation_state",
      "org_id",
      "plan",
    ]);
  });

  it("ignores an identify with no user id rather than creating an anonymous alias", () => {
    const { client, identities } = fakeClient();
    setAnalyticsClient(client);
    identify("", { org_id: "o_7", plan: "free", activation_state: "active" });
    assert.strictEqual(identities.length, 0);
  });
});

describe("sanitize", () => {
  it("truncates long strings so a backend error can't carry a payload", () => {
    const out = sanitize({ reason: "x".repeat(500) });
    assert.strictEqual((out.reason as string).length, 201); // 200 + ellipsis
    assert.ok((out.reason as string).endsWith("…"));
  });

  it("leaves short strings and non-strings alone", () => {
    const out = sanitize({ reason: "quota exceeded", max_workers: 3, has_test_cmd: false });
    assert.strictEqual(out.reason, "quota exceeded");
    assert.strictEqual(out.max_workers, 3);
    assert.strictEqual(out.has_test_cmd, false);
  });

  it("drops undefined so an omitted optional isn't sent as a null property", () => {
    const out = sanitize({ planner_model: undefined, worker_model: "gpt-5-mini" });
    assert.ok(!("planner_model" in out));
    assert.strictEqual(out.worker_model, "gpt-5-mini");
  });
});

describe("pagePath", () => {
  it("strips the fragment, which is where the auth callback carries the session token", () => {
    assert.strictEqual(pagePath("/auth/callback#token=kw_secret"), "/auth/callback");
  });

  it("strips query strings", () => {
    assert.strictEqual(pagePath("/jobs?repo=acme%2Fprivate"), "/jobs");
  });

  it("keeps a plain path and maps an empty one to root", () => {
    assert.strictEqual(pagePath("/integrations"), "/integrations");
    assert.strictEqual(pagePath("?x=1"), "/");
  });
});

describe("markOnce", () => {
  it("returns true once per key", () => {
    const store = fakeStore();
    assert.strictEqual(markOnce("pr:job_1", store), true);
    assert.strictEqual(markOnce("pr:job_1", store), false);
    assert.strictEqual(markOnce("pr:job_1", store), false);
  });

  it("tracks distinct keys independently", () => {
    const store = fakeStore();
    assert.strictEqual(markOnce("pr:job_1", store), true);
    assert.strictEqual(markOnce("pr:job_2", store), true);
    assert.strictEqual(markOnce("pr:job_1", store), false);
  });

  it("bounds the stored set so it can't grow for the life of the profile", () => {
    const store = fakeStore();
    for (let i = 0; i < 250; i++) markOnce(`pr:job_${i}`, store);
    const seen: string[] = JSON.parse(store.getItem("kiwi_analytics_seen")!);
    assert.strictEqual(seen.length, 200);
    // The oldest keys are the ones evicted.
    assert.ok(!seen.includes("pr:job_0"));
    assert.ok(seen.includes("pr:job_249"));
  });

  it("reports the event when storage is unavailable rather than going silent", () => {
    assert.strictEqual(markOnce("pr:job_1", null), true);
  });

  it("reports the event when stored state is corrupt", () => {
    const store = fakeStore();
    store.setItem("kiwi_analytics_seen", "{not json");
    assert.strictEqual(markOnce("pr:job_1", store), true);
  });
});
