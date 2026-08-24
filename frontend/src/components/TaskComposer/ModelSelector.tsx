"use client";

import { useEffect, useState } from "react";
import { Compass, Hammer, Sparkles, Cpu, DollarSign, Layers, AlertTriangle, AlertCircle } from "lucide-react";
import { api, providerLabel, type CatalogModel, type AllowanceBucket } from "@/lib/api";
import { getModelAllowanceStatus } from "@/lib/allowanceUtils";
import { Select, type SelectOption } from "@/components/Select";

const TIER_META = {
  frontier: {
    label: "👑 Frontier",
    badge: "bg-gradient-to-r from-amber-100 to-orange-100 text-amber-900 border-amber-300 font-bold",
    pill: "from-amber-500 to-orange-500 text-white shadow-xs",
  },
  economy: {
    label: "🌿 Economy",
    badge: "bg-emerald-50 text-emerald-900 border-emerald-300 font-semibold",
    pill: "from-emerald-600 to-kiwi-700 text-white shadow-xs",
  },
  free: {
    label: "⚡ Free",
    badge: "bg-sky-50 text-sky-900 border-sky-300 font-semibold",
    pill: "from-sky-500 to-blue-600 text-white shadow-xs",
  },
};

function getProviderMeta(provider: string) {
  const p = (provider || "").toLowerCase();
  if (p.includes("anthropic") || p.includes("claude")) {
    return { name: "Anthropic", icon: "✦", bg: "bg-amber-50 text-amber-900 border-amber-200" };
  }
  if (p.includes("openai") || p.includes("gpt")) {
    return { name: "OpenAI", icon: "✻", bg: "bg-emerald-50 text-emerald-900 border-emerald-200" };
  }
  if (p.includes("google") || p.includes("gemini")) {
    return { name: "Google", icon: "✧", bg: "bg-blue-50 text-blue-900 border-blue-200" };
  }
  if (p.includes("deepseek")) {
    return { name: "DeepSeek", icon: "🐋", bg: "bg-cyan-50 text-cyan-900 border-cyan-200" };
  }
  if (p.includes("meta") || p.includes("llama")) {
    return { name: "Meta", icon: "♾️", bg: "bg-purple-50 text-purple-900 border-purple-200" };
  }
  if (p.includes("mistral")) {
    return { name: "Mistral", icon: "🌪️", bg: "bg-orange-50 text-orange-900 border-orange-200" };
  }
  return { name: providerLabel(provider), icon: "⚙️", bg: "bg-sand-100 text-stone-800 border-sand-200" };
}

function formatCost(model: CatalogModel): string | null {
  if (model.input_cost_per_m == null && model.output_cost_per_m == null) return null;
  const inCost = model.input_cost_per_m != null ? `$${model.input_cost_per_m.toFixed(2)}/M in` : "—";
  const outCost = model.output_cost_per_m != null ? `$${model.output_cost_per_m.toFixed(2)}/M out` : "—";
  return `${inCost} · ${outCost}`;
}

function ModelPicker({
  role,
  value,
  onChange,
  models,
  allowance,
  loading,
}: {
  role: "architect" | "worker";
  value: string;
  onChange: (model: string) => void;
  models: CatalogModel[];
  allowance: AllowanceBucket[];
  loading: boolean;
}) {
  const [tierFilter, setTierFilter] = useState<"all" | "frontier" | "economy" | "free">("all");

  const filtered = tierFilter === "all" ? models : models.filter((m) => m.tier === tierFilter);

  const options: SelectOption[] = filtered.map((m) => {
    const prov = getProviderMeta(m.provider);
    const tier = TIER_META[m.tier as keyof typeof TIER_META] || TIER_META.economy;
    const status = getModelAllowanceStatus(m.model_id, models, allowance);
    const isExhausted = allowance.length > 0 && status.isExhausted && !status.isBYOK;

    return {
      value: m.model_id,
      label: m.display_name || m.model_id,
      sublabel: isExhausted
        ? `${prov.name} · ⛔ Quota Exhausted`
        : `${prov.name} · ${m.context_length ? `${Math.round(m.context_length / 1000)}k ctx` : "Standard context"}`,
      icon: <span className="text-xs font-bold leading-none">{prov.icon}</span>,
      badge: isExhausted ? (
        <span className="text-[9px] font-mono px-2 py-0.5 rounded-full border shadow-2xs bg-rose-50 text-rose-800 border-rose-300 font-bold">
          ⛔ EXHAUSTED
        </span>
      ) : (
        <span className={`text-[9px] font-mono px-2 py-0.5 rounded-full border shadow-2xs ${tier.badge}`}>
          {m.tier.toUpperCase()}
        </span>
      ),
    };
  });

  // Ensure current selected value is always in options even if tierFilter or custom
  if (value && !options.some((o) => o.value === value)) {
    const known = models.find((m) => m.model_id === value);
    if (known) {
      const prov = getProviderMeta(known.provider);
      const tier = TIER_META[known.tier as keyof typeof TIER_META] || TIER_META.economy;
      options.unshift({
        value: known.model_id,
        label: known.display_name || known.model_id,
        sublabel: `${prov.name} · ${known.tier}`,
        icon: <span className="text-xs font-bold leading-none">{prov.icon}</span>,
        badge: (
          <span className={`text-[9px] font-mono px-2 py-0.5 rounded-full border shadow-2xs ${tier.badge}`}>
            {known.tier.toUpperCase()}
          </span>
        ),
      });
    } else {
      options.unshift({
        value: value,
        label: value,
        sublabel: "Selected Model",
        icon: <span className="text-xs font-bold leading-none">⚙️</span>,
      });
    }
  }

  const selectedModel = models.find((m) => m.model_id === value);
  const selectedTier = selectedModel?.tier && TIER_META[selectedModel.tier as keyof typeof TIER_META];
  const selectedStatus = selectedModel ? getModelAllowanceStatus(selectedModel.model_id, models, allowance) : null;
  const isSelectedExhausted = allowance.length > 0 && selectedStatus?.isExhausted && !selectedStatus.isBYOK;

  return (
    <div className="space-y-2">
      <div className="flex items-center justify-between">
        <label className="font-bold text-stone-800 flex items-center gap-1.5 text-xs">
          {role === "architect" ? (
            <div className="w-5 h-5 rounded-md bg-indigo-50 border border-indigo-200 flex items-center justify-center text-indigo-700">
              <Compass className="w-3.5 h-3.5" />
            </div>
          ) : (
            <div className="w-5 h-5 rounded-md bg-emerald-50 border border-emerald-200 flex items-center justify-center text-emerald-700">
              <Hammer className="w-3.5 h-3.5" />
            </div>
          )}
          <span>{role === "architect" ? "Architect Model (Planning & Strategy)" : "Implementer Model (Code & Tests)"}</span>
        </label>

        {selectedModel && (
          <div className="flex items-center gap-1">
            {isSelectedExhausted ? (
              <span className="text-[9px] font-mono font-bold px-2 py-0.5 rounded-full border bg-rose-50 text-rose-800 border-rose-300">
                ⛔ QUOTA EXHAUSTED
              </span>
            ) : (
              <span className={`text-[10px] font-mono font-bold px-2 py-0.5 rounded-full border ${selectedTier?.badge || "bg-sand-100 text-stone-700 border-sand-200"}`}>
                {selectedModel.tier.toUpperCase()}
              </span>
            )}
          </div>
        )}
      </div>

      {/* Tier Category Filters */}
      <div className="flex items-center gap-1.5 text-[11px]">
        {(["all", "frontier", "economy", "free"] as const).map((t) => {
          const isSelected = tierFilter === t;
          const tierAllowance = allowance.find((a) => a.tier === t);
          const isTierExhausted = allowance.length > 0 && tierAllowance && tierAllowance.granted >= 0 && (tierAllowance.remaining <= 0 || tierAllowance.used >= tierAllowance.granted);

          return (
            <button
              key={t}
              type="button"
              onClick={() => setTierFilter(t)}
              className={`px-2.5 py-1 rounded-xl font-semibold transition-all cursor-pointer border text-xs flex items-center gap-1 ${
                isSelected
                  ? t === "frontier"
                    ? "bg-gradient-to-r from-amber-500 to-orange-500 text-white border-transparent shadow-xs"
                    : t === "economy"
                    ? "bg-gradient-to-r from-emerald-600 to-kiwi-700 text-white border-transparent shadow-xs"
                    : t === "free"
                    ? "bg-gradient-to-r from-sky-500 to-blue-600 text-white border-transparent shadow-xs"
                    : "bg-stone-900 text-white border-stone-900 shadow-xs"
                  : "bg-sand-100/90 text-stone-600 hover:text-stone-900 hover:bg-sand-200/80 border-sand-200"
              }`}
            >
              <span>{t === "all" ? "All Models" : TIER_META[t]?.label || t}</span>
              {isTierExhausted && <span className="text-[10px] font-bold text-rose-500">⛔</span>}
            </button>
          );
        })}
      </div>

      {loading ? (
        <div className="w-full px-4 py-3 rounded-2xl bg-sand-50/90 border border-sand-200 text-xs text-stone-400 font-mono animate-pulse">
          Fetching available models…
        </div>
      ) : options.length === 0 ? (
        <div className="w-full px-4 py-3 rounded-2xl bg-sand-50/90 border border-sand-200 text-xs text-stone-400 font-mono">
          No models currently selectable in this tier.
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
            const prov = getProviderMeta(m.provider);
            const cost = formatCost(m);
            const isArchitect = role === "architect";
            const status = getModelAllowanceStatus(m.model_id, models, allowance);
            const isExhausted = allowance.length > 0 && status.isExhausted && !status.isBYOK;

            return (
              <div className="space-y-2 text-xs text-stone-800">
                {/* Header info */}
                <div className="flex items-center justify-between gap-2">
                  <div className="flex items-center gap-2 min-w-0">
                    <span className="text-sm">{prov.icon}</span>
                    <div className="min-w-0">
                      <div className="font-bold text-stone-950 truncate text-xs">{m.display_name || m.model_id}</div>
                      <div className="text-[10px] font-mono text-stone-400 truncate leading-none">{m.model_id}</div>
                    </div>
                  </div>

                  <div className="flex items-center gap-1 shrink-0">
                    <span className={`text-[9px] font-mono font-bold px-1.5 py-0.5 rounded border ${prov.bg}`}>
                      {prov.name}
                    </span>
                    {isExhausted ? (
                      <span className="text-[9px] font-mono font-bold px-1.5 py-0.5 rounded border bg-rose-50 text-rose-800 border-rose-300">
                        ⛔ EXHAUSTED
                      </span>
                    ) : (
                      <span className={`text-[9px] font-mono font-bold px-1.5 py-0.5 rounded border ${TIER_META[m.tier as keyof typeof TIER_META]?.badge || "bg-sand-100 text-stone-700"}`}>
                        {m.tier.toUpperCase()}
                      </span>
                    )}
                  </div>
                </div>

                {/* Quota Exhausted Warning Banner */}
                {isExhausted ? (
                  <div className="p-2 rounded-xl bg-rose-50 border border-rose-200 text-rose-800 flex items-start gap-2 text-[11px] font-medium leading-tight">
                    <AlertTriangle className="w-4 h-4 text-rose-600 shrink-0 mt-0.5" />
                    <span>Monthly {status.tierLabel} quota is exhausted. Switch to an Economy model or connect your own provider key.</span>
                  </div>
                ) : status.isWarning ? (
                  <div className="p-2 rounded-xl bg-amber-50 border border-amber-200 text-amber-800 flex items-center gap-2 text-[11px] leading-tight">
                    <AlertCircle className="w-3.5 h-3.5 text-amber-600 shrink-0" />
                    <span>{status.hint}</span>
                  </div>
                ) : null}

                {/* Description */}
                {m.description ? (
                  <p className="text-[11px] text-stone-600 leading-snug line-clamp-2 bg-white/90 p-2 rounded-xl border border-sand-200">
                    {m.description}
                  </p>
                ) : (
                  <p className="text-[11px] text-stone-500 italic bg-white/90 p-2 rounded-xl border border-sand-200">
                    {isArchitect
                      ? "Calibrated for deep planning, architectural reasoning, and task decomposition."
                      : "Tuned for low-latency code generation, test fixing, and pinpoint diff verification."}
                  </p>
                )}

                {/* Capabilities & Token Economics Grid */}
                <div className="grid grid-cols-2 gap-2">
                  <div className="p-1.5 rounded-xl bg-white border border-sand-200 space-y-0.5">
                    <div className="text-[9px] text-stone-400 font-mono flex items-center gap-1">
                      <Layers className="w-3 h-3 text-indigo-500" />
                      <span>Context Window</span>
                    </div>
                    <div className="font-mono font-bold text-stone-900 text-xs">
                      {m.context_length ? `${Math.round(m.context_length / 1000).toLocaleString()}k Tokens` : "Standard Limit"}
                    </div>
                  </div>

                  <div className="p-1.5 rounded-xl bg-white border border-sand-200 space-y-0.5">
                    <div className="text-[9px] text-stone-400 font-mono flex items-center gap-1">
                      <DollarSign className="w-3 h-3 text-emerald-600" />
                      <span>Token Rate</span>
                    </div>
                    <div className="font-mono font-bold text-stone-900 text-xs truncate">
                      {cost || "Subscription Rate"}
                    </div>
                  </div>
                </div>

                {/* Role recommendation banner */}
                <div className="flex items-center justify-between text-[10px] font-mono text-stone-500">
                  <span className="flex items-center gap-1">
                    <Sparkles className="w-3 h-3 text-amber-500" />
                    <span className="truncate">{isArchitect ? "Role: Strategy & Plan Synthesis" : "Role: Code Generation"}</span>
                  </span>
                  {m.kiwi_provided && (
                    <span className="font-bold text-emerald-700 bg-emerald-50 border border-emerald-200 px-1 py-0.2 rounded text-[9px]">
                      ✨ Kiwi-Funded
                    </span>
                  )}
                </div>
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
  const [allowance, setAllowance] = useState<AllowanceBucket[]>([]);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    Promise.all([
      api.listCatalogModels().catch(() => ({ models: [] })),
      api.getSpend().catch(() => null),
    ])
      .then(([modelsRes, spendRes]) => {
        setModels((modelsRes.models || []).filter((m) => m.selectable));
        setAllowance(spendRes?.allowance || []);
      })
      .finally(() => setLoading(false));
  }, []);

  return (
    <div className="relative z-30 p-5 rounded-3xl bg-white/90 backdrop-blur-xl border border-sand-200 shadow-2xs space-y-4">
      <div className="flex items-center justify-between border-b border-sand-150 pb-2.5">
        <div className="flex items-center gap-2">
          <Cpu className="w-4 h-4 text-indigo-600" />
          <span className="text-xs font-bold text-stone-900 uppercase tracking-wider">Dual AI Model Engine</span>
        </div>
        <span className="text-[11px] text-stone-400 font-mono">Specialized Architect &amp; Worker Pair</span>
      </div>

      <div className="grid grid-cols-1 sm:grid-cols-2 gap-5">
        <div className="relative z-20">
          <ModelPicker
            role="architect"
            value={architectModel}
            onChange={onArchitectChange}
            models={models}
            allowance={allowance}
            loading={loading}
          />
        </div>
        <div className="relative z-10">
          <ModelPicker
            role="worker"
            value={workerModel}
            onChange={onWorkerChange}
            models={models}
            allowance={allowance}
            loading={loading}
          />
        </div>
      </div>
    </div>
  );
}
