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
  const barColor = over ? "bg-red-400" : near ? "bg-amber-400" : "bg-[#93C645]";
  const usedColor = over ? "text-red-300" : near ? "text-amber-300" : "text-zinc-300";

  return (
    <div className="glass-panel p-6">
      <h2 className="text-lg font-medium text-white flex items-center gap-2 mb-4">
        <Gauge className="w-5 h-5 text-zinc-300" /> Plan &amp; usage
      </h2>

      <div className="flex items-center gap-2 text-sm mb-5">
        <span className="inline-flex items-center px-2 py-0.5 rounded bg-[#93C645]/10 text-[#93C645] text-xs font-medium uppercase">
          {u.plan}
        </span>
        {u.plan === "free" && (
          <span className="text-zinc-500 text-xs">shared fleet · one job at a time</span>
        )}
      </div>

      {/* Agent-minutes meter */}
      <div className="mb-5">
        <div className="flex items-center justify-between text-xs mb-1.5">
          <span className="text-zinc-400 uppercase tracking-widest">Agent-minutes this month</span>
          <span className={usedColor}>
            {u.agent_minutes_used.toFixed(1)}
            {hasCap ? ` / ${u.agent_minutes_limit}` : ""} min
          </span>
        </div>
        {hasCap ? (
          <div className="h-2 rounded-full bg-white/[0.06] overflow-hidden">
            <div className={`h-full rounded-full transition-all ${barColor}`} style={{ width: `${pct}%` }} />
          </div>
        ) : (
          <p className="text-xs text-zinc-500">No monthly cap on this plan.</p>
        )}
        {over && (
          <p className="text-xs text-red-300 mt-1.5">
            You&apos;ve reached your monthly allowance — new tasks won&apos;t start until it resets.
          </p>
        )}
        {near && (
          <p className="text-xs text-amber-300/80 mt-1.5">You&apos;re close to your monthly allowance.</p>
        )}
      </div>

      {/* Concurrency */}
      <div className="flex items-center justify-between text-xs">
        <span className="text-zinc-400 uppercase tracking-widest">Concurrent jobs</span>
        <span className="text-zinc-300">
          {u.concurrent_jobs_running} / {u.max_concurrent_jobs} running
        </span>
      </div>
    </div>
  );
}
