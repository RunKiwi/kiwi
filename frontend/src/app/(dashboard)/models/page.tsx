"use client";

import React, { useEffect, useState, useMemo } from "react";
import {
  client,
  providerLabel,
  modelClassLabel,
  planLabel,
  formatTokens,
  type ModelEntry,
  type ProviderInfo,
  type CatalogModel,
  type SpendResponse,
} from "@/lib/api";
import {
  Cpu,
  Plus,
  Trash2,
  AlertCircle,
  Search,
  Key,
  Zap,
  CheckCircle2,
  X,
  Server,
} from "lucide-react";
import { KiwiMicroButtonLoader } from "@/components/KiwiLoaders";
import Link from "next/link";

function nextResetLabel(period: string): string {
  const [y, m] = period.split("-").map(Number);
  if (!y || !m) return "next month";
  const d = new Date(Date.UTC(m === 12 ? y + 1 : y, m === 12 ? 0 : m, 1));
  return d.toLocaleDateString(undefined, { month: "short", day: "numeric" });
}

function priceLabel(m: CatalogModel): string {
  const i = m.input_cost_per_m;
  const o = m.output_cost_per_m;
  if (i == null || o == null) return "Pricing varies";
  if (i === 0 && o === 0) return "Free";
  return `$${i}/M in · $${o}/M out`;
}

function formatContextLength(len: number | null): string {
  if (!len) return "";
  if (len >= 1_000_000) return `${(len / 1_000_000).toFixed(0)}M ctx`;
  if (len >= 1_000) return `${(len / 1_000).toFixed(0)}K ctx`;
  return `${len} ctx`;
}

const PROVIDER_COLORS: Record<string, { bg: string; text: string; border: string }> = {
  anthropic: { bg: "bg-amber-50", text: "text-amber-800", border: "border-amber-200" },
  gemini: { bg: "bg-sky-50", text: "text-sky-800", border: "border-sky-200" },
  google: { bg: "bg-sky-50", text: "text-sky-800", border: "border-sky-200" },
  openai: { bg: "bg-emerald-50", text: "text-emerald-800", border: "border-emerald-200" },
  custom: { bg: "bg-stone-100", text: "text-stone-800", border: "border-sand-300" },
};

const TIER_BADGES: Record<string, { label: string; bg: string; text: string; border: string; dot: string; activeBg: string }> = {
  frontier: {
    label: "FRONTIER",
    bg: "bg-purple-100/90",
    text: "text-purple-900",
    border: "border-purple-300",
    dot: "bg-purple-600",
    activeBg: "bg-purple-900 text-white",
  },
  economy: {
    label: "ECONOMY",
    bg: "bg-amber-100/90",
    text: "text-amber-900",
    border: "border-amber-300",
    dot: "bg-amber-600",
    activeBg: "bg-amber-800 text-white",
  },
  free: {
    label: "FREE TIER",
    bg: "bg-emerald-100/90",
    text: "text-emerald-900",
    border: "border-emerald-300",
    dot: "bg-emerald-600",
    activeBg: "bg-emerald-800 text-white",
  },
  unknown: {
    label: "STANDARD",
    bg: "bg-sand-150",
    text: "text-stone-800",
    border: "border-sand-300",
    dot: "bg-stone-500",
    activeBg: "bg-stone-800 text-white",
  },
};

export default function ModelsPage() {
  const [models, setModels] = useState<ModelEntry[]>([]);
  const [providers, setProviders] = useState<ProviderInfo[]>([]);
  const [catalog, setCatalog] = useState<CatalogModel[]>([]);
  const [spend, setSpend] = useState<SpendResponse | null>(null);
  const [loading, setLoading] = useState(true);

  // Search & Filters
  const [searchQuery, setSearchQuery] = useState("");
  const [selectedProvider, setSelectedProvider] = useState<string>("all");
  const [selectedTier, setSelectedTier] = useState<string>("all");

  // Custom Model Modal
  const [showAddModal, setShowAddModal] = useState(false);
  const [name, setName] = useState("");
  const [provider, setProvider] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

  const load = () => {
    Promise.all([
      client.listModels().then((r) => setModels(r.models)).catch(() => []),
      client.listProviders().then((r) => setProviders(r.providers)).catch(() => []),
      client.listCatalogModels().then((r) => setCatalog(r.models)).catch(() => []),
      (() => {
        const to = new Date().toISOString();
        const from = new Date(Date.now() - 30 * 24 * 3600 * 1000).toISOString();
        return client.getSpend(from, to, "kiwi").then((r) => setSpend(r)).catch(() => null);
      })(),
    ]).finally(() => setLoading(false));
  };

  useEffect(() => {
    load();
  }, []);

  const connectedProvidersSet = useMemo(() => {
    return new Set(providers.filter((p) => p.connected).map((p) => p.id));
  }, [providers]);

  const isProviderConnected = (prov: string) => {
    if (prov === "google") return connectedProvidersSet.has("gemini") || connectedProvidersSet.has("google");
    return connectedProvidersSet.has(prov);
  };

  const handleAddModel = async (e: React.FormEvent) => {
    e.preventDefault();
    setError("");
    if (!name.trim()) {
      setError("Model ID is required (e.g. gemini-2.5-flash or meta-llama/llama-3.3-70b-instruct).");
      return;
    }
    setBusy(true);
    try {
      await client.createModel(name.trim(), provider.trim());
      setName("");
      setProvider("");
      setShowAddModal(false);
      await load();
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to register model");
    } finally {
      setBusy(false);
    }
  };

  const handleRemoveModel = async (id: string) => {
    if (!confirm("Are you sure you want to remove this custom model?")) return;
    try {
      await client.deleteModel(id);
      await load();
    } catch (err) {
      alert("Failed to remove model: " + (err instanceof Error ? err.message : String(err)));
    }
  };

  // Filter models
  const filteredCatalog = useMemo(() => {
    const q = searchQuery.trim().toLowerCase();
    return catalog.filter((m) => {
      const matchesQuery =
        !q ||
        m.display_name.toLowerCase().includes(q) ||
        m.model_id.toLowerCase().includes(q) ||
        (m.description && m.description.toLowerCase().includes(q)) ||
        m.provider.toLowerCase().includes(q);

      const matchesProvider =
        selectedProvider === "all" ||
        m.provider === selectedProvider ||
        (selectedProvider === "gemini" && m.provider === "google");

      const matchesTier = selectedTier === "all" || m.tier === selectedTier;

      return matchesQuery && matchesProvider && matchesTier;
    });
  }, [catalog, searchQuery, selectedProvider, selectedTier]);

  const filteredCustomModels = useMemo(() => {
    const q = searchQuery.trim().toLowerCase();
    return models.filter((m) => {
      const matchesQuery =
        !q ||
        m.name.toLowerCase().includes(q) ||
        (m.provider && m.provider.toLowerCase().includes(q));
      const matchesProvider =
        selectedProvider === "all" ||
        selectedProvider === "custom" ||
        m.provider === selectedProvider;
      return matchesQuery && matchesProvider;
    });
  }, [models, searchQuery, selectedProvider]);

  const totalModelsCount = catalog.length + models.length;
  const connectedCount = providers.filter((p) => p.connected).length;

  if (loading && catalog.length === 0 && models.length === 0) {
    return (
      <div className="p-12 text-stone-500 flex flex-col items-center justify-center gap-3">
        <KiwiMicroButtonLoader />
        <span className="text-xs font-mono">Loading model catalog...</span>
      </div>
    );
  }

  return (
    <div className="max-w-6xl mx-auto flex flex-col gap-6 w-full font-sans text-stone-900">
      {/* ================= HERO HEADER ================= */}
      <div className="flex flex-wrap items-start justify-between gap-4 pb-2 border-b border-sand-200">
        <div>
          <h1 className="text-2xl font-bold tracking-tight text-stone-900 flex items-center gap-2.5">
            <span>Model Catalog</span>
            <span className="text-xs font-mono font-bold bg-sand-100 text-stone-600 border border-sand-200 px-2 py-0.5 rounded-md">
              {totalModelsCount} Models
            </span>
          </h1>
          <p className="text-xs text-stone-500 mt-1 max-w-2xl leading-relaxed">
            Foundation models available for agent execution, planning, and review. Built-in models draw from your platform quota; connect your own keys for unlimited throughput.
          </p>
        </div>

        <div className="flex items-center gap-2.5">
          <Link
            href="/integrations"
            className="px-3.5 py-2 rounded-xl bg-white hover:bg-sand-100 border border-sand-200 text-stone-700 font-semibold text-xs shadow-2xs flex items-center gap-1.5 transition-all"
          >
            <Key className="w-3.5 h-3.5 text-stone-500" />
            <span>Manage API Keys</span>
          </Link>

          <button
            onClick={() => {
              setShowAddModal(true);
              setError("");
            }}
            className="px-4 py-2 rounded-xl bg-stone-900 hover:bg-stone-800 text-white font-semibold text-xs shadow-2xs flex items-center gap-1.5 transition-all active:scale-[0.98]"
          >
            <Plus className="w-3.5 h-3.5 text-kiwi-400 stroke-[2.5]" />
            <span>+ Add Model</span>
          </button>
        </div>
      </div>

      {/* ================= 4 KPI METRIC TILES ================= */}
      <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-3.5">
        <div className="p-4 rounded-2xl bg-white border border-sand-200 shadow-2xs flex flex-col justify-between">
          <div className="flex items-center justify-between text-xs text-stone-500 font-medium">
            <span>Catalog Directory</span>
            <Cpu className="w-4 h-4 text-stone-400" />
          </div>
          <div className="mt-2">
            <div className="text-2xl font-bold font-mono text-stone-900">{totalModelsCount}</div>
            <div className="text-[10px] text-stone-400 font-mono mt-0.5">
              {catalog.length} catalog • {models.length} custom endpoints
            </div>
          </div>
        </div>

        <div className="p-4 rounded-2xl bg-white border border-sand-200 shadow-2xs flex flex-col justify-between">
          <div className="flex items-center justify-between text-xs text-stone-500 font-medium">
            <span>Connected Providers</span>
            <Key className="w-4 h-4 text-emerald-500" />
          </div>
          <div className="mt-2">
            <div className="text-2xl font-bold font-mono text-emerald-950">
              {connectedCount} / {providers.length || 3}
            </div>
            <div className="text-[10px] text-stone-400 font-mono mt-0.5">
              {connectedCount > 0 ? "BYOK Direct Routing Active" : "No custom keys connected"}
            </div>
          </div>
        </div>

        <div className="p-4 rounded-2xl bg-white border border-sand-200 shadow-2xs flex flex-col justify-between">
          <div className="flex items-center justify-between text-xs text-stone-500 font-medium">
            <span>Platform Token Budget</span>
            <Zap className="w-4 h-4 text-amber-500" />
          </div>
          <div className="mt-2">
            <div className="text-2xl font-bold font-mono text-stone-900">
              {spend?.plan ? spend.plan.toUpperCase() : "FREE"}
            </div>
            <div className="text-[10px] text-stone-400 font-mono mt-0.5">
              {spend?.allowance && spend.allowance[0]
                ? `Resets ${nextResetLabel(spend.allowance[0].period)}`
                : "Monthly renewal"}
            </div>
          </div>
        </div>

        <div className="p-4 rounded-2xl bg-white border border-sand-200 shadow-2xs flex flex-col justify-between">
          <div className="flex items-center justify-between text-xs text-stone-500 font-medium">
            <span>Custom Deployments</span>
            <Server className="w-4 h-4 text-indigo-500" />
          </div>
          <div className="mt-2">
            <div className="text-2xl font-bold font-mono text-stone-900">{models.length}</div>
            <div className="text-[10px] text-stone-400 font-mono mt-0.5">
              {models.length > 0 ? "Custom models configured" : "No custom models added"}
            </div>
          </div>
        </div>
      </div>

      {/* ================= MONTHLY TOKEN ALLOWANCE CARD ================= */}
      {spend && !spend.allowance_stale && spend.allowance && spend.allowance.length > 0 && (
        <div className="p-5 rounded-2xl bg-white border border-sand-200 shadow-2xs space-y-4">
          <div className="flex items-start justify-between gap-4">
            <div>
              <h3 className="text-xs font-bold uppercase tracking-wider text-stone-900 flex items-center gap-2">
                <Zap className="w-4 h-4 text-kiwi-600" />
                <span>Monthly Platform Token Allowances</span>
                {spend.plan && (
                  <span className="font-mono text-[10px] bg-sand-100 text-stone-700 px-2 py-0.5 rounded-md border border-sand-200">
                    {planLabel(spend.plan)}
                  </span>
                )}
              </h3>
              <p className="text-xs text-stone-500 mt-1">
                Platform-funded tokens allocated to your workspace. Models executed with your own connected provider keys draw zero platform tokens.
              </p>
            </div>
          </div>

          <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4 pt-1">
            {spend.allowance.map((a) => {
              const unlimited = a.granted < 0;
              const exhausted = !unlimited && a.remaining <= 0;
              const pct = unlimited ? 0 : Math.min(100, (a.used / Math.max(a.granted, 1)) * 100);

              return (
                <div key={a.tier} className="p-3.5 rounded-xl bg-sand-50/70 border border-sand-200 flex flex-col justify-between space-y-2">
                  <div className="flex items-center justify-between">
                    <div className="flex items-center gap-1.5">
                      <span className={`w-2 h-2 rounded-full ${a.tier === "economy" ? "bg-amber-500" : a.tier === "frontier" ? "bg-purple-600" : "bg-emerald-500"}`} />
                      <span className="text-xs font-bold text-stone-800">{modelClassLabel(a.tier)}</span>
                    </div>
                    <span className={`text-xs font-mono font-bold ${exhausted ? "text-rose-600" : "text-stone-900"}`}>
                      {unlimited ? (
                        "Unlimited"
                      ) : exhausted ? (
                        "Exhausted"
                      ) : (
                        `${formatTokens(a.remaining)} left`
                      )}
                    </span>
                  </div>

                  <div className="text-[10px] text-stone-400 font-mono">
                    {unlimited ? "Unlimited runs" : `${formatTokens(a.used)} / ${formatTokens(a.granted)} used`}
                  </div>

                  <div className="w-full bg-sand-200 rounded-full h-1.5 overflow-hidden">
                    <div
                      className={`h-full transition-all duration-300 ${
                        exhausted ? "bg-rose-500" : pct > 80 ? "bg-amber-500" : a.tier === "frontier" ? "bg-purple-600" : a.tier === "economy" ? "bg-amber-500" : "bg-emerald-500"
                      }`}
                      style={{ width: `${unlimited ? 100 : pct}%` }}
                    />
                  </div>
                </div>
              );
            })}
          </div>
        </div>
      )}

      {/* ================= SEARCH & FILTER TOOLBAR ================= */}
      <div className="flex flex-wrap items-center justify-between gap-3 bg-white p-3 rounded-2xl border border-sand-200 shadow-2xs text-xs">
        <div className="flex items-center gap-2.5 flex-1 min-w-[220px]">
          <div className="relative flex-1">
            <Search className="w-3.5 h-3.5 text-stone-400 absolute left-3 top-2.5 pointer-events-none" />
            <input
              type="text"
              value={searchQuery}
              onChange={(e) => setSearchQuery(e.target.value)}
              placeholder="Search by model name, ID, or provider..."
              className="w-full bg-sand-50/70 border border-sand-200 rounded-xl pl-8 pr-3 py-1.5 text-xs placeholder:text-stone-400 focus:outline-none focus:border-stone-400 focus:bg-white transition-all font-mono"
            />
          </div>
        </div>

        {/* Filters */}
        <div className="flex items-center gap-2 flex-wrap">
          {/* Provider Filter */}
          <div className="flex items-center gap-1 bg-sand-100 p-0.5 rounded-xl border border-sand-200">
            <button
              onClick={() => setSelectedProvider("all")}
              className={`px-2.5 py-1 rounded-lg text-xs font-semibold transition-all ${
                selectedProvider === "all" ? "bg-white text-stone-900 shadow-2xs" : "text-stone-600 hover:text-stone-900"
              }`}
            >
              All Providers
            </button>
            <button
              onClick={() => setSelectedProvider("anthropic")}
              className={`px-2.5 py-1 rounded-lg text-xs font-semibold transition-all ${
                selectedProvider === "anthropic" ? "bg-white text-stone-900 shadow-2xs" : "text-stone-600 hover:text-stone-900"
              }`}
            >
              Anthropic
            </button>
            <button
              onClick={() => setSelectedProvider("gemini")}
              className={`px-2.5 py-1 rounded-lg text-xs font-semibold transition-all ${
                selectedProvider === "gemini" ? "bg-white text-stone-900 shadow-2xs" : "text-stone-600 hover:text-stone-900"
              }`}
            >
              Gemini
            </button>
            <button
              onClick={() => setSelectedProvider("openai")}
              className={`px-2.5 py-1 rounded-lg text-xs font-semibold transition-all ${
                selectedProvider === "openai" ? "bg-white text-stone-900 shadow-2xs" : "text-stone-600 hover:text-stone-900"
              }`}
            >
              OpenAI
            </button>
            {models.length > 0 && (
              <button
                onClick={() => setSelectedProvider("custom")}
                className={`px-2.5 py-1 rounded-lg text-xs font-semibold transition-all ${
                  selectedProvider === "custom" ? "bg-white text-stone-900 shadow-2xs" : "text-stone-600 hover:text-stone-900"
                }`}
              >
                Custom ({models.length})
              </button>
            )}
          </div>

          {/* Tier Filter with distinctive color pills */}
          <div className="flex items-center gap-1 bg-sand-100 p-0.5 rounded-xl border border-sand-200">
            <button
              onClick={() => setSelectedTier("all")}
              className={`px-2.5 py-1 rounded-lg text-xs font-semibold transition-all ${
                selectedTier === "all" ? "bg-white text-stone-900 shadow-2xs" : "text-stone-600 hover:text-stone-900"
              }`}
            >
              All Tiers
            </button>
            <button
              onClick={() => setSelectedTier("frontier")}
              className={`px-2.5 py-1 rounded-lg text-xs font-semibold transition-all flex items-center gap-1.5 ${
                selectedTier === "frontier"
                  ? "bg-purple-900 text-white shadow-2xs"
                  : "text-purple-900 hover:bg-purple-100/80"
              }`}
            >
              <span className={`w-1.5 h-1.5 rounded-full ${selectedTier === "frontier" ? "bg-purple-300" : "bg-purple-600"}`} />
              <span>Frontier</span>
            </button>
            <button
              onClick={() => setSelectedTier("economy")}
              className={`px-2.5 py-1 rounded-lg text-xs font-semibold transition-all flex items-center gap-1.5 ${
                selectedTier === "economy"
                  ? "bg-amber-900 text-white shadow-2xs"
                  : "text-amber-900 hover:bg-amber-100/80"
              }`}
            >
              <span className={`w-1.5 h-1.5 rounded-full ${selectedTier === "economy" ? "bg-amber-300" : "bg-amber-600"}`} />
              <span>Economy</span>
            </button>
            <button
              onClick={() => setSelectedTier("free")}
              className={`px-2.5 py-1 rounded-lg text-xs font-semibold transition-all flex items-center gap-1.5 ${
                selectedTier === "free"
                  ? "bg-emerald-900 text-white shadow-2xs"
                  : "text-emerald-900 hover:bg-emerald-100/80"
              }`}
            >
              <span className={`w-1.5 h-1.5 rounded-full ${selectedTier === "free" ? "bg-emerald-300" : "bg-emerald-600"}`} />
              <span>Free</span>
            </button>
          </div>

          <span className="text-stone-400 font-mono text-[11px] hidden lg:inline">
            {filteredCatalog.length + filteredCustomModels.length} models
          </span>
        </div>
      </div>

      {/* ================= CUSTOM REGISTERED MODELS SECTION ================= */}
      {filteredCustomModels.length > 0 && (
        <div className="space-y-3">
          <div className="flex items-center justify-between">
            <h3 className="text-xs font-bold text-stone-900 uppercase tracking-wider flex items-center gap-2">
              <Server className="w-3.5 h-3.5 text-indigo-600" />
              <span>Custom Registered Endpoints ({filteredCustomModels.length})</span>
            </h3>
          </div>

          <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-3.5">
            {filteredCustomModels.map((m) => (
              <div
                key={m.id}
                className="p-4 rounded-2xl bg-white border border-sand-200 shadow-2xs flex flex-col justify-between space-y-3 hover:border-sand-300 transition-all group"
              >
                <div className="flex items-start justify-between gap-2">
                  <div className="min-w-0 space-y-1">
                    <div className="flex items-center gap-1.5 flex-wrap">
                      <span className="font-mono text-xs font-bold text-stone-900 truncate">{m.name}</span>
                    </div>
                    <div className="text-[11px] text-stone-500 font-mono">
                      Provider: <span className="font-semibold text-stone-700">{m.provider || "auto-detect"}</span>
                    </div>
                  </div>

                  <button
                    onClick={() => handleRemoveModel(m.id)}
                    className="p-1.5 text-stone-400 hover:text-rose-600 hover:bg-rose-50 rounded-lg transition-colors"
                    title="Remove custom model"
                  >
                    <Trash2 className="w-3.5 h-3.5" />
                  </button>
                </div>

                <div className="pt-2 border-t border-sand-150 flex items-center justify-between text-[10px] font-mono text-stone-400">
                  <span className="flex items-center gap-1 text-emerald-700 bg-emerald-50 px-2 py-0.5 rounded border border-emerald-200 font-bold">
                    <CheckCircle2 className="w-3 h-3 text-emerald-600" />
                    <span>Registered</span>
                  </span>
                  <span>Direct BYOK</span>
                </div>
              </div>
            ))}
          </div>
        </div>
      )}

      {/* ================= CATALOG DIRECTORY GRID ================= */}
      <div className="space-y-3">
        <div className="flex items-center justify-between">
          <h3 className="text-xs font-bold text-stone-900 uppercase tracking-wider flex items-center gap-2">
            <Cpu className="w-3.5 h-3.5 text-stone-600" />
            <span>Available Catalog ({filteredCatalog.length})</span>
          </h3>
        </div>

        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-3.5">
          {filteredCatalog.map((m) => {
            const hasConnectedKey = isProviderConnected(m.provider);
            const provColor = PROVIDER_COLORS[m.provider] || PROVIDER_COLORS.custom;
            const contextText = formatContextLength(m.context_length);

            return (
              <div
                key={m.model_id}
                className="p-4 rounded-2xl bg-white border border-sand-200 shadow-2xs flex flex-col justify-between space-y-3 hover:border-sand-300 hover:shadow-island-hover transition-all"
              >
                <div className="space-y-2">
                  {/* Top tags */}
                  <div className="flex items-center justify-between gap-2">
                    <span
                      className={`px-2 py-0.5 rounded-md text-[10px] font-mono font-bold uppercase border ${provColor.bg} ${provColor.text} ${provColor.border}`}
                    >
                      {providerLabel(m.provider)}
                    </span>

                    {(() => {
                      const tierInfo = TIER_BADGES[m.tier] || TIER_BADGES.unknown;
                      return (
                        <span
                          className={`inline-flex items-center gap-1.5 px-2 py-0.5 rounded-md text-[10px] font-mono font-bold uppercase border ${tierInfo.bg} ${tierInfo.text} ${tierInfo.border} shadow-2xs`}
                        >
                          <span className={`w-1.5 h-1.5 rounded-full ${tierInfo.dot}`} />
                          <span>{tierInfo.label}</span>
                        </span>
                      );
                    })()}
                  </div>

                  {/* Name & ID */}
                  <div>
                    <h4 className="text-sm font-bold text-stone-900 leading-snug">{m.display_name}</h4>
                    <div className="text-[11px] font-mono text-stone-400 truncate mt-0.5 select-all" title={m.model_id}>
                      {m.model_id}
                    </div>
                  </div>

                  {/* Description */}
                  {m.description && (
                    <p className="text-xs text-stone-500 line-clamp-2 leading-relaxed">{m.description}</p>
                  )}
                </div>

                {/* Bottom Specs & Status */}
                <div className="space-y-2 pt-2 border-t border-sand-150">
                  <div className="flex items-center justify-between text-[11px] font-mono text-stone-500">
                    <span>{priceLabel(m)}</span>
                    {contextText && <span>{contextText}</span>}
                  </div>

                  <div className="flex items-center justify-between pt-1">
                    {m.kiwi_provided ? (
                      <span className="inline-flex items-center gap-1 text-[10px] font-mono font-bold text-emerald-800 bg-emerald-50 px-2 py-0.5 rounded-md border border-emerald-200">
                        <Zap className="w-3 h-3 text-emerald-600 fill-current" />
                        <span>Platform Included</span>
                      </span>
                    ) : hasConnectedKey ? (
                      <span className="inline-flex items-center gap-1 text-[10px] font-mono font-bold text-indigo-800 bg-indigo-50 px-2 py-0.5 rounded-md border border-indigo-200">
                        <CheckCircle2 className="w-3 h-3 text-indigo-600" />
                        <span>BYOK Connected</span>
                      </span>
                    ) : (
                      <Link
                        href="/integrations"
                        className="inline-flex items-center gap-1 text-[10px] font-mono font-bold text-amber-800 bg-amber-50 hover:bg-amber-100 px-2 py-0.5 rounded-md border border-amber-200 transition-colors"
                      >
                        <Key className="w-3 h-3 text-amber-600" />
                        <span>Connect {providerLabel(m.provider)} Key &rarr;</span>
                      </Link>
                    )}

                    {m.supports_tools && (
                      <span className="text-[9px] font-mono font-semibold text-stone-400 bg-sand-100 px-1.5 py-0.5 rounded">
                        Tools
                      </span>
                    )}
                  </div>
                </div>
              </div>
            );
          })}
        </div>

        {filteredCatalog.length === 0 && filteredCustomModels.length === 0 && (
          <div className="p-12 text-center text-stone-400 font-mono bg-white border border-sand-200 rounded-2xl shadow-2xs space-y-2">
            <Cpu className="w-8 h-8 text-stone-300 mx-auto" />
            <p>No models matching your search criteria.</p>
          </div>
        )}
      </div>

      {/* ================= MODAL: REGISTER CUSTOM MODEL ================= */}
      {showAddModal && (
        <div className="fixed inset-0 bg-stone-900/40 backdrop-blur-xs flex items-center justify-center p-4 z-50 animate-fadeIn">
          <div className="bg-white border border-sand-200 rounded-2xl max-w-md w-full p-6 shadow-popover space-y-4">
            <div className="flex items-center justify-between pb-3 border-b border-sand-200">
              <div className="flex items-center gap-2">
                <Cpu className="w-4 h-4 text-stone-700" />
                <h3 className="text-sm font-bold text-stone-900">Register Custom Model</h3>
              </div>
              <button
                onClick={() => {
                  setShowAddModal(false);
                  setError("");
                }}
                className="p-1 text-stone-400 hover:text-stone-700 rounded-lg"
              >
                <X className="w-4 h-4" />
              </button>
            </div>

            {error && (
              <div className="p-3 rounded-xl bg-rose-50 border border-rose-200 text-rose-800 text-xs flex items-center gap-2">
                <AlertCircle className="w-4 h-4 text-rose-600 shrink-0" />
                <span>{error}</span>
              </div>
            )}

            <form onSubmit={handleAddModel} className="space-y-3.5 text-xs">
              <div>
                <label className="block font-bold text-stone-700 mb-1">Model Identifier (API ID)</label>
                <input
                  type="text"
                  value={name}
                  onChange={(e) => setName(e.target.value)}
                  placeholder="e.g. meta-llama/llama-3.3-70b-instruct or gpt-4o-mini"
                  required
                  className="w-full bg-sand-50 border border-sand-200 rounded-xl px-3 py-2 text-xs font-mono focus:outline-none focus:border-stone-800"
                />
                <p className="text-[10px] text-stone-400 mt-1">
                  The exact identifier recognized by your connected provider or custom proxy.
                </p>
              </div>

              <div>
                <label className="block font-bold text-stone-700 mb-1">Provider Routing</label>
                <select
                  value={provider}
                  onChange={(e) => setProvider(e.target.value)}
                  className="w-full bg-sand-50 border border-sand-200 rounded-xl px-3 py-2 text-xs font-semibold text-stone-900 focus:outline-none focus:border-stone-800"
                >
                  <option value="">Auto-detect Provider</option>
                  <option value="anthropic">Anthropic</option>
                  <option value="openai">OpenAI</option>
                  <option value="gemini">Google Gemini</option>
                </select>
              </div>

              <div className="pt-3 border-t border-sand-200 flex justify-end gap-2">
                <button
                  type="button"
                  onClick={() => setShowAddModal(false)}
                  className="px-3.5 py-2 rounded-xl text-stone-600 hover:bg-sand-100 font-medium text-xs transition-colors"
                >
                  Cancel
                </button>
                <button
                  type="submit"
                  disabled={busy || !name.trim()}
                  className="px-4 py-2 rounded-xl bg-stone-900 hover:bg-stone-800 text-white font-bold text-xs shadow-2xs flex items-center gap-1.5 transition-all disabled:opacity-50"
                >
                  {busy ? <KiwiMicroButtonLoader /> : <Plus className="w-3.5 h-3.5 text-kiwi-400 stroke-[2.5]" />}
                  <span>Register Model</span>
                </button>
              </div>
            </form>
          </div>
        </div>
      )}
    </div>
  );
}
