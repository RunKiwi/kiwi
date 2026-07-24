"use client";

import { useEffect, useState } from "react";
import { Gauge } from "lucide-react";
import { client, type UsageResponse, PRO_UPGRADE_MAILTO } from "@/lib/api";

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
        {over && u.plan !== "free" && (
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

      {u.plan === "free" && (
        <div className="mt-6 pt-5 border-t border-white/5">
          <div className="mb-4">
            <h3 className="text-sm font-medium text-white mb-1.5">You&apos;re on the Free plan.</h3>
            <p className="text-xs text-zinc-400 mb-2">Pro unlocks:</p>
            <ul className="text-xs text-zinc-300 space-y-1 ml-4 list-disc marker:text-zinc-600">
              <li>Dedicated BYOC or managed fleet</li>
              <li>Higher concurrency limits</li>
              <li>More agent-minutes</li>
              <li>Priority support</li>
            </ul>
            <p className="text-[10px] text-zinc-500 mt-2">from $39/user/mo</p>
          </div>
          
          {over && (
            <p className="text-xs text-red-400 mb-3">
              You&apos;ve reached your monthly allowance. Upgrade to start new tasks immediately.
            </p>
          )}

          <a 
            href={PRO_UPGRADE_MAILTO} 
            className="flex items-center justify-center w-full btn-primary px-4 py-2 text-sm font-medium rounded-lg"
          >
            Upgrade to Pro
          </a>
        </div>
      )}
    </div>
  );
}
