"use client";

import { useEffect, useState } from "react";
import { client, type PostMergeMonitor } from "@/lib/api";
import { Radar, Ban, Loader2, AlertCircle, CheckCircle2, XCircle, GitPullRequest, Plus } from "lucide-react";

// Monitor statuses are a distinct state machine from job statuses (statusColors.ts
// is keyed to QUEUED/RUNNING/... and has no MONITORING/VERIFIED/REGRESSION), so
// this is its own small map rather than overloading that one.
const STATUS_META: Record<PostMergeMonitor["status"], { label: string; Icon: typeof Radar; color: string; border: string; wash: string }> = {
  MONITORING: { label: "Monitoring", Icon: Loader2, color: "#5A9DF5", border: "rgba(59,130,246,0.34)", wash: "rgba(59,130,246,0.15)" },
  VERIFIED: { label: "Verified", Icon: CheckCircle2, color: "#93C645", border: "rgba(147,198,69,0.30)", wash: "rgba(147,198,69,0.13)" },
  REGRESSION: { label: "Regression", Icon: XCircle, color: "#EF6060", border: "rgba(239,68,68,0.30)", wash: "rgba(239,68,68,0.14)" },
  CANCELLED: { label: "Cancelled", Icon: Ban, color: "#A0A0A0", border: "rgba(160,160,160,0.30)", wash: "rgba(160,160,160,0.10)" },
};

export default function MonitorsPage() {
  const [monitors, setMonitors] = useState<PostMergeMonitor[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [confirmCancelId, setConfirmCancelId] = useState<string | null>(null);
  const [busyId, setBusyId] = useState<string | null>(null);
  const [prUrl, setPrUrl] = useState("");
  const [createError, setCreateError] = useState<string | null>(null);
  const [creating, setCreating] = useState(false);

  const load = async () => {
    try {
      const res = await client.listMonitors();
      setMonitors(res.monitors);
    } catch (e) {
      setError(e instanceof Error ? e.message : "Failed to load monitors");
    } finally {
      setLoading(false);
    }
  };
  // eslint-disable-next-line react-hooks/set-state-in-effect
  useEffect(() => { load(); }, []);

  // Stand a primed confirm down on Escape or any click outside the primed
  // button — same convention as the Tasks board's card cancel/delete buttons.
  useEffect(() => {
    if (!confirmCancelId) return;
    const standDown = () => setConfirmCancelId(null);
    const onKey = (e: KeyboardEvent) => { if (e.key === "Escape") standDown(); };
    const onDown = (e: MouseEvent) => {
      if ((e.target as HTMLElement).closest("[data-confirm-action]")) return;
      standDown();
    };
    document.addEventListener("keydown", onKey);
    document.addEventListener("mousedown", onDown);
    return () => {
      document.removeEventListener("keydown", onKey);
      document.removeEventListener("mousedown", onDown);
    };
  }, [confirmCancelId]);

  const handleCancel = async (id: string) => {
    if (confirmCancelId !== id) {
      setConfirmCancelId(id);
      return;
    }
    setConfirmCancelId(null);
    setError("");
    setBusyId(id);
    try {
      await client.cancelMonitor(id);
      await load();
    } catch (e) {
      setError(e instanceof Error ? e.message : "Failed to cancel monitor");
    } finally {
      setBusyId(null);
    }
  };

  const handleCreate = async (e: React.FormEvent) => {
    e.preventDefault();
    setCreateError(null);
    setCreating(true);
    try {
      await client.createMonitor(prUrl);
      setPrUrl("");
      await load();
    } catch (err) {
      setCreateError(err instanceof Error ? err.message : "Failed to create monitor");
    } finally {
      setCreating(false);
    }
  };

  return (
    <div className="p-8 max-w-5xl mx-auto h-full flex flex-col text-white">
      <div className="mb-8">
        <h1 className="text-3xl font-light tracking-tight mb-2">Post-Merge Monitors</h1>
        <p className="text-zinc-400">
          Kiwi watches a merged PR&apos;s telemetry for a window after deploy and flags a regression
          if one appears.
        </p>
      </div>

      {error && <div className="flex items-center gap-2 text-red-400 text-sm mb-4"><AlertCircle className="w-4 h-4 shrink-0" />{error}</div>}

      <div className="glass-panel border border-white/10 rounded-2xl p-5 mb-8">
        <label className="block text-[10px] font-bold text-zinc-500 uppercase tracking-widest mb-1.5">Watch a merged PR</label>
        <form onSubmit={handleCreate} className="flex gap-3">
          <input
            value={prUrl}
            onChange={e => setPrUrl(e.target.value)}
            placeholder="https://github.com/org/repo/pull/123"
            aria-label="Pull request URL"
            className="w-full field text-sm"
          />
          <button type="submit" disabled={creating || !prUrl.trim()}
            className="flex items-center justify-center gap-2 btn-primary px-4 py-2 rounded-lg font-semibold disabled:opacity-50 h-[38px] shrink-0">
            {creating ? <Loader2 className="w-4 h-4 animate-spin" /> : <Plus className="w-4 h-4" />} Add
          </button>
        </form>
        {createError && <div className="flex items-center gap-2 text-red-400 text-sm mt-3"><AlertCircle className="w-4 h-4 shrink-0" />{createError}</div>}
      </div>

      <h2 className="text-xs font-bold text-zinc-500 uppercase tracking-widest mb-3">Monitors</h2>
      {loading ? (
        <div className="flex items-center gap-2 text-sm text-zinc-500">
          <Loader2 className="w-4 h-4 animate-spin" /> Loading monitors...
        </div>
      ) : monitors.length === 0 ? (
        <p className="text-zinc-500 text-sm">No monitors yet.</p>
      ) : (
        <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
          {monitors.map(m => {
            const meta = STATUS_META[m.status] ?? STATUS_META.MONITORING;
            const StatusIcon = meta.Icon;
            return (
              <div key={m.id} className="glass-panel p-4 border border-white/10 rounded-xl flex items-center justify-between group">
                <div className="flex items-center gap-3 min-w-0">
                  <GitPullRequest className="w-5 h-5 text-zinc-400 shrink-0" />
                  <div className="min-w-0">
                    <div className="text-sm text-white truncate">
                      {m.repo} <span className="text-zinc-500">#{m.pr_number}</span>
                    </div>
                    <div className="flex items-center gap-2 mt-1">
                      <span className="text-[10px] font-medium uppercase tracking-wider text-zinc-400 bg-white/5 px-1.5 py-0.5 rounded shrink-0">
                        {m.origin === "kiwi_pr" ? "Kiwi-authored" : "Watching"}
                      </span>
                      <span
                        className="inline-flex items-center gap-1 rounded-full border px-1.5 py-0.5 text-[10px] font-bold uppercase tracking-wider shrink-0"
                        style={{ color: meta.color, borderColor: meta.border, background: meta.wash }}
                      >
                        <StatusIcon className={`w-3 h-3 shrink-0 ${m.status === "MONITORING" ? "animate-spin" : ""}`} />
                        {meta.label}
                      </span>
                    </div>
                    <div className="text-[11px] text-zinc-600 truncate mt-1">
                      Window ends {new Date(m.window_ends_at).toLocaleString()}
                    </div>
                  </div>
                </div>
                {m.status === "MONITORING" && (
                  busyId === m.id ? (
                    <Loader2 className="w-4 h-4 animate-spin text-zinc-400 shrink-0" />
                  ) : (
                    <button
                      type="button"
                      onClick={() => handleCancel(m.id)}
                      data-confirm-action
                      className={`shrink-0 flex items-center gap-1.5 px-2 py-1 rounded text-[11px] font-medium border transition-all ${
                        confirmCancelId === m.id
                          ? "border-amber-500/50 bg-amber-500/20 text-amber-300"
                          : "border-white/10 text-zinc-400 hover:text-amber-300 hover:border-amber-500/30 hover:bg-amber-500/10"
                      }`}
                    >
                      <Ban className="w-3.5 h-3.5 shrink-0" />
                      {confirmCancelId === m.id ? "Confirm cancel?" : "Cancel"}
                    </button>
                  )
                )}
              </div>
            );
          })}
        </div>
      )}
    </div>
  );
}
