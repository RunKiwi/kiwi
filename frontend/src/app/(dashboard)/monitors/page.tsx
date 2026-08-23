"use client";

import React, { useEffect, useState, useMemo, useCallback } from "react";
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
} from "lucide-react";
import { KiwiMicroButtonLoader } from "@/components/KiwiLoaders";
import { LoadingState } from "@/components/LoadingState";
import { Logo } from "@/components/Logo";

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

  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect
    load();
    const interval = setInterval(() => {
      setNow(Date.now());
      load();
    }, 10000);
    return () => clearInterval(interval);
  }, [load]);

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
    <div className="max-w-6xl mx-auto flex flex-col gap-6 w-full font-sans text-stone-900">
      {/* ================= HERO HEADER WITH ANIMATED MASCOT ================= */}
      <div className="relative overflow-hidden p-6 rounded-3xl border border-sand-200 bg-gradient-to-r from-sand-100/90 via-white to-sky-50/70 backdrop-blur-xl flex flex-wrap items-center justify-between gap-4 shadow-2xs group">
        <div
          className="absolute inset-0 opacity-[0.035] pointer-events-none"
          style={{
            backgroundImage: `url("data:image/svg+xml,%3Csvg viewBox='0 0 200 200' xmlns='http://www.w3.org/2000/svg'%3E%3Cfilter id='n'%3E%3CfeTurbulence type='fractalNoise' baseFrequency='0.8' numOctaves='3'/%3E%3C/filter%3E%3Crect width='100%25' height='100%25' filter='url(%23n)'/%3E%3C/svg%3E")`,
          }}
        />
        <div className="absolute -top-12 -right-12 w-36 h-36 bg-sky-400/20 rounded-full blur-3xl group-hover:scale-110 transition-transform" />

        <div className="relative z-10 flex items-center gap-3.5">
          <div className="w-12 h-12 rounded-2xl bg-white border border-sand-200/90 shadow-2xs flex items-center justify-center shrink-0">
            <Logo variant="full-color" pose="guarding" animated={true} className="w-8 h-8" />
          </div>
          <div>
            <h1 className="text-xl font-bold tracking-tight text-stone-900 flex items-center gap-2.5">
              <span>PR Watchdogs</span>
              <span className="text-xs font-mono font-bold bg-sky-100 text-sky-900 border border-sky-200 px-2 py-0.5 rounded-md flex items-center gap-1.5">
                <Radar className="w-3.5 h-3.5 text-sky-600 animate-pulse" />
                <span>{stats.active} Active Canary Watchdogs</span>
              </span>
            </h1>
            <p className="text-xs text-stone-600 mt-0.5 max-w-2xl leading-relaxed">
              Continuous post-merge telemetry monitors tracking p99 latency, error rates, and canary regressions in production for up to 24 hours.
            </p>
          </div>
        </div>

        <div className="relative z-10 flex items-center gap-2">
          <button
            onClick={load}
            className="px-3.5 py-2 rounded-xl bg-white hover:bg-sand-100 border border-sand-200 text-stone-700 font-semibold text-xs shadow-2xs flex items-center gap-1.5 transition-all cursor-pointer"
          >
            <Activity className="w-3.5 h-3.5 text-stone-500" />
            <span>Poll Telemetry</span>
          </button>
        </div>
      </div>

      {error && (
        <div className="p-3.5 rounded-2xl bg-rose-50 border border-rose-200 text-rose-800 text-xs flex items-center gap-2 shadow-2xs">
          <AlertTriangle className="w-4 h-4 text-rose-600 shrink-0" />
          <span>{error}</span>
        </div>
      )}

      {/* ================= TOP 4 KPI TILES (HYBRID FROSTED + LIGHT AURA + SPARKLINES) ================= */}
      <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-3.5">
        {/* KPI 1 */}
        <div className="relative overflow-hidden p-4 rounded-2xl bg-white/85 backdrop-blur-xl border border-sand-200 shadow-2xs hover:border-sky-300 hover:shadow-island transition-all group flex flex-col justify-between">
          <div
            className="absolute inset-0 opacity-[0.03] pointer-events-none"
            style={{
              backgroundImage: `url("data:image/svg+xml,%3Csvg viewBox='0 0 200 200' xmlns='http://www.w3.org/2000/svg'%3E%3Cfilter id='n'%3E%3CfeTurbulence type='fractalNoise' baseFrequency='0.8' numOctaves='3'/%3E%3C/filter%3E%3Crect width='100%25' height='100%25' filter='url(%23n)'/%3E%3C/svg%3E")`,
            }}
          />
          <div className="absolute -top-8 -right-8 w-20 h-20 bg-sky-400/20 rounded-full blur-2xl group-hover:scale-125 transition-transform" />

          <div className="relative z-10 flex items-center justify-between text-xs text-stone-600 font-medium">
            <span>Active Monitoring</span>
            <Radar className="w-4 h-4 text-sky-500" />
          </div>
          <div className="relative z-10 mt-2">
            <div className="text-2xl font-bold font-mono text-sky-900">{stats.active}</div>
            <div className="text-[10px] text-stone-400 font-mono mt-0.5">Sampling live production signals</div>
          </div>
          <div className="relative z-10 mt-2 flex items-end gap-1 h-3.5">
            {[40, 60, 45, 80, 50, 90, 75].map((h, i) => (
              <div key={i} className="flex-1 bg-sky-200 rounded-2xs" style={{ height: `${h}%` }} />
            ))}
          </div>
        </div>

        {/* KPI 2 */}
        <div className="relative overflow-hidden p-4 rounded-2xl bg-white/85 backdrop-blur-xl border border-sand-200 shadow-2xs hover:border-emerald-300 hover:shadow-island transition-all group flex flex-col justify-between">
          <div
            className="absolute inset-0 opacity-[0.03] pointer-events-none"
            style={{
              backgroundImage: `url("data:image/svg+xml,%3Csvg viewBox='0 0 200 200' xmlns='http://www.w3.org/2000/svg'%3E%3Cfilter id='n'%3E%3CfeTurbulence type='fractalNoise' baseFrequency='0.8' numOctaves='3'/%3E%3C/filter%3E%3Crect width='100%25' height='100%25' filter='url(%23n)'/%3E%3C/svg%3E")`,
            }}
          />
          <div className="absolute -top-8 -right-8 w-20 h-20 bg-emerald-400/20 rounded-full blur-2xl group-hover:scale-125 transition-transform" />

          <div className="relative z-10 flex items-center justify-between text-xs text-stone-600 font-medium">
            <span>Verified Releases</span>
            <CheckCircle2 className="w-4 h-4 text-emerald-500" />
          </div>
          <div className="relative z-10 mt-2">
            <div className="text-2xl font-bold font-mono text-emerald-900">{stats.verified}</div>
            <div className="text-[10px] text-stone-400 font-mono mt-0.5">Passed telemetry threshold</div>
          </div>
          <div className="relative z-10 mt-2 flex items-end gap-1 h-3.5">
            {[80, 85, 90, 92, 95, 98, 100].map((h, i) => (
              <div key={i} className="flex-1 bg-emerald-200 rounded-2xs" style={{ height: `${h}%` }} />
            ))}
          </div>
        </div>

        {/* KPI 3 */}
        <div className="relative overflow-hidden p-4 rounded-2xl bg-white/85 backdrop-blur-xl border border-sand-200 shadow-2xs hover:border-rose-300 hover:shadow-island transition-all group flex flex-col justify-between">
          <div
            className="absolute inset-0 opacity-[0.03] pointer-events-none"
            style={{
              backgroundImage: `url("data:image/svg+xml,%3Csvg viewBox='0 0 200 200' xmlns='http://www.w3.org/2000/svg'%3E%3Cfilter id='n'%3E%3CfeTurbulence type='fractalNoise' baseFrequency='0.8' numOctaves='3'/%3E%3C/filter%3E%3Crect width='100%25' height='100%25' filter='url(%23n)'/%3E%3C/svg%3E")`,
            }}
          />
          <div className="absolute -top-8 -right-8 w-20 h-20 bg-rose-400/20 rounded-full blur-2xl group-hover:scale-125 transition-transform" />

          <div className="relative z-10 flex items-center justify-between text-xs text-stone-600 font-medium">
            <span>Regressions Detected</span>
            <XCircle className="w-4 h-4 text-rose-500" />
          </div>
          <div className="relative z-10 mt-2">
            <div className="text-2xl font-bold font-mono text-rose-900">{stats.regressions}</div>
            <div className="text-[10px] text-stone-400 font-mono mt-0.5">Anomaly detected in window</div>
          </div>
          <div className="relative z-10 mt-2 flex items-end gap-1 h-3.5">
            {[0, 0, 0, 0, 0, 0, stats.regressions > 0 ? 80 : 0].map((h, i) => (
              <div key={i} className="flex-1 bg-rose-200 rounded-2xs" style={{ height: `${h}%` }} />
            ))}
          </div>
        </div>

        {/* KPI 4 */}
        <div className="relative overflow-hidden p-4 rounded-2xl bg-white/85 backdrop-blur-xl border border-sand-200 shadow-2xs hover:border-amber-300 hover:shadow-island transition-all group flex flex-col justify-between">
          <div
            className="absolute inset-0 opacity-[0.03] pointer-events-none"
            style={{
              backgroundImage: `url("data:image/svg+xml,%3Csvg viewBox='0 0 200 200' xmlns='http://www.w3.org/2000/svg'%3E%3Cfilter id='n'%3E%3CfeTurbulence type='fractalNoise' baseFrequency='0.8' numOctaves='3'/%3E%3C/filter%3E%3Crect width='100%25' height='100%25' filter='url(%23n)'/%3E%3C/svg%3E")`,
            }}
          />
          <div className="absolute -top-8 -right-8 w-20 h-20 bg-amber-400/20 rounded-full blur-2xl group-hover:scale-125 transition-transform" />

          <div className="relative z-10 flex items-center justify-between text-xs text-stone-600 font-medium">
            <span>Monitoring Window</span>
            <Clock className="w-4 h-4 text-amber-500" />
          </div>
          <div className="relative z-10 mt-2">
            <div className="text-2xl font-bold font-mono text-stone-900">24 Hours</div>
            <div className="text-[10px] text-stone-400 font-mono mt-0.5">Automated canary observation</div>
          </div>
          <div className="relative z-10 mt-2 flex items-end gap-1 h-3.5">
            {[50, 60, 70, 80, 85, 90, 100].map((h, i) => (
              <div key={i} className="flex-1 bg-amber-200 rounded-2xs" style={{ height: `${h}%` }} />
            ))}
          </div>
        </div>
      </div>

      {/* ================= ADD WATCHDOG FORM CARD ================= */}
      <div className="p-5 rounded-2xl border border-sand-200 bg-white shadow-2xs space-y-3">
        <div>
          <h2 className="text-sm font-bold text-stone-900 uppercase tracking-wider flex items-center gap-2">
            <Plus className="w-4 h-4 text-stone-600" />
            <span>Attach Post-Merge PR Watchdog</span>
          </h2>
          <p className="text-xs text-stone-500 mt-0.5">
            Input a merged GitHub pull request URL. Kiwi will continuously query Datadog and Prometheus metrics for regressions.
          </p>
        </div>

        <form onSubmit={handleCreate} className="flex flex-wrap sm:flex-nowrap gap-2.5 pt-1">
          <div className="relative flex-1">
            <GitPullRequest className="w-3.5 h-3.5 text-stone-400 absolute left-3 top-3 pointer-events-none" />
            <input
              value={prUrl}
              onChange={(e) => setPrUrl(e.target.value)}
              placeholder="https://github.com/owner/repository/pull/123"
              aria-label="Pull request URL"
              className="w-full pl-8 pr-3 py-2 rounded-xl border border-sand-200 bg-sand-50/60 text-xs font-mono text-stone-900 placeholder:text-stone-400 focus:outline-none focus:border-stone-400 focus:bg-white transition-all shadow-2xs"
            />
          </div>

          <button
            type="submit"
            disabled={creating || !prUrl.trim()}
            className="px-4 py-2 rounded-xl bg-stone-900 hover:bg-stone-800 text-white text-xs font-bold flex items-center justify-center gap-1.5 disabled:opacity-50 transition-all shadow-2xs shrink-0"
          >
            {creating ? <KiwiMicroButtonLoader /> : <Plus className="w-3.5 h-3.5 text-kiwi-400 stroke-[2.5]" />}
            <span>Deploy Watchdog</span>
          </button>
        </form>

        {createError && (
          <div className="text-xs text-rose-700 flex items-center gap-1.5 pt-1 font-mono">
            <AlertTriangle className="w-3.5 h-3.5 shrink-0 text-rose-600" />
            <span>{createError}</span>
          </div>
        )}
      </div>

      {/* ================= MONITORS ROSTER & FILTERS ================= */}
      <div className="bg-white border border-sand-200 rounded-2xl shadow-2xs p-5 sm:p-6 space-y-4">
        {/* Search & Status Filters */}
        <div className="flex flex-wrap items-center justify-between gap-3">
          <div>
            <h3 className="text-sm font-bold text-stone-900 uppercase tracking-wider flex items-center gap-2">
              <Layers className="w-4 h-4 text-stone-600" />
              <span>Watchdogs Roster ({filteredMonitors.length})</span>
            </h3>
            <p className="text-xs text-stone-500 mt-0.5">Automated telemetry evaluations and canary observation windows.</p>
          </div>

          <div className="flex items-center gap-2 flex-wrap text-xs">
            <div className="relative">
              <Search className="w-3.5 h-3.5 text-stone-400 absolute left-3 top-2.5 pointer-events-none" />
              <input
                type="text"
                value={searchQuery}
                onChange={(e) => setSearchQuery(e.target.value)}
                placeholder="Search repo, PR #, or ID..."
                className="bg-sand-50/70 border border-sand-200 rounded-xl pl-8 pr-3 py-1.5 text-xs placeholder:text-stone-400 focus:outline-none focus:border-stone-400 focus:bg-white transition-all font-mono min-w-[180px]"
              />
            </div>

            <div className="flex items-center gap-1 bg-sand-100 p-0.5 rounded-xl border border-sand-200">
              <button
                onClick={() => setStatusFilter("all")}
                className={`px-2.5 py-1 rounded-lg font-semibold transition-all ${
                  statusFilter === "all" ? "bg-white text-stone-900 shadow-2xs" : "text-stone-600 hover:text-stone-900"
                }`}
              >
                All
              </button>
              <button
                onClick={() => setStatusFilter("monitoring")}
                className={`px-2.5 py-1 rounded-lg font-semibold transition-all ${
                  statusFilter === "monitoring" ? "bg-white text-sky-800 shadow-2xs" : "text-stone-600 hover:text-stone-900"
                }`}
              >
                Monitoring
              </button>
              <button
                onClick={() => setStatusFilter("verified")}
                className={`px-2.5 py-1 rounded-lg font-semibold transition-all ${
                  statusFilter === "verified" ? "bg-white text-emerald-800 shadow-2xs" : "text-stone-600 hover:text-stone-900"
                }`}
              >
                Verified
              </button>
              <button
                onClick={() => setStatusFilter("regression")}
                className={`px-2.5 py-1 rounded-lg font-semibold transition-all ${
                  statusFilter === "regression" ? "bg-white text-rose-800 shadow-2xs" : "text-stone-600 hover:text-stone-900"
                }`}
              >
                Regression
              </button>
            </div>
          </div>
        </div>

        {/* List of Watchdogs */}
        {loading ? (
          <LoadingState state="searching" size={48} label="Loading telemetry monitors..." className="py-12" />
        ) : filteredMonitors.length === 0 ? (
          <div className="relative overflow-hidden p-10 rounded-2xl border border-sand-200 bg-white/80 backdrop-blur-xl text-center space-y-3 shadow-2xs group">
            <div
              className="absolute inset-0 opacity-[0.035] pointer-events-none"
              style={{
                backgroundImage: `url("data:image/svg+xml,%3Csvg viewBox='0 0 200 200' xmlns='http://www.w3.org/2000/svg'%3E%3Cfilter id='n'%3E%3CfeTurbulence type='fractalNoise' baseFrequency='0.8' numOctaves='3'/%3E%3C/filter%3E%3Crect width='100%25' height='100%25' filter='url(%23n)'/%3E%3C/svg%3E")`,
              }}
            />
            <div className="absolute -top-10 -right-10 w-28 h-28 bg-sky-400/15 rounded-full blur-2xl pointer-events-none" />

            <div className="relative z-10 w-14 h-14 mx-auto rounded-2xl bg-sand-50 border border-sand-200/80 shadow-2xs flex items-center justify-center">
              <Logo variant="full-color" pose="guarding" animated={true} className="w-8 h-8" />
            </div>
            <div className="relative z-10 space-y-1">
              <div className="text-stone-900 font-bold text-sm">No PR Watchdogs Found</div>
              <p className="text-xs text-stone-500 max-w-xs mx-auto">
                Attach a merged pull request above to start automated telemetry and error rate monitoring.
              </p>
            </div>
          </div>
        ) : (
          <div className="grid grid-cols-1 gap-3.5">
            {filteredMonitors.map((m) => {
              const isMonitoring = m.status === "MONITORING";
              const isVerified = m.status === "VERIFIED";
              const isRegression = m.status === "REGRESSION";
              const expiry = getExpiryInfo(m);
              const prHref = `https://github.com/${m.repo}/pull/${m.pr_number}`;

              return (
                <div
                  key={m.id}
                  className={`p-4 rounded-2xl bg-white border shadow-2xs transition-all flex flex-col justify-between gap-3.5 ${
                    isRegression
                      ? "border-rose-300 ring-1 ring-rose-200"
                      : isMonitoring
                      ? "border-sky-300"
                      : "border-sand-200"
                  }`}
                >
                  {/* Top Row: PR Title, Origin, Status Badge, & Delete Button */}
                  <div className="flex flex-wrap items-start justify-between gap-3">
                    <div className="min-w-0 space-y-1">
                      <div className="flex items-center gap-2 flex-wrap">
                        <a
                          href={prHref}
                          target="_blank"
                          rel="noreferrer"
                          className="text-sm font-bold text-stone-900 hover:text-kiwi-700 flex items-center gap-1.5 transition-colors group"
                        >
                          <GitPullRequest className="w-4 h-4 text-stone-600 group-hover:text-kiwi-600" />
                          <span>{m.repo} #{m.pr_number}</span>
                          <ExternalLink className="w-3 h-3 text-stone-400 opacity-0 group-hover:opacity-100 transition-opacity" />
                        </a>

                        <span className={`text-[10px] font-mono font-bold px-2 py-0.5 rounded-full border ${
                          m.origin === "kiwi_pr"
                            ? "bg-sand-100 text-stone-700 border-sand-200"
                            : "bg-purple-50 text-purple-800 border-purple-200"
                        }`}>
                          {m.origin === "kiwi_pr" ? "Kiwi Automated PR" : "External PR"}
                        </span>
                      </div>

                      <div className="flex items-center gap-3 text-[11px] font-mono text-stone-400 flex-wrap">
                        <span>Watchdog ID: <span className="text-stone-600 font-semibold">{m.id}</span></span>
                        {m.merge_commit_sha && (
                          <button
                            onClick={() => copySha(m.merge_commit_sha)}
                            className="flex items-center gap-1 text-stone-600 hover:text-stone-900 transition-colors"
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

                    <div className="flex items-center gap-2.5 shrink-0">
                      {/* Status Badge */}
                      <span
                        className={`px-2.5 py-1 rounded-full text-[11px] font-mono font-bold border flex items-center gap-1.5 ${
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
                        className="p-1.5 rounded-xl hover:bg-rose-50 text-stone-400 hover:text-rose-700 border border-sand-200 hover:border-rose-200 transition-all shadow-2xs"
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

                  {/* Middle Row: Expiry Progress Meter & Timestamps */}
                  <div className="p-3 rounded-xl bg-sand-50/70 border border-sand-200 space-y-2">
                    <div className="flex items-center justify-between text-xs font-mono">
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

                  {/* Bottom Row: Verdict Evidence (if regression or verified details exist) */}
                  {m.verdict_evidence && (
                    <div className={`p-3 rounded-xl text-xs font-mono border ${
                      isRegression
                        ? "bg-rose-50 border-rose-200 text-rose-900"
                        : "bg-sand-50 border-sand-200 text-stone-800"
                    }`}>
                      <div className="font-bold flex items-center gap-1 mb-0.5">
                        {isRegression && <ShieldAlert className="w-3.5 h-3.5 text-rose-600 shrink-0" />}
                        <span>Telemetry Verdict:</span>
                      </div>
                      <p className="leading-relaxed">{m.verdict_evidence}</p>
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
