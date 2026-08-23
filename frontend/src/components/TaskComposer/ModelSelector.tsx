"use client";

import React, { useState } from "react";
import { Search, Compass, Hammer, Check } from "lucide-react";

export interface ModelOption {
  id: string;
  name: string;
  provider: "anthropic" | "openai" | "deepseek" | "google";
  tier: "FREE" | "ECONOMY" | "FRONTIER";
  context: string;
  latency: string;
  description: string;
}

export const AVAILABLE_MODELS: ModelOption[] = [
  {
    id: "claude-3-7-sonnet",
    name: "Claude 3.7 Sonnet (Hybrid Thought)",
    provider: "anthropic",
    tier: "FRONTIER",
    context: "200k",
    latency: "Fast",
    description: "Industry-leading reasoning & code synthesis.",
  },
  {
    id: "claude-3-5-haiku",
    name: "Claude 3.5 Haiku",
    provider: "anthropic",
    tier: "FREE",
    context: "200k",
    latency: "Ultra Fast",
    description: "Lightning-fast, cost-effective implementer.",
  },
  {
    id: "gpt-4.5-preview",
    name: "GPT-4.5 Preview",
    provider: "openai",
    tier: "FRONTIER",
    context: "128k",
    latency: "Moderate",
    description: "Deep architecture planning & debugging.",
  },
  {
    id: "gpt-4o-mini",
    name: "GPT-4o Mini",
    provider: "openai",
    tier: "ECONOMY",
    context: "128k",
    latency: "Ultra Fast",
    description: "High-speed parallel test fix loop.",
  },
  {
    id: "deepseek-v3",
    name: "DeepSeek-V3",
    provider: "deepseek",
    tier: "FREE",
    context: "64k",
    latency: "Fast",
    description: "Exceptional cost efficiency on code generation.",
  },
];

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
  const [activeTab, setActiveTab] = useState<"architect" | "worker">("architect");
  const [search, setSearch] = useState("");

  const filteredModels = AVAILABLE_MODELS.filter(
    (m) =>
      m.name.toLowerCase().includes(search.toLowerCase()) ||
      m.id.toLowerCase().includes(search.toLowerCase()) ||
      m.provider.toLowerCase().includes(search.toLowerCase())
  );

  const selectedId = activeTab === "architect" ? architectModel : workerModel;
  const onSelect = activeTab === "architect" ? onArchitectChange : onWorkerChange;

  return (
    <div className="rounded-2xl border border-sand-200 bg-white p-3 space-y-3 shadow-2xs">
      <div className="grid grid-cols-2 gap-1 p-1 bg-sand-150 rounded-xl">
        <button
          type="button"
          onClick={() => setActiveTab("architect")}
          className={`py-1.5 px-2.5 rounded-lg text-xs font-semibold flex items-center justify-center gap-1.5 transition-all ${
            activeTab === "architect"
              ? "bg-white text-stone-900 shadow-xs"
              : "text-stone-600 hover:text-stone-900"
          }`}
        >
          <Compass className="w-3.5 h-3.5 text-indigo-600" />
          <span>Architect: {AVAILABLE_MODELS.find((m) => m.id === architectModel)?.name.split(" ")[0] || "Sonnet"}</span>
        </button>
        <button
          type="button"
          onClick={() => setActiveTab("worker")}
          className={`py-1.5 px-2.5 rounded-lg text-xs font-semibold flex items-center justify-center gap-1.5 transition-all ${
            activeTab === "worker"
              ? "bg-white text-stone-900 shadow-xs"
              : "text-stone-600 hover:text-stone-900"
          }`}
        >
          <Hammer className="w-3.5 h-3.5 text-emerald-600" />
          <span>Worker: {AVAILABLE_MODELS.find((m) => m.id === workerModel)?.name.split(" ")[0] || "Haiku"}</span>
        </button>
      </div>

      <div className="relative">
        <Search className="w-3.5 h-3.5 text-stone-400 absolute left-2.5 top-2.5" />
        <input
          type="text"
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          placeholder={`Search ${activeTab === "architect" ? "Architect (Planner)" : "Worker (Implementer)"} models...`}
          className="w-full pl-8 pr-3 py-1.5 rounded-xl border border-sand-200 text-xs focus:outline-none focus:ring-1 focus:ring-stone-900"
        />
      </div>

      <div className="max-h-48 overflow-y-auto space-y-1.5 pr-1">
        {filteredModels.map((model) => {
          const isSelected = selectedId === model.id;
          return (
            <div
              key={model.id}
              onClick={() => onSelect(model.id)}
              className={`p-2 rounded-xl border transition-all cursor-pointer flex items-center justify-between gap-2 ${
                isSelected
                  ? "border-stone-900 bg-sand-100/80 shadow-2xs"
                  : "border-sand-200 hover:border-sand-300 hover:bg-sand-50"
              }`}
            >
              <div className="min-w-0">
                <div className="flex items-center gap-1.5">
                  <span className="text-xs font-bold text-stone-900 truncate">{model.name}</span>
                  <span
                    className={`px-1.5 py-0.2 rounded text-[9px] font-mono font-bold ${
                      model.tier === "FRONTIER"
                        ? "bg-indigo-50 text-indigo-800 border border-indigo-200"
                        : model.tier === "FREE"
                        ? "bg-lime-50 text-lime-800 border border-lime-200"
                        : "bg-stone-100 text-stone-700 border border-sand-200"
                    }`}
                  >
                    {model.tier}
                  </span>
                </div>
                <p className="text-[10px] text-stone-500 truncate">{model.description}</p>
              </div>

              <div className="flex items-center gap-2 shrink-0">
                <span className="text-[10px] font-mono text-stone-400">{model.context}</span>
                {isSelected && <Check className="w-4 h-4 text-stone-900" />}
              </div>
            </div>
          );
        })}
      </div>
    </div>
  );
}
