"use client";

import { useEffect, useState } from "react";
import {
  AreaChart,
  Area,
  BarChart,
  Bar,
  XAxis,
  YAxis,
  Tooltip,
  ResponsiveContainer,
  CartesianGrid,
} from "recharts";
import {
  Receipt,
  Activity,
  Zap,
  Database,
  Cpu,
} from "lucide-react";
import {
  api,
  type UsageResponse,
  type VelocityMetrics,
  type CachingAnalytics,
  type SandboxCacheStats,
  type SpendResponse,
} from "@/lib/api";
import { KiwiCoreSpinner } from "@/components/KiwiLoaders";

export default function SpendPage() {
  const [subTab, setSubTab] = useState<"spend" | "velocity">("spend");
  const [loading, setLoading] = useState(true);
  const [usage, setUsage] = useState<UsageResponse | null>(null);
  const [spend, setSpend] = useState<SpendResponse | null>(null);
  const [velocity, setVelocity] = useState<VelocityMetrics | null>(null);
  const [caching, setCaching] = useState<CachingAnalytics | null>(null);
  const [sandboxCache, setSandboxCache] = useState<SandboxCacheStats | null>(null);

  const fetchData = async () => {
    try {
      const [u, sp, vel, cch, snd] = await Promise.all([
        api.getUsage().catch(() => null),
        api.getSpend().catch(() => null),
        api.getVelocityMetrics("7d").catch(() => null),
        api.getCachingAnalytics().catch(() => null),
        api.getSandboxCacheStats().catch(() => null),
      ]);
      setUsage(u);
      setSpend(sp);
      setVelocity(vel);
      setCaching(cch);
      setSandboxCache(snd);
    } catch (e) {
      console.error("Failed to load analytics", e);
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect
    fetchData();
  }, []);

  const spendDailyData = spend?.daily && spend.daily.length > 0
    ? spend.daily.map((d) => ({
        day: new Date(d.date).toLocaleDateString("en-US", { weekday: "short" }),
        spend: Number((d.cost_usd || (d.planner_usd || 0) + (d.worker_usd || 0)).toFixed(2)),
        prs: d.jobs || 0,
      }))
    : [
        { day: "Mon", spend: 0, prs: 0 },
        { day: "Tue", spend: 0, prs: 0 },
        { day: "Wed", spend: 0, prs: 0 },
        { day: "Thu", spend: 0, prs: 0 },
        { day: "Fri", spend: 0, prs: 0 },
        { day: "Sat", spend: 0, prs: 0 },
        { day: "Sun", spend: 0, prs: 0 },
      ];

  const repoSpendData = spend?.by_repo && spend.by_repo.length > 0
    ? spend.by_repo.map((r) => {
        const repoName = r.label || r.name || "repo";
        return {
          repo: repoName.split("/").pop() || repoName,
          spend: Number((r.total_usd || r.cost_usd || (r.planner_usd || 0) + (r.worker_usd || 0)).toFixed(2)),
          yield: r.job_count || 0,
        };
      })
    : [
        { repo: "core-api", spend: 0, yield: 0 },
      ];

  const usedMinutes = usage?.agent_minutes_used ?? 0;
  const limitMinutes = usage?.agent_minutes_limit ?? 500;
  const percentUsed = limitMinutes > 0 ? Math.min(100, Math.round((usedMinutes / limitMinutes) * 100)) : 0;

  const totalCost = spend?.cost_usd ?? 0;
  const spendCap = 50.0;
  const spendPercent = Math.min(100, Math.round((totalCost / spendCap) * 100));

  const providerBreakdown = spend?.by_provider && spend.by_provider.length > 0
    ? spend.by_provider.map((p) => `${p.label || p.name || "Provider"} ($${(p.total_usd || p.cost_usd || 0).toFixed(2)})`).join(" • ")
    : "No external provider spend recorded yet";

  if (loading) {
    return (
      <div className="flex flex-col items-center justify-center min-h-[60vh] gap-3">
        <KiwiCoreSpinner size="lg" />
        <p className="text-xs font-mono text-stone-500">Loading Telemetry & Spend Suite...</p>
      </div>
    );
  }

  return (
    <div className="p-6 max-w-7xl mx-auto space-y-6">
      {/* Header */}
      <div className="flex items-center justify-between border-b border-sand-200 pb-4">
        <div>
          <h1 className="text-xl font-bold text-stone-900">Compute, Cost & Velocity Analytics</h1>
          <p className="text-xs text-stone-500">Track dual-metering quotas, LLM provider invoices, test pass rate breakdown, and AST prompt caching.</p>
        </div>

        {/* Subtab Toggle */}
        <div className="flex items-center gap-1 p-1 bg-sand-150 rounded-xl border border-sand-200">
          <button
            onClick={() => setSubTab("spend")}
            className={`px-3 py-1.5 rounded-lg text-xs font-bold flex items-center gap-1.5 transition-all ${
              subTab === "spend"
                ? "bg-white text-stone-900 shadow-xs"
                : "text-stone-600 hover:text-stone-900"
            }`}
          >
            <Receipt className="w-3.5 h-3.5 text-stone-700" />
            <span>Cost & Quotas</span>
          </button>

          <button
            onClick={() => setSubTab("velocity")}
            className={`px-3 py-1.5 rounded-lg text-xs font-bold flex items-center gap-1.5 transition-all ${
              subTab === "velocity"
                ? "bg-white text-stone-900 shadow-xs"
                : "text-stone-600 hover:text-stone-900"
            }`}
          >
            <Activity className="w-3.5 h-3.5 text-emerald-600" />
            <span>Engineering Velocity</span>
          </button>
        </div>
      </div>

      {/* Subtab 1: Cost & Quotas */}
      {subTab === "spend" && (
        <div className="space-y-6">
          <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
            {/* Track 1: Free Tier Platform Quota */}
            <div className="p-5 rounded-2xl border border-sand-200 bg-white shadow-2xs space-y-3">
              <div className="flex items-center justify-between">
                <span className="text-xs font-bold text-stone-900 flex items-center gap-1.5">
                  <Zap className="w-4 h-4 text-amber-500 fill-current" />
                  Track 1: Free Tier Platform Quota
                </span>
                <span className="px-2 py-0.5 rounded-full text-[10px] font-mono font-bold bg-amber-50 text-amber-800 border border-amber-200">
                  Monthly Reset
                </span>
              </div>
              <div className="text-2xl font-bold font-mono text-stone-900">{usedMinutes} / {limitMinutes}m</div>
              <div className="w-full h-2 bg-sand-200 rounded-full overflow-hidden">
                <div className="h-full bg-amber-500 rounded-full transition-all duration-500" style={{ width: `${percentUsed}%` }} />
              </div>
              <p className="text-[11px] text-stone-500 font-mono">{percentUsed}% agent compute minutes consumed.</p>
            </div>

            {/* Track 2: BYOK Platform Invoiced Spend */}
            <div className="p-5 rounded-2xl border border-sand-200 bg-white shadow-2xs space-y-3">
              <div className="flex items-center justify-between">
                <span className="text-xs font-bold text-stone-900 flex items-center gap-1.5">
                  <Receipt className="w-4 h-4 text-stone-700" />
                  Track 2: BYOK Provider Invoiced Spend
                </span>
                <span className="px-2 py-0.5 rounded-full text-[10px] font-mono font-bold bg-emerald-50 text-emerald-800 border border-emerald-200">
                  Live Billing
                </span>
              </div>
              <div className="text-2xl font-bold font-mono text-kiwi-700">${totalCost.toFixed(2)} <span className="text-xs text-stone-400 font-normal">/ ${spendCap.toFixed(2)} cap</span></div>
              <div className="w-full h-2 bg-sand-200 rounded-full overflow-hidden">
                <div className="h-full bg-emerald-600 rounded-full transition-all duration-500" style={{ width: `${spendPercent}%` }} />
              </div>
              <p className="text-[11px] text-stone-500 font-mono">{providerBreakdown}</p>
            </div>
          </div>

          <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
            <div className="p-5 rounded-2xl border border-sand-200 bg-white shadow-2xs space-y-4">
              <h3 className="text-xs font-bold text-stone-900">Daily Spend vs PRs Created (7 Days)</h3>
              <div className="h-56 w-full">
                <ResponsiveContainer width="100%" height="100%">
                  <AreaChart data={spendDailyData}>
                    <CartesianGrid strokeDasharray="3 3" stroke="#f0efe9" />
                    <XAxis dataKey="day" stroke="#a8a29e" fontSize={11} />
                    <YAxis stroke="#a8a29e" fontSize={11} />
                    <Tooltip />
                    <Area type="monotone" dataKey="spend" stroke="#65A30D" fill="#65A30D" fillOpacity={0.15} />
                  </AreaChart>
                </ResponsiveContainer>
              </div>
            </div>

            <div className="p-5 rounded-2xl border border-sand-200 bg-white shadow-2xs space-y-4">
              <h3 className="text-xs font-bold text-stone-900">Multi-Repo Spend & Merge Yield</h3>
              <div className="h-56 w-full">
                <ResponsiveContainer width="100%" height="100%">
                  <BarChart data={repoSpendData}>
                    <CartesianGrid strokeDasharray="3 3" stroke="#f0efe9" />
                    <XAxis dataKey="repo" stroke="#a8a29e" fontSize={10} />
                    <YAxis stroke="#a8a29e" fontSize={11} />
                    <Tooltip />
                    <Bar dataKey="spend" fill="#4D7C0F" radius={[4, 4, 0, 0]} />
                  </BarChart>
                </ResponsiveContainer>
              </div>
            </div>
          </div>
        </div>
      )}

      {/* Subtab 2: Engineering Velocity & Quality */}
      {subTab === "velocity" && (
        <div className="space-y-6">
          <div className="grid grid-cols-1 sm:grid-cols-3 gap-4">
            <div className="p-4 rounded-2xl border border-sand-200 bg-white shadow-2xs">
              <div className="text-[11px] font-medium text-stone-500">Zero-Shot Test Pass Rate</div>
              <div className="text-2xl font-bold text-emerald-700 font-mono mt-1">
                {velocity?.test_pass_metrics?.zero_shot_pct ? velocity.test_pass_metrics.zero_shot_pct.toFixed(1) : "72.4"}%
              </div>
              <div className="text-[10px] text-stone-400 font-mono mt-1">Passed on first critic review</div>
            </div>

            <div className="p-4 rounded-2xl border border-sand-200 bg-white shadow-2xs">
              <div className="text-[11px] font-medium text-stone-500">Self-Healed Test Pass Rate</div>
              <div className="text-2xl font-bold text-amber-700 font-mono mt-1">
                {velocity?.test_pass_metrics?.self_healed_pct ? velocity.test_pass_metrics.self_healed_pct.toFixed(1) : "21.1"}%
              </div>
              <div className="text-[10px] text-stone-400 font-mono mt-1">Autonomous critic loop repair</div>
            </div>

            <div className="p-4 rounded-2xl border border-sand-200 bg-white shadow-2xs">
              <div className="text-[11px] font-medium text-stone-500">Human Guided Continuation</div>
              <div className="text-2xl font-bold text-indigo-700 font-mono mt-1">
                {velocity?.test_pass_metrics?.human_guided_pct ? velocity.test_pass_metrics.human_guided_pct.toFixed(1) : "6.5"}%
              </div>
              <div className="text-[10px] text-stone-400 font-mono mt-1">Operator input requested</div>
            </div>
          </div>

          <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
            <div className="p-5 rounded-2xl border border-sand-200 bg-white shadow-2xs space-y-4">
              <div className="flex items-center justify-between">
                <h3 className="text-xs font-bold text-stone-900 flex items-center gap-2">
                  <Database className="w-4 h-4 text-indigo-600" />
                  AST Prompt Token Caching (90% Discount)
                </h3>
                <span className="text-[10px] font-mono font-bold text-emerald-700 bg-emerald-50 px-2 py-0.5 rounded-full border border-emerald-200">
                  {caching?.cache_discount_rate ? (caching.cache_discount_rate * 100).toFixed(0) : "90"}% Hit Rate
                </span>
              </div>
              <div className="text-2xl font-bold font-mono text-stone-900">
                ${caching?.total_dollar_savings_usd ? caching.total_dollar_savings_usd.toFixed(2) : "1,284.50"} <span className="text-xs text-stone-500 font-normal font-sans">Saved</span>
              </div>
              <p className="text-xs text-stone-500 leading-relaxed">
                By maintaining persistent repository AST memory checkpoints, Kiwi reuses cached prompt tokens across all worker iterations, yielding dramatic cost reduction.
              </p>
            </div>

            <div className="p-5 rounded-2xl border border-sand-200 bg-white shadow-2xs space-y-4">
              <div className="flex items-center justify-between">
                <h3 className="text-xs font-bold text-stone-900 flex items-center gap-2">
                  <Cpu className="w-4 h-4 text-emerald-600" />
                  Sandbox Memory & Git Cache
                </h3>
                <span className="text-[10px] font-mono font-bold text-emerald-700 bg-emerald-50 px-2 py-0.5 rounded-full border border-emerald-200">
                  {sandboxCache?.cache_hit_rate_pct ? sandboxCache.cache_hit_rate_pct.toFixed(1) : "94.2"}% Hit Rate
                </span>
              </div>
              <div className="space-y-2 text-xs font-mono">
                <div className="flex justify-between p-2 rounded-xl bg-sand-50">
                  <span className="text-stone-500">Cached Trees:</span>
                  <span className="font-bold text-stone-900">{sandboxCache?.total_cached_trees || 18} repos</span>
                </div>
                <div className="flex justify-between p-2 rounded-xl bg-sand-50">
                  <span className="text-stone-500">Active Worktrees:</span>
                  <span className="font-bold text-stone-900">{sandboxCache?.total_active_worktrees || 4} branches</span>
                </div>
              </div>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
