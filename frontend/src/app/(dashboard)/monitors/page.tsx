"use client";

import React, { useState, useMemo, useCallback } from "react";
import Link from "next/link";
import { client, type PostMergeMonitor } from "@/lib/api";
import {
  Radar,
  Ban,
  CheckCircle2,
  XCircle,
  GitPullRequest,
  Plus,
  Clock,
  ExternalLink,
  Trash2,
  Search,
  AlertTriangle,
  Activity,
  Copy,
  Check,
  ShieldAlert,
  GitCommit,
  Layers,
  LineChart,
} from "lucide-react";
import { KiwiMicroButtonLoader } from "@/components/KiwiLoaders";
import { LoadingState } from "@/components/LoadingState";
import { Logo } from "@/components/Logo";
import { usePolling } from "@/hooks/usePolling";

export default function MonitorsPage() {
  const [monitors, setMonitors] = useState<PostMergeMonitor[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [prUrl, setPrUrl] = useState("");
  const [createError, setCreateError] = useState<string | null>(null);
  const [creating, setCreating] = useState(false);
  const [cancellingId, setCancellingId] = useState<string | null>(null);
  const [searchQuery, setSearchQuery] = useState("");
  const [statusFilter, setStatusFilter] = useState<string>("all");
  const [copiedSha, setCopiedSha] = useState<string | null>(null);
  const [now, setNow] = useState(() => Date.now());

  const load = useCallback(async () => {
    try {
      const res = await client.listMonitors();
      setMonitors(res.monitors || []);
    } catch (e) {
      setError(e instanceof Error ? e.message : "Failed to load monitors");
    } finally {
      setLoading(false);
    }
  }, []);

  usePolling(
    async () => {
      setNow(Date.now());
      await load();
    },
    {
      activeIntervalMs: 5000,
      idleIntervalMs: 15000,
      isIdle: monitors.length === 0 || !monitors.some((m) => m.status === "MONITORING"),
    }
  );

  const handleCreate = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!prUrl.trim()) return;
    setCreating(true);
    setCreateError(null);
    try {
      await client.createMonitor(prUrl.trim());
      setPrUrl("");
      await load();
    } catch (err) {
      setCreateError(err instanceof Error ? err.message : "Failed to create monitor");
    } finally {
      setCreating(false);
    }
  };

  const handleDeleteOrCancel = async (id: string, isMonitoring: boolean) => {
    const promptMsg = isMonitoring
      ? "Are you sure you want to stop and delete this active PR watchdog?"
      : "Delete this watchdog from your list?";
    if (!window.confirm(promptMsg)) return;

    setCancellingId(id);
    try {
      if (isMonitoring) {
        await client.cancelMonitor(id);
      }
      setMonitors((prev) => prev.filter((m) => m.id !== id));
      await load();
    } catch (err) {
      alert(err instanceof Error ? err.message : "Failed to delete monitor");
    } finally {
      setCancellingId(null);
    }
  };

  const copySha = (sha: string) => {
    navigator.clipboard.writeText(sha);
    setCopiedSha(sha);
    setTimeout(() => setCopiedSha(null), 2000);
  };

  // Helper for relative time and expiry
  const getExpiryInfo = (m: PostMergeMonitor) => {
    if (!m.window_ends_at) return { text: "24h window", remainingText: "24h window", percent: 0, isExpired: false };

    const endMs = new Date(m.window_ends_at).getTime();
    const startMs = m.deployed_at ? new Date(m.deployed_at).getTime() : endMs - 24 * 3600 * 1000;
    const diffMs = endMs - now;
    const totalMs = Math.max(1, endMs - startMs);
    const elapsedMs = now - startMs;
    const percent = Math.min(100, Math.max(0, Math.round((elapsedMs / totalMs) * 100)));
    const isExpired = diffMs <= 0;

    const formattedEndDate = new Date(endMs).toLocaleTimeString([], {
      month: "short",
      day: "numeric",
      hour: "2-digit",
      minute: "2-digit",
    });

    if (isExpired) {
      return {
        text: `Expired on ${formattedEndDate}`,
        remainingText: "Window ended",
        percent: 100,
        isExpired: true,
      };
    }

    const hours = Math.floor(diffMs / (1000 * 60 * 60));
    const minutes = Math.floor((diffMs % (1000 * 60 * 60)) / (1000 * 60));

    return {
      text: `Expires ${formattedEndDate}`,
      remainingText: hours > 0 ? `${hours}h ${minutes}m left` : `${minutes}m left`,
      percent,
      isExpired: false,
    };
  };

  // Compute Telemetry KPI stats
  const stats = useMemo(() => {
    const active = monitors.filter((m) => m.status === "MONITORING").length;
    const verified = monitors.filter((m) => m.status === "VERIFIED").length;
    const regressions = monitors.filter((m) => m.status === "REGRESSION").length;
    const cancelled = monitors.filter((m) => m.status === "CANCELLED").length;
    return { active, verified, regressions, cancelled, total: monitors.length };
  }, [monitors]);

  // Filtered monitors list
  const filteredMonitors = useMemo(() => {
    const q = searchQuery.trim().toLowerCase();
    return monitors.filter((m) => {
      const matchesQuery =
        !q ||
        m.repo.toLowerCase().includes(q) ||
        String(m.pr_number).includes(q) ||
        m.id.toLowerCase().includes(q) ||
        (m.merge_commit_sha && m.merge_commit_sha.toLowerCase().includes(q));

      const matchesStatus =
        statusFilter === "all" ||
        m.status.toLowerCase() === statusFilter.toLowerCase();

      return matchesQuery && matchesStatus;
    });
  }, [monitors, searchQuery, statusFilter]);

  return (
    <div className="max-w-6xl mx-auto flex flex-col gap-4 w-full font-sans text-stone-900 select-none">
      
      {/* Header Banner with Modern Swiss Styling */}
      <div className="p-4 sm:p-5 rounded-2xl border border-sand-200/90 bg-white shadow-2xs flex flex-col sm:flex-row items-start sm:items-center justify-between gap-4">
        <div className="flex items-center gap-3.5">
          <div className="w-12 h-12 rounded-2xl bg-sand-50 border border-sand-200/90 shadow-2xs flex items-center justify-center shrink-0">
            <Logo variant="full-color" pose="guarding" animated={true} className="w-7 h-7" />
          </div>
          <div>
            <div className="flex items-center gap-2">
              <span className="text-[9px] font-mono font-bold uppercase tracking-wider text-sky-800 bg-sky-50 px-2 py-0.5 rounded border border-sky-200 flex items-center gap-1">
                <Radar className="w-3 h-3 text-sky-600 animate-pulse" />
                <span>{stats.active} CANARY WATCHDOGS ACTIVE</span>
              </span>
            </div>
            <h1 className="text-lg font-bold text-stone-900 tracking-tight mt-0.5">
              PR Telemetry Watchdogs
            </h1>
            <p className="text-xs text-stone-500 mt-0.5">
              Continuous post-merge telemetry tracking p99 latency, error rate spikes, and canary regressions.
            </p>
          </div>
        </div>

        <div className="flex items-center gap-2 self-end sm:self-center">
          <Link
            href="/metrics"
            className="px-3 py-1.5 rounded-xl bg-sand-50/80 hover:bg-sand-100 border border-sand-200/90 text-stone-700 font-semibold text-xs transition-all flex items-center gap-1.5 cursor-pointer shadow-2xs"
          >
            <LineChart className="w-3.5 h-3.5 text-indigo-600" />
            <span>Configure SLOs</span>
          </Link>

          <button
            onClick={load}
            className="px-3 py-1.5 rounded-xl bg-charcoal-900 hover:bg-charcoal-800 text-white font-semibold text-xs shadow-2xs flex items-center gap-1.5 transition-all cursor-pointer"
          >
            <Activity className="w-3.5 h-3.5 text-kiwi-400" />
            <span>Poll Telemetry</span>
          </button>
        </div>
      </div>

      {error && (
        <div className="p-3 rounded-xl bg-rose-50 border border-rose-200 text-rose-800 text-xs flex items-center gap-2 shadow-2xs font-mono">
          <AlertTriangle className="w-4 h-4 text-rose-600 shrink-0" />
          <span>{error}</span>
        </div>
      )}

      {/* Top 4 KPI Tiles */}
      <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-3">
        {/* KPI 1 */}
        <div className="p-3.5 rounded-xl bg-white border border-sand-200/90 shadow-2xs flex flex-col justify-between">
          <div className="flex items-center justify-between text-xs text-stone-500 font-medium">
            <span>Active Monitoring</span>
            <Radar className="w-3.5 h-3.5 text-sky-600" />
          </div>
          <div className="mt-2">
            <div className="text-xl font-bold font-mono text-stone-900">{stats.active}</div>
            <div className="text-[10px] text-stone-400 font-mono mt-0.5">Live sampling canary signals</div>
          </div>
          <div className="mt-2 flex items-end gap-1 h-2.5">
            {[40, 60, 45, 80, 50, 90, 75].map((h, i) => (
              <div key={i} className="flex-1 bg-sky-200 rounded-2xs" style={{ height: `${h}%` }} />
            ))}
          </div>
        </div>

        {/* KPI 2 */}
        <div className="p-3.5 rounded-xl bg-white border border-sand-200/90 shadow-2xs flex flex-col justify-between">
          <div className="flex items-center justify-between text-xs text-stone-500 font-medium">
            <span>Verified Releases</span>
            <CheckCircle2 className="w-3.5 h-3.5 text-emerald-600" />
          </div>
          <div className="mt-2">
            <div className="text-xl font-bold font-mono text-emerald-800">{stats.verified}</div>
            <div className="text-[10px] text-stone-400 font-mono mt-0.5">Passed telemetry bounds</div>
          </div>
          <div className="mt-2 flex items-end gap-1 h-2.5">
            {[80, 85, 90, 92, 95, 98, 100].map((h, i) => (
              <div key={i} className="flex-1 bg-emerald-200 rounded-2xs" style={{ height: `${h}%` }} />
            ))}
          </div>
        </div>

        {/* KPI 3 */}
        <div className="p-3.5 rounded-xl bg-white border border-sand-200/90 shadow-2xs flex flex-col justify-between">
          <div className="flex items-center justify-between text-xs text-stone-500 font-medium">
            <span>Regressions Detected</span>
            <XCircle className="w-3.5 h-3.5 text-rose-600" />
          </div>
          <div className="mt-2">
            <div className="text-xl font-bold font-mono text-rose-800">{stats.regressions}</div>
            <div className="text-[10px] text-stone-400 font-mono mt-0.5">Anomalies caught in window</div>
          </div>
          <div className="mt-2 flex items-end gap-1 h-2.5">
            {[0, 0, 0, 0, 0, 0, stats.regressions > 0 ? 80 : 0].map((h, i) => (
              <div key={i} className="flex-1 bg-rose-200 rounded-2xs" style={{ height: `${h}%` }} />
            ))}
          </div>
        </div>

        {/* KPI 4 */}
        <div className="p-3.5 rounded-xl bg-white border border-sand-200/90 shadow-2xs flex flex-col justify-between">
          <div className="flex items-center justify-between text-xs text-stone-500 font-medium">
            <span>Observation Window</span>
            <Clock className="w-3.5 h-3.5 text-amber-600" />
          </div>
          <div className="mt-2">
            <div className="text-xl font-bold font-mono text-stone-900">24 Hours</div>
            <div className="text-[10px] text-stone-400 font-mono mt-0.5">Automated telemetry sample</div>
          </div>
          <div className="mt-2 flex items-end gap-1 h-2.5">
            {[50, 60, 70, 80, 85, 90, 100].map((h, i) => (
              <div key={i} className="flex-1 bg-amber-200 rounded-2xs" style={{ height: `${h}%` }} />
            ))}
          </div>
        </div>
      </div>

      {/* Deploy Watchdog Form Card */}
      <div className="p-4 rounded-2xl border border-sand-200/90 bg-white shadow-2xs space-y-2.5">
        <div>
          <h2 className="text-xs font-bold text-stone-900 uppercase tracking-wider flex items-center gap-1.5">
            <Plus className="w-3.5 h-3.5 text-stone-700" />
            <span>Attach Post-Merge PR Watchdog</span>
          </h2>
          <p className="text-xs text-stone-500 mt-0.5">
            Enter a merged GitHub pull request URL. Kiwi will monitor live Datadog &amp; Prometheus metrics for latency and error rate spikes.
          </p>
        </div>

        <form onSubmit={handleCreate} className="flex flex-wrap sm:flex-nowrap gap-2 pt-1">
          <div className="relative flex-1">
            <GitPullRequest className="w-3.5 h-3.5 text-stone-400 absolute left-3 top-2.5 pointer-events-none" />
            <input
              value={prUrl}
              onChange={(e) => setPrUrl(e.target.value)}
              placeholder="https://github.com/owner/repository/pull/123"
              aria-label="Pull request URL"
              className="w-full pl-8 pr-3 py-2 rounded-xl border border-sand-200 bg-sand-50/70 text-xs font-mono text-stone-900 placeholder:text-stone-400 focus:outline-none focus:border-stone-900 focus:bg-white transition-all shadow-2xs"
            />
          </div>

          <button
            type="submit"
            disabled={creating || !prUrl.trim()}
            className="px-4 py-2 rounded-xl bg-charcoal-900 hover:bg-charcoal-800 text-white text-xs font-bold flex items-center justify-center gap-1.5 disabled:opacity-40 transition-all shadow-2xs shrink-0 cursor-pointer"
          >
            {creating ? <KiwiMicroButtonLoader /> : <Plus className="w-3.5 h-3.5 text-kiwi-400 stroke-[2.5]" />}
            <span>Deploy Watchdog</span>
          </button>
        </form>

        {createError && (
          <div className="text-xs text-rose-700 flex items-center gap-1.5 pt-0.5 font-mono">
            <AlertTriangle className="w-3.5 h-3.5 shrink-0 text-rose-600" />
            <span>{createError}</span>
          </div>
        )}
      </div>

      {/* Watchdogs Roster Card */}
      <div className="bg-white border border-sand-200/90 rounded-2xl shadow-2xs p-4 sm:p-5 space-y-3.5">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <div>
            <h3 className="text-xs font-bold text-stone-900 uppercase tracking-wider flex items-center gap-1.5">
              <Layers className="w-3.5 h-3.5 text-stone-600" />
              <span>Watchdogs Roster ({filteredMonitors.length})</span>
            </h3>
            <p className="text-xs text-stone-500 mt-0.5">Automated telemetry evaluations and canary observation windows.</p>
          </div>

          <div className="flex items-center gap-2 flex-wrap text-xs">
            <div className="relative">
              <Search className="w-3.5 h-3.5 text-stone-400 absolute left-3 top-2 pointer-events-none" />
              <input
                type="text"
                value={searchQuery}
                onChange={(e) => setSearchQuery(e.target.value)}
                placeholder="Search repo, PR #, or ID..."
                className="bg-sand-50/70 border border-sand-200 rounded-xl pl-8 pr-3 py-1.5 text-xs placeholder:text-stone-400 focus:outline-none focus:border-stone-900 focus:bg-white transition-all font-mono min-w-[180px]"
              />
            </div>

            <div className="flex items-center gap-1 bg-sand-100 p-0.5 rounded-xl border border-sand-200">
              {(["all", "monitoring", "verified", "regression"] as const).map((st) => (
                <button
                  key={st}
                  onClick={() => setStatusFilter(st)}
                  className={`px-2 py-0.5 rounded-lg text-xs font-semibold capitalize transition-all cursor-pointer ${
                    statusFilter === st
                      ? "bg-white text-stone-900 shadow-2xs font-bold"
                      : "text-stone-600 hover:text-stone-900"
                  }`}
                >
                  {st}
                </button>
              ))}
            </div>
          </div>
        </div>

        {/* List of Watchdogs */}
        {loading ? (
          <LoadingState state="searching" size={40} label="Loading telemetry monitors..." className="py-10" />
        ) : filteredMonitors.length === 0 ? (
          <div className="p-8 rounded-2xl border border-sand-200/90 bg-sand-50/40 text-center space-y-2.5 shadow-2xs">
            <div className="w-12 h-12 mx-auto rounded-2xl bg-white border border-sand-200/90 shadow-2xs flex items-center justify-center">
              <Logo variant="full-color" pose="guarding" animated={true} className="w-7 h-7" />
            </div>
            <div className="space-y-0.5">
              <div className="text-stone-900 font-bold text-xs">No PR Watchdogs Found</div>
              <p className="text-xs text-stone-500 max-w-xs mx-auto">
                Attach a merged pull request above to start automated telemetry and error rate monitoring.
              </p>
            </div>
          </div>
        ) : (
          <div className="grid grid-cols-1 gap-3">
            {filteredMonitors.map((m) => {
              const isMonitoring = m.status === "MONITORING";
              const isVerified = m.status === "VERIFIED";
              const isRegression = m.status === "REGRESSION";
              const expiry = getExpiryInfo(m);
              const prHref = `https://github.com/${m.repo}/pull/${m.pr_number}`;

              return (
                <div
                  key={m.id}
                  className={`p-3.5 sm:p-4 rounded-xl bg-white border shadow-2xs transition-all flex flex-col justify-between gap-3 ${
                    isRegression
                      ? "border-rose-300 ring-1 ring-rose-200"
                      : isMonitoring
                      ? "border-sky-300"
                      : "border-sand-200/90"
                  }`}
                >
                  {/* Top Row: PR Title, Origin, Status Badge, & Delete Button */}
                  <div className="flex flex-wrap items-start justify-between gap-2.5">
                    <div className="min-w-0 space-y-0.5">
                      <div className="flex items-center gap-2 flex-wrap">
                        <a
                          href={prHref}
                          target="_blank"
                          rel="noreferrer"
                          className="text-xs sm:text-sm font-bold text-stone-900 hover:text-kiwi-700 flex items-center gap-1.5 transition-colors group"
                        >
                          <GitPullRequest className="w-3.5 h-3.5 text-stone-600 group-hover:text-kiwi-600" />
                          <span>{m.repo} #{m.pr_number}</span>
                          <ExternalLink className="w-3 h-3 text-stone-400 opacity-0 group-hover:opacity-100 transition-opacity" />
                        </a>

                        <span className={`text-[9px] font-mono font-bold px-1.5 py-0.2 rounded border ${
                          m.origin === "kiwi_pr"
                            ? "bg-sand-100 text-stone-700 border-sand-200"
                            : "bg-purple-50 text-purple-800 border-purple-200"
                        }`}>
                          {m.origin === "kiwi_pr" ? "Kiwi PR" : "External"}
                        </span>
                      </div>

                      <div className="flex items-center gap-2.5 text-[10px] font-mono text-stone-400 flex-wrap">
                        <span>ID: <span className="text-stone-600 font-semibold">{m.id}</span></span>
                        {m.merge_commit_sha && (
                          <button
                            onClick={() => copySha(m.merge_commit_sha)}
                            className="flex items-center gap-1 text-stone-600 hover:text-stone-900 transition-colors cursor-pointer"
                            title="Copy Commit SHA"
                          >
                            <GitCommit className="w-3 h-3 text-stone-400" />
                            <span>{m.merge_commit_sha.slice(0, 7)}</span>
                            {copiedSha === m.merge_commit_sha ? (
                              <Check className="w-2.5 h-2.5 text-emerald-600" />
                            ) : (
                              <Copy className="w-2.5 h-2.5 text-stone-400" />
                            )}
                          </button>
                        )}
                      </div>
                    </div>

                    <div className="flex items-center gap-2 shrink-0">
                      {/* Status Badge */}
                      <span
                        className={`px-2 py-0.5 rounded-lg text-[10px] font-mono font-bold border flex items-center gap-1 ${
                          isMonitoring
                            ? "bg-sky-50 text-sky-800 border-sky-200"
                            : isVerified
                            ? "bg-emerald-50 text-emerald-800 border-emerald-200"
                            : isRegression
                            ? "bg-rose-50 text-rose-800 border-rose-200"
                            : "bg-sand-100 text-stone-600 border-sand-200"
                        }`}
                      >
                        {isMonitoring ? (
                          <span className="w-1.5 h-1.5 rounded-full bg-sky-500 animate-pulse" />
                        ) : isVerified ? (
                          <CheckCircle2 className="w-3 h-3 text-emerald-600" />
                        ) : isRegression ? (
                          <XCircle className="w-3 h-3 text-rose-600" />
                        ) : (
                          <Ban className="w-3 h-3 text-stone-400" />
                        )}
                        <span>{m.status}</span>
                      </span>

                      {/* Delete / Cancel Button */}
                      <button
                        onClick={() => handleDeleteOrCancel(m.id, isMonitoring)}
                        disabled={cancellingId === m.id}
                        className="p-1 rounded-lg hover:bg-rose-50 text-stone-400 hover:text-rose-700 border border-sand-200 hover:border-rose-200 transition-all shadow-2xs cursor-pointer"
                        title={isMonitoring ? "Cancel and Stop Watchdog" : "Delete Watchdog Record"}
                      >
                        {cancellingId === m.id ? (
                          <KiwiMicroButtonLoader />
                        ) : (
                          <Trash2 className="w-3.5 h-3.5" />
                        )}
                      </button>
                    </div>
                  </div>

                  {/* Middle Row: Observation Progress Meter */}
                  <div className="p-2.5 rounded-xl bg-sand-50/70 border border-sand-200 space-y-1.5">
                    <div className="flex items-center justify-between text-[11px] font-mono">
                      <span className="text-stone-600 flex items-center gap-1">
                        <Clock className="w-3 h-3 text-stone-400" />
                        <span>Observation Window</span>
                      </span>

                      <span className={`font-bold ${
                        isMonitoring ? "text-sky-800" : isRegression ? "text-rose-800" : "text-stone-700"
                      }`}>
                        {expiry.text} ({expiry.remainingText})
                      </span>
                    </div>

                    <div className="w-full h-1.5 bg-sand-200 rounded-full overflow-hidden">
                      <div
                        className={`h-full rounded-full transition-all duration-300 ${
                          isRegression
                            ? "bg-rose-500"
                            : isVerified
                            ? "bg-emerald-500"
                            : isMonitoring
                            ? "bg-sky-500"
                            : "bg-stone-400"
                        }`}
                        style={{ width: `${expiry.percent}%` }}
                      />
                    </div>
                  </div>

                  {/* Bottom Row: Verdict Evidence */}
                  {m.verdict_evidence && (
                    <div className={`p-2.5 rounded-xl text-xs font-mono border ${
                      isRegression
                        ? "bg-rose-50 border-rose-200 text-rose-900"
                        : "bg-sand-50 border-sand-200 text-stone-800"
                    }`}>
                      <div className="font-bold flex items-center gap-1 mb-0.5">
                        {isRegression && <ShieldAlert className="w-3.5 h-3.5 text-rose-600 shrink-0" />}
                        <span>Telemetry Verdict:</span>
                      </div>
                      <p className="leading-relaxed text-[11px]">{m.verdict_evidence}</p>
                    </div>
                  )}
                </div>
              );
            })}
          </div>
        )}
      </div>
    </div>
  );
}
