"use client";

import { useEffect, useState } from "react";
import { client, type PostMergeMonitor } from "@/lib/api";
import { Radar, Ban, Loader2, AlertCircle, CheckCircle2, XCircle, GitPullRequest, Plus, ShieldCheck, Activity } from "lucide-react";
import { KiwiCoreSpinner } from "@/components/KiwiLoaders";

const STATUS_META: Record<PostMergeMonitor["status"], { label: string; Icon: any; colorClass: string; badgeClass: string }> = {
  MONITORING: {
    label: "Monitoring",
    Icon: Loader2,
    colorClass: "text-sky-600",
    badgeClass: "bg-sky-50 text-sky-800 border-sky-200",
  },
  VERIFIED: {
    label: "Verified",
    Icon: CheckCircle2,
    colorClass: "text-emerald-600",
    badgeClass: "bg-emerald-50 text-emerald-800 border-emerald-200",
  },
  REGRESSION: {
    label: "Regression",
    Icon: XCircle,
    colorClass: "text-rose-600",
    badgeClass: "bg-rose-50 text-rose-800 border-rose-200",
  },
  CANCELLED: {
    label: "Cancelled",
    Icon: Ban,
    colorClass: "text-stone-400",
    badgeClass: "bg-sand-100 text-stone-600 border-sand-200",
  },
};

export default function MonitorsPage() {
  const [monitors, setMonitors] = useState<PostMergeMonitor[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [prUrl, setPrUrl] = useState("");
  const [createError, setCreateError] = useState<string | null>(null);
  const [creating, setCreating] = useState(false);

  const load = async () => {
    try {
      const res = await client.listMonitors();
      setMonitors(res.monitors || []);
    } catch (e) {
      setError(e instanceof Error ? e.message : "Failed to load monitors");
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => { load(); }, []);

  const handleCreate = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!prUrl.trim()) return;
    setCreateError(null);
    setCreating(true);
    try {
      await client.createMonitor(prUrl.trim());
      setPrUrl("");
      await load();
    } catch (err: any) {
      setCreateError(err?.message || "Failed to create monitor");
    } finally {
      setCreating(false);
    }
  };

  return (
    <div className="p-6 max-w-5xl mx-auto space-y-6">
      {/* Header */}
      <div>
        <h1 className="text-xl font-bold text-stone-900">Post-Merge PR Watchdogs</h1>
        <p className="text-xs text-stone-500">Continuous telemetry verification after pull request merge & deploy.</p>
      </div>

      {error && (
        <div className="p-3 rounded-xl bg-rose-50 border border-rose-200 text-rose-800 text-xs flex items-center gap-2">
          <AlertCircle className="w-4 h-4 shrink-0" />
          <span>{error}</span>
        </div>
      )}

      {/* Add Watchdog Form */}
      <div className="p-4 rounded-2xl border border-sand-200 bg-white shadow-2xs space-y-2">
        <label className="block text-[11px] font-bold text-stone-700">Watch a merged pull request</label>
        <form onSubmit={handleCreate} className="flex gap-2">
          <input
            value={prUrl}
            onChange={(e) => setPrUrl(e.target.value)}
            placeholder="https://github.com/org/repo/pull/123"
            aria-label="Pull request URL"
            className="flex-1 p-2.5 rounded-xl border border-sand-200 bg-sand-50/50 text-xs font-mono text-stone-900 placeholder:text-stone-400 focus:outline-none focus:ring-1 focus:ring-stone-900"
          />
          <button
            type="submit"
            disabled={creating || !prUrl.trim()}
            className="px-4 py-2 rounded-xl bg-stone-900 hover:bg-stone-800 text-white text-xs font-bold flex items-center gap-1.5 disabled:opacity-50 transition-all shadow-2xs"
          >
            {creating ? <Loader2 className="w-3.5 h-3.5 animate-spin" /> : <Plus className="w-3.5 h-3.5" />}
            <span>Add Watchdog</span>
          </button>
        </form>
        {createError && (
          <div className="text-xs text-rose-600 flex items-center gap-1.5 pt-1">
            <AlertCircle className="w-3.5 h-3.5 shrink-0" />
            <span>{createError}</span>
          </div>
        )}
      </div>

      {/* Monitors List */}
      <div className="rounded-2xl border border-sand-200 bg-white overflow-hidden shadow-2xs">
        <div className="px-4 py-3 border-b border-sand-200 bg-sand-50/50 flex items-center justify-between">
          <span className="text-xs font-bold text-stone-900">Active Monitors ({monitors.length})</span>
          <span className="text-[10px] font-mono text-stone-500">p99 Latency & Error Rate Telemetry</span>
        </div>

        {loading ? (
          <div className="py-12 flex flex-col items-center justify-center gap-2">
            <KiwiCoreSpinner size="md" />
            <span className="text-xs font-mono text-stone-500">Loading telemetry monitors...</span>
          </div>
        ) : monitors.length === 0 ? (
          <div className="p-8 text-center text-xs text-stone-500 space-y-2">
            <Radar className="w-6 h-6 mx-auto text-stone-400" />
            <p>No active pull request watchdogs configured.</p>
          </div>
        ) : (
          <div className="divide-y divide-sand-200">
            {monitors.map((m) => {
              const meta = STATUS_META[m.status] || STATUS_META.MONITORING;
              return (
                <div key={m.id} className="p-4 flex items-center justify-between gap-4 hover:bg-sand-50/50 transition-all">
                  <div className="space-y-1">
                    <div className="flex items-center gap-2">
                      <GitPullRequest className="w-3.5 h-3.5 text-stone-500" />
                      <span className="text-xs font-bold text-stone-900">
                        {m.repo} #{m.pr_number}
                      </span>
                    </div>
                    <div className="text-[10px] font-mono text-stone-500">
                      ID: {m.id} • SHA: {m.merge_commit_sha ? m.merge_commit_sha.slice(0, 7) : "HEAD"}
                    </div>
                  </div>

                  <div className="flex items-center gap-3">
                    <span className={`px-2.5 py-0.5 rounded-full text-[10px] font-mono font-bold border flex items-center gap-1.5 ${meta.badgeClass}`}>
                      <span className="w-1.5 h-1.5 rounded-full bg-current" />
                      <span>{meta.label}</span>
                    </span>
                  </div>
                </div>
              );
            })}
          </div>
        )}
      </div>
    </div>
  );
}
