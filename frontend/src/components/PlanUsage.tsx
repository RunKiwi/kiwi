"use client";

import { useEffect, useState } from "react";
import { Gauge } from "lucide-react";
import { client, type UsageResponse } from "@/lib/api";

// Plan + current-month usage panel: an agent-minutes meter against the plan's
// monthly allowance, plus concurrency. Backed by GET /api/v1/usage. The backend
// meters agent-minutes and refuses new leases past the cap, so this makes that
// otherwise-invisible ceiling legible before a task silently stops starting.
export function PlanUsage() {
  const [u, setU] = useState<UsageResponse | null>(null);

  useEffect(() => {
    client.getUsage().then(setU).catch(() => setU(null));
  }, []);

  if (!u) return null;

  const hasCap = u.agent_minutes_limit > 0;
  const pct = hasCap ? Math.min(100, (u.agent_minutes_used / u.agent_minutes_limit) * 100) : 0;
  const over = hasCap && u.agent_minutes_used >= u.agent_minutes_limit;
  const near = hasCap && !over && pct >= 80;
  const barColor = over ? "bg-rose-500" : near ? "bg-amber-500" : "bg-kiwi-600";
  const usedColor = over ? "text-rose-700" : near ? "text-amber-700" : "text-stone-700";

  return (
    <div className="bg-white border border-sand-200 rounded-2xl shadow-2xs p-6">
      <h2 className="text-lg font-bold text-stone-900 flex items-center gap-2 mb-4">
        <Gauge className="w-5 h-5 text-stone-500" /> Plan &amp; usage
      </h2>

      <div className="flex items-center gap-2 text-sm mb-5">
        <span className="inline-flex items-center px-2 py-0.5 rounded bg-kiwi-50 text-kiwi-800 border border-kiwi-200 text-xs font-bold uppercase">
          {u.plan}
        </span>
        {u.plan === "free" && (
          <span className="text-stone-400 text-xs">shared fleet · one job at a time</span>
        )}
      </div>

      {/* Agent-minutes meter */}
      <div className="mb-5">
        <div className="flex items-center justify-between text-xs mb-1.5">
          <span className="text-stone-500 uppercase tracking-widest">Agent-minutes this month</span>
          <span className={usedColor}>
            {u.agent_minutes_used.toFixed(1)}
            {hasCap ? ` / ${u.agent_minutes_limit}` : ""} min
          </span>
        </div>
        {hasCap ? (
          <div className="h-2 rounded-full bg-sand-150 overflow-hidden">
            <div className={`h-full rounded-full transition-all ${barColor}`} style={{ width: `${pct}%` }} />
          </div>
        ) : (
          <p className="text-xs text-stone-400">No monthly cap on this plan.</p>
        )}
        {over && (
          <p className="text-xs text-rose-700 mt-1.5">
            You&apos;ve reached your monthly allowance — new tasks won&apos;t start until it resets.
          </p>
        )}
        {near && (
          <p className="text-xs text-amber-700 mt-1.5">You&apos;re close to your monthly allowance.</p>
        )}
      </div>

      {/* Concurrency */}
      <div className="flex items-center justify-between text-xs">
        <span className="text-stone-500 uppercase tracking-widest">Concurrent jobs</span>
        <span className="text-stone-700">
          {u.concurrent_jobs_running} / {u.max_concurrent_jobs} running
        </span>
      </div>
    </div>
  );
}
