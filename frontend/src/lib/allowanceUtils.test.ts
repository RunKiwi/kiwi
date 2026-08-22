import { describe, it } from "node:test";
import assert from "node:assert";
import {
  getModelAllowanceStatus,
  getOverallAllowanceHealth,
  findFallbackNoCostModel,
} from "./allowanceUtils.ts";
import type { CatalogModel, AllowanceBucket, UsageResponse } from "./api.ts";

describe("allowanceUtils", () => {
  const catalogModels: CatalogModel[] = [
    {
      org_id: "org_1",
      model_id: "gemini-2.0-flash",
      provider: "gemini",
      display_name: "Gemini 2.0 Flash",
      description: "Fast free model",
      input_cost_per_m: 0,
      output_cost_per_m: 0,
      context_length: 1000000,
      supports_tools: true,
      tier: "free",
      kiwi_provided: true,
      selectable: true,
    },
    {
      org_id: "org_1",
      model_id: "claude-3-5-haiku",
      provider: "anthropic",
      display_name: "Claude 3.5 Haiku",
      description: "Fast economy model",
      input_cost_per_m: 0.8,
      output_cost_per_m: 4.0,
      context_length: 200000,
      supports_tools: true,
      tier: "economy",
      kiwi_provided: true,
      selectable: true,
    },
    {
      org_id: "org_1",
      model_id: "claude-3-5-sonnet",
      provider: "anthropic",
      display_name: "Claude 3.5 Sonnet",
      description: "Frontier model",
      input_cost_per_m: 3.0,
      output_cost_per_m: 15.0,
      context_length: 200000,
      supports_tools: true,
      tier: "frontier",
      kiwi_provided: true,
      selectable: true,
    },
    {
      org_id: "org_1",
      model_id: "gpt-4o",
      provider: "openai",
      display_name: "GPT-4o",
      description: "BYOK OpenAI model",
      input_cost_per_m: 2.5,
      output_cost_per_m: 10.0,
      context_length: 128000,
      supports_tools: true,
      tier: "frontier",
      kiwi_provided: false,
      selectable: true,
    },
  ];

  it("handles BYOK models as unlimited and non-exhausted", () => {
    const allowances: AllowanceBucket[] = [
      { tier: "frontier", period: "2026-08", granted: 50000, used: 50000, remaining: 0 },
    ];
    const status = getModelAllowanceStatus("gpt-4o", catalogModels, allowances);
    assert.strictEqual(status.isBYOK, true);
    assert.strictEqual(status.isExhausted, false);
    assert.strictEqual(status.isUnlimited, true);
  });

  it("correctly flags an exhausted Kiwi-provided model", () => {
    const allowances: AllowanceBucket[] = [
      { tier: "economy", period: "2026-08", granted: 1000000, used: 1000000, remaining: 0 },
    ];
    const status = getModelAllowanceStatus("claude-3-5-haiku", catalogModels, allowances);
    assert.strictEqual(status.isBYOK, false);
    assert.strictEqual(status.isExhausted, true);
    assert.strictEqual(status.tier, "economy");
    assert.strictEqual(status.hint?.includes("Exhausted"), true);
  });

  it("correctly flags a warning state when allowance is 85% spent", () => {
    const allowances: AllowanceBucket[] = [
      { tier: "economy", period: "2026-08", granted: 1000000, used: 850000, remaining: 150000 },
    ];
    const status = getModelAllowanceStatus("claude-3-5-haiku", catalogModels, allowances);
    assert.strictEqual(status.isExhausted, false);
    assert.strictEqual(status.isWarning, true);
    assert.strictEqual(status.percentage, 85);
    assert.strictEqual(status.hint?.includes("150k left"), true);
  });

  it("evaluates overall allowance health correctly", () => {
    const allowances: AllowanceBucket[] = [
      { tier: "free", period: "2026-08", granted: 10000000, used: 100000, remaining: 9900000 },
      { tier: "economy", period: "2026-08", granted: 1000000, used: 1000000, remaining: 0 },
      { tier: "frontier", period: "2026-08", granted: 50000, used: 50000, remaining: 0 },
    ];
    const usage: UsageResponse = {
      plan: "free",
      activation_state: "active",
      agent_minutes_used: 120,
      agent_minutes_limit: 500,
      concurrent_jobs_running: 0,
      max_concurrent_jobs: 1,
    };

    const health = getOverallAllowanceHealth(allowances, usage);
    assert.strictEqual(health.status, "exhausted");
    assert.deepStrictEqual(health.exhaustedTiers, ["economy", "frontier"]);
    assert.strictEqual(health.summaryText.includes("Economy & Frontier"), true);
  });

  it("finds the fallback no-cost model", () => {
    const fallback = findFallbackNoCostModel(catalogModels);
    assert.strictEqual(fallback, "gemini-2.0-flash");
  });
});
