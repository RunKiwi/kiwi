import { describe, it } from "node:test";
import assert from "node:assert";
import * as api from "./api.ts";
import { RECOMMENDED_MODELS } from "./api.ts";

describe("provider resolution", () => {
  // providerOf duplicated a rule that lives in Go (provider.ProviderOf). The
  // catalog API is now the one source, and each catalog row carries the
  // provider the Control Plane resolved. A second copy here is how the two
  // drift into telling a user to connect a key their model does not use.
  it("no longer exports a client-side providerOf", () => {
    assert.strictEqual((api as Record<string, unknown>).providerOf, undefined);
    assert.strictEqual((api as Record<string, unknown>).OPENAI_MODEL_PREFIXES, undefined);
  });

  it("exposes catalog and provider fetchers", () => {
    assert.strictEqual(typeof api.client.listProviders, "function");
    assert.strictEqual(typeof api.client.listCatalogModels, "function");
  });
});

// RECOMMENDED_MODELS is still a hardcoded list, and its `provider` field is what
// the Models page shows and groups by. Nothing resolves it at runtime, so a
// stale value there tells the user to connect the wrong key for a model we
// ourselves recommended. This is the one place a hand-written provider label
// survives, so it is the one place that still needs checking.
describe("recommended models", () => {
  const KNOWN_PROVIDERS = new Set(["anthropic", "gemini", "openai", "openrouter"]);

  it("labels every recommended model with a known provider", () => {
    for (const m of RECOMMENDED_MODELS) {
      assert.ok(
        KNOWN_PROVIDERS.has(m.provider),
        `${m.id} is labelled ${m.provider}, which is not a provider in the registry`,
      );
    }
  });

  it("gives every recommended model a non-empty id and label", () => {
    for (const m of RECOMMENDED_MODELS) {
      assert.ok(m.id.trim().length > 0, "a recommended model has an empty id");
      assert.ok(m.label.trim().length > 0, `${m.id} has no display label`);
    }
  });

  it("does not recommend the same model twice", () => {
    const seen = new Set<string>();
    for (const m of RECOMMENDED_MODELS) {
      assert.ok(!seen.has(m.id), `${m.id} is recommended twice`);
      seen.add(m.id);
    }
  });
});
