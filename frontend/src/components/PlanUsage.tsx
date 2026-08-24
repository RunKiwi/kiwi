"use client";

import React, { useEffect, useState } from "react";
import { Gauge, AlertCircle, Clock, Zap } from "lucide-react";
import {
  client,
  formatTokens,
  modelClassLabel,
  MODEL_CLASS_BLURB,
  CLASS_ORDER,
  type UsageResponse,
  type SpendResponse,
} from "@/lib/api";
import { UpgradeButton } from "@/components/UpgradeButton";

export function PlanUsage() {
  const [u, setU] = useState<UsageResponse | null>(null);
  const [spend, setSpend] = useState<SpendResponse | null>(null);

  useEffect(() => {
    client.getUsage().then(setU).catch(() => setU(null));
    client.getSpend().then(setSpend).catch(() => setSpend(null));
  }, []);

  if (!u) return null;

  // agent_minutes_limit is always present and 0 means unlimited (see api.ts) —
  // there is no "unset" case to guess a plan-based default for, and guessing
  // one broke any plan (e.g. enterprise) not covered by the guess.
  const resolvedLimit = u.agent_minutes_limit;
  const hasCap = resolvedLimit > 0;
  const pct = hasCap ? Math.min(100, (u.agent_minutes_used / resolvedLimit) * 100) : 0;
  const over = hasCap && u.agent_minutes_used >= resolvedLimit;
  const near = hasCap && !over && pct >= 80;
  const barColor = over ? "bg-rose-500" : near ? "bg-amber-500" : "bg-kiwi-600";
  const usedColor = over ? "text-rose-700" : near ? "text-amber-700" : "text-stone-900";

  return (
    <div className="bg-white border border-sand-200 rounded-2xl shadow-2xs p-5 sm:p-6 space-y-5 font-sans">
      <div className="flex flex-wrap items-start justify-between gap-4">
        <div>
          <div className="flex items-center gap-2">
            <h2 className="text-sm font-bold text-stone-900 uppercase tracking-wider flex items-center gap-2">
              <Gauge className="w-4 h-4 text-stone-600" />
              <span>Current Subscription &amp; Quota</span>
            </h2>
            <span className="inline-flex items-center px-2 py-0.5 rounded-md bg-sand-100 text-stone-700 border border-sand-200 text-[10px] font-mono font-bold uppercase">
              {u.plan} PLAN
            </span>
          </div>
          <p className="text-xs text-stone-500 mt-1">
            {u.plan === "free"
              ? "Running on the shared Kiwi fleet with monthly pooled agent minutes."
              : "Dedicated compute fleet with high-throughput multi-agent execution."}
          </p>
        </div>

        <UpgradeButton plan={u.plan} variant="compact" />
      </div>

      {/* Usage Meter Grid */}
      <div className="grid grid-cols-1 md:grid-cols-2 gap-4 pt-1">
        {/* Agent Minutes Meter */}
        <div className="p-4 rounded-xl bg-sand-50/70 border border-sand-200 space-y-2.5">
          <div className="flex items-center justify-between text-xs">
            <span className="font-bold text-stone-700">Monthly Agent-Minutes</span>
            <span className={`font-mono font-bold ${usedColor}`}>
              {u.agent_minutes_used.toFixed(1)} {hasCap ? `/ ${resolvedLimit}` : ""} min
            </span>
          </div>

          {hasCap ? (
            <div className="space-y-1.5">
              <div className="h-2 rounded-full bg-sand-200 overflow-hidden">
                <div
                  className={`h-full rounded-full transition-all duration-300 ${barColor}`}
                  style={{ width: `${pct}%` }}
                />
              </div>
              <div className="flex items-center justify-between text-[10px] font-mono text-stone-400">
                <span>{(100 - pct).toFixed(0)}% remaining</span>
                <span>{hasCap ? `${(resolvedLimit - u.agent_minutes_used).toFixed(1)} min left` : "Unlimited"}</span>
              </div>
            </div>
          ) : (
            <p className="text-xs text-stone-400 font-mono">Unlimited agent minutes on this plan.</p>
          )}

          {over && (
            <div className="p-2.5 rounded-lg bg-rose-50 border border-rose-200 text-rose-800 text-xs flex items-center gap-2">
              <AlertCircle className="w-4 h-4 text-rose-600 shrink-0" />
              <span>Monthly quota reached — {u.plan === "free" ? "upgrade to Pro to continue running tasks." : "contact enterprise to increase compute limits."}</span>
            </div>
          )}
          {near && (
            <div className="p-2.5 rounded-lg bg-amber-50 border border-amber-200 text-amber-800 text-xs flex items-center gap-2">
              <AlertCircle className="w-4 h-4 text-amber-600 shrink-0" />
              <span>Approaching monthly allowance limit.</span>
            </div>
          )}
        </div>

        {/* Concurrency & Fleet */}
        <div className="p-4 rounded-xl bg-sand-50/70 border border-sand-200 flex flex-col justify-between space-y-3">
          <div className="space-y-2">
            <div className="flex items-center justify-between text-xs">
              <span className="font-bold text-stone-700">Concurrent Tasks</span>
              <span className="font-mono font-bold text-stone-900">
                {u.concurrent_jobs_running} / {u.max_concurrent_jobs} active
              </span>
            </div>

            <div className="h-2 rounded-full bg-sand-200 overflow-hidden">
              <div
                className="h-full rounded-full bg-kiwi-600 transition-all duration-300"
                style={{
                  width: `${Math.min(100, (u.concurrent_jobs_running / Math.max(1, u.max_concurrent_jobs)) * 100)}%`,
                }}
              />
            </div>
          </div>

          <div className="pt-2 border-t border-sand-200 flex items-center justify-between text-[10px] font-mono text-stone-500">
            <span className="flex items-center gap-1">
              <Clock className="w-3 h-3 text-stone-400" />
              <span>Resets on the 1st of next month</span>
            </span>
            <span className="text-emerald-700 font-bold">Auto-renewal active</span>
          </div>
        </div>
      </div>

      {/* Platform Token Quotas (Kiwi-Funded) */}
      {spend?.allowance && spend.allowance.length > 0 && (
        <div className="pt-2 border-t border-sand-150 space-y-3">
          <div className="flex items-center justify-between">
            <span className="text-xs font-bold text-stone-800 flex items-center gap-1.5 uppercase tracking-wider">
              <Zap className="w-3.5 h-3.5 text-kiwi-600" />
              <span>Kiwi Platform Token Allowances</span>
            </span>
            <span className="text-[10px] font-mono text-stone-400">Monthly renewal</span>
          </div>

          <div className="grid grid-cols-1 sm:grid-cols-3 gap-3">
            {CLASS_ORDER.map((tierKey) => {
              const a = spend.allowance?.find((x) => x.tier === tierKey);
              if (!a) return null;
              const unlimited = a.granted < 0;
              const exhausted = !unlimited && (a.remaining <= 0 || a.used >= a.granted);
              const pct = unlimited ? 0 : Math.min(100, (a.used / Math.max(a.granted, 1)) * 100);

              return (
                <div
                  key={a.tier}
                  className={`p-3 rounded-xl border space-y-1.5 ${
                    exhausted ? "bg-rose-50/60 border-rose-200" : "bg-sand-50/70 border-sand-200"
                  }`}
                >
                  <div className="flex items-center justify-between text-xs">
                    <span className="font-bold text-stone-800">{modelClassLabel(a.tier)}</span>
                    <span className={`font-mono text-[10px] font-bold ${exhausted ? "text-rose-700" : "text-stone-700"}`}>
                      {unlimited ? "Unlimited" : exhausted ? "⛔ Exhausted" : `${formatTokens(a.remaining)} left`}
                    </span>
                  </div>

                  <div className="w-full bg-sand-200 rounded-full h-1 overflow-hidden">
                    <div
                      className={`h-full rounded-full transition-all duration-300 ${
                        exhausted
                          ? "bg-rose-500"
                          : pct > 80
                          ? "bg-amber-500"
                          : a.tier === "frontier"
                          ? "bg-amber-500"
                          : a.tier === "economy"
                          ? "bg-emerald-500"
                          : "bg-sky-500"
                      }`}
                      style={{ width: `${unlimited ? 100 : pct}%` }}
                    />
                  </div>

                  <div className="text-[9px] text-stone-400 font-mono">
                    {MODEL_CLASS_BLURB[a.tier] || "Platform allowance"}
                  </div>
                </div>
              );
            })}
          </div>
        </div>
      )}
    </div>
  );
}
