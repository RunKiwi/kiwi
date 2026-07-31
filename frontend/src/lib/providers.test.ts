import { describe, it } from "node:test";
import assert from "node:assert";
import { providerOf, RECOMMENDED_MODELS, BUILTIN_MODELS } from "./api.ts";

// providerOf decides which key the dashboard tells you to connect. The daemon
// decides which key the task then actually needs, using provider.ProviderOf in
// the Go tree. When the two disagree the UI reports a model as ready and the
// task fails on a credential nobody was asked for — so this mirrors the Go
// table case for case.
describe("providerOf", () => {
  it("routes each model family", () => {
    const cases: Record<string, string> = {
      "claude-opus-4-8": "anthropic",
      "claude-haiku-4-5-20251001": "anthropic",
      "gemini-flash-latest": "gemini",
      "gemini-2.0-flash": "gemini",
      "gpt-5": "openai",
      "gpt-5-mini": "openai",
      "gpt-4.1": "openai",
      "gpt-4o-mini": "openai",
      "GPT-5": "openai",
      "o3-mini": "openai",
      "o1-preview": "openai",
      "chatgpt-4o-latest": "openai",
      "some-new-model": "anthropic",
      "": "anthropic",
    };
    for (const [model, want] of Object.entries(cases)) {
      assert.equal(providerOf(model), want, `providerOf(${JSON.stringify(model)})`);
    }
  });

  // The o-prefixes must stay narrow. A bare "o" would claim "opus-*" for
  // OpenAI, routing an Anthropic model to a provider the org may not have.
  it("does not over-match on a leading o", () => {
    for (const model of ["opus-4-8", "orca-mini", "olmo-2"]) {
      assert.notEqual(providerOf(model), "openai", model);
    }
  });
});

// The `provider` field on a recommended model is what the Models page shows and
// what it groups by. A stale value there tells the user to connect the wrong
// key for a model we ourselves recommended.
describe("model catalog", () => {
  it("labels every recommended model with the provider it routes to", () => {
    for (const m of RECOMMENDED_MODELS) {
      assert.equal(providerOf(m.id), m.provider, `${m.id} is labelled ${m.provider}`);
    }
  });

  it("offers a built-in model for every provider", () => {
    const covered = new Set(BUILTIN_MODELS.map(providerOf));
    for (const p of ["anthropic", "gemini", "openai"]) {
      assert.ok(covered.has(p), `no built-in model routes to ${p}`);
    }
  });
});
