"use client";

import { useEffect, useState } from "react";
import { Compass, Hammer } from "lucide-react";
import { api, providerLabel, type CatalogModel } from "@/lib/api";
import { Select, type SelectOption } from "@/components/Select";

const TIER_LABEL: Record<string, string> = {
  frontier: "👑 Frontier",
  economy: "🌿 Economy",
  free: "⚡ Free",
};

function formatCost(model: CatalogModel): string | null {
  if (model.input_cost_per_m == null && model.output_cost_per_m == null) return null;
  const inCost = model.input_cost_per_m != null ? `$${model.input_cost_per_m.toFixed(2)}/M in` : "—";
  const outCost = model.output_cost_per_m != null ? `$${model.output_cost_per_m.toFixed(2)}/M out` : "—";
  return `${inCost} · ${outCost}`;
}

/**
 * One dropdown, backed by the org's real model catalog
 * (`GET /api/v1/catalog/models`) instead of a hand-typed list — the previous
 * version shipped ids like `claude-3-7-sonnet` that don't exist in
 * `provider.PricingMap` and would fail on submit, and it listed DeepSeek,
 * which isn't one of Kiwi's three providers.
 */
function ModelPicker({
  role,
  value,
  onChange,
  models,
  loading,
}: {
  role: "architect" | "worker";
  value: string;
  onChange: (model: string) => void;
  models: CatalogModel[];
  loading: boolean;
}) {
  const [tierFilter, setTierFilter] = useState<"all" | "frontier" | "economy" | "free">("all");

  const filtered = tierFilter === "all" ? models : models.filter((m) => m.tier === tierFilter);
  const options: SelectOption[] = filtered.map((m) => ({
    value: m.model_id,
    label: m.display_name || m.model_id,
    hint: TIER_LABEL[m.tier]?.replace(/^\S+\s/, "") ?? m.tier,
  }));
  const selectedModel = models.find((m) => m.model_id === value);

  return (
    <div className="space-y-1.5">
      <div className="flex items-center justify-between">
        <label className="font-bold text-stone-800 flex items-center gap-1.5 text-xs">
          {role === "architect" ? (
            <Compass className="w-3.5 h-3.5 text-indigo-600" />
          ) : (
            <Hammer className="w-3.5 h-3.5 text-emerald-600" />
          )}
          <span>{role === "architect" ? "Architect Model (Planning & Strategy)" : "Worker Model (Code Edits & Test Fixes)"}</span>
        </label>
        {selectedModel && (
          <span className="text-[9px] font-mono font-bold bg-sand-100 text-stone-700 px-1.5 py-0.2 rounded border border-sand-200 uppercase">
            {selectedModel.tier}
          </span>
        )}
      </div>

      <div className="flex items-center gap-1 text-[10px]">
        {(["all", "frontier", "economy", "free"] as const).map((t) => (
          <button
            key={t}
            type="button"
            onClick={() => setTierFilter(t)}
            className={`px-2 py-0.5 rounded-lg font-semibold transition-all ${
              tierFilter === t ? "bg-stone-900 text-white" : "bg-sand-100 text-stone-600 hover:bg-sand-200"
            }`}
          >
            {t === "all" ? "All" : TIER_LABEL[t]}
          </button>
        ))}
      </div>

      {loading ? (
        <div className="w-full px-3.5 py-2.5 rounded-xl bg-sand-50/90 border border-sand-200 text-xs text-stone-400">
          Loading models…
        </div>
      ) : options.length === 0 ? (
        <div className="w-full px-3.5 py-2.5 rounded-xl bg-sand-50/90 border border-sand-200 text-xs text-stone-400">
          No models available in this tier.
        </div>
      ) : (
        <Select
          value={value}
          onChange={onChange}
          options={options}
          searchable
          placeholder="Select a model…"
          ariaLabel={`${role} model`}
          renderDetail={(opt) => {
            const m = models.find((mm) => mm.model_id === opt.value);
            if (!m) return null;
            const cost = formatCost(m);
            return (
              <div className="space-y-1.5 px-1 pb-0.5">
                <div className="flex items-center justify-between">
                  <span className="text-[9px] font-mono font-bold px-1.5 py-0.2 rounded border bg-sand-100 text-stone-700 border-sand-200 uppercase">
                    {m.tier}
                  </span>
                  <span className="text-[9px] font-mono text-stone-400 uppercase font-semibold">{providerLabel(m.provider)}</span>
                </div>
                {m.description && <p className="text-[11px] text-stone-600 leading-snug">{m.description}</p>}
                <div className="flex items-center justify-between text-[10px] text-stone-500 font-mono pt-1 border-t border-sand-150">
                  <span>{cost ?? "Pricing not available"}</span>
                  <span>{m.context_length ? `${Math.round(m.context_length / 1000)}k context` : ""}</span>
                </div>
                {m.kiwi_provided && (
                  <span className="inline-block text-[9px] font-mono font-bold text-emerald-700 bg-emerald-50 border border-emerald-200 px-1.5 py-0.2 rounded">
                    Kiwi-funded
                  </span>
                )}
              </div>
            );
          }}
        />
      )}
    </div>
  );
}

export function ModelSelector({
  architectModel,
  workerModel,
  onArchitectChange,
  onWorkerChange,
}: {
  architectModel: string;
  workerModel: string;
  onArchitectChange: (model: string) => void;
  onWorkerChange: (model: string) => void;
}) {
  const [models, setModels] = useState<CatalogModel[]>([]);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    api
      .listCatalogModels()
      .then((res) => setModels((res.models || []).filter((m) => m.selectable)))
      .catch(() => setModels([]))
      .finally(() => setLoading(false));
  }, []);

  return (
    <div className="p-4 rounded-2xl bg-white border border-sand-200 shadow-xs space-y-3">
      <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
        <ModelPicker role="architect" value={architectModel} onChange={onArchitectChange} models={models} loading={loading} />
        <ModelPicker role="worker" value={workerModel} onChange={onWorkerChange} models={models} loading={loading} />
      </div>
    </div>
  );
}
