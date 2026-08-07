import { describe, it } from "node:test";
import assert from "node:assert";
import * as api from "./api.ts";

describe("provider resolution", () => {
  // providerOf duplicated a rule that lives in Go. The catalog API is now the
  // one source; a second copy here is how the two drift.
  it("no longer exports a client-side providerOf", () => {
    assert.strictEqual((api as Record<string, unknown>).providerOf, undefined);
    assert.strictEqual((api as Record<string, unknown>).OPENAI_MODEL_PREFIXES, undefined);
  });

  it("exposes catalog and provider fetchers", () => {
    assert.strictEqual(typeof api.client.listProviders, "function");
    assert.strictEqual(typeof api.client.listCatalogModels, "function");
  });
});
