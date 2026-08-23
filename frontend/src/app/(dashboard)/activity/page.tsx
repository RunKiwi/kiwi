"use client";

import React, { useCallback, useMemo, useState } from "react";
import { client, type Fleet, type Daemon, type JobSummary, type RecordStep } from "@/lib/api";
import { TaskDrawer } from "@/components/TaskDrawer";
import { ActivityTimeline, type ActivityLaneData, type ActivityBar } from "@/components/ActivityTimeline";
import { usePolling } from "@/hooks/usePolling";
import {
  buildLanes,
  laneForJob,
  jobsInWindow,
  defaultWindow,
  buildPhaseSegments,
  barExtent,
  assignSubRows,
  computeHeaderStats,
  isTerminalStatus,
} from "@/lib/jobActivity";
import {
  CheckCircle2,
  AlertCircle,
  Clock,
  RefreshCw,
  Search,
  ArrowRight,
  Zap,
  Check,
  XCircle,
  Play,
  RotateCcw,
} from "lucide-react";
import { statusOf } from "@/lib/statusColors";
import { Logo } from "@/components/Logo";

const POLL_MS = 5000;

export default function ActivityPage() {
  const [fleets, setFleets] = useState<Fleet[]>([]);
  const [daemons, setDaemons] = useState<Daemon[]>([]);
  const [jobs, setJobs] = useState<JobSummary[]>([]);
  const [stepsByJob, setStepsByJob] = useState<Map<string, RecordStep[]>>(new Map());
  const [selectedJobId, setSelectedJobId] = useState<string | null>(null);
  const [windowHours, setWindowHours] = useState<number>(2);
  const [searchQuery, setSearchQuery] = useState("");
  const [statusFilter, setStatusFilter] = useState<string>("all");
  const [isRefreshing, setIsRefreshing] = useState(false);
  const [now, setNow] = useState(() => Date.now());

  const load = useCallback(async () => {
    setIsRefreshing(true);
    try {
      const [fleetsRes, daemonsRes, jobsRes] = await Promise.all([
        client.listFleets().catch(() => ({ fleets: [] as Fleet[] })),
        client.listDaemons().catch(() => [] as Daemon[]),
        client.listJobs().catch(() => ({ jobs: [] as JobSummary[] })),
      ]);

      const fl = fleetsRes.fleets || [];
      const da = Array.isArray(daemonsRes) ? daemonsRes : [];
      const jb = (jobsRes.jobs || []).map((j) => ({
        ...j,
        job_id: j.job_id || (j as { id?: string }).id || "",
      }));

      setFleets(fl);
      setDaemons(da);
      setJobs(jb);
      setNow(Date.now());

      const inWindow = jobsInWindow(jb, defaultWindow(Date.now(), windowHours));
      const entries = await Promise.all(
        inWindow.map(async (job) => {
          const progress = await client.getJobProgress(job.job_id).catch(() => null);
          const steps = (progress?.tasks ?? []).flatMap((t) => t.steps ?? []);
          return [job.job_id, steps] as const;
        }),
      );
      setStepsByJob(new Map(entries));
    } catch {
      // non-fatal
    } finally {
      setIsRefreshing(false);
    }
  }, [windowHours]);

  const headerStats = useMemo(() => computeHeaderStats(jobs), [jobs]);

  usePolling(load, {
    activeIntervalMs: POLL_MS,
    idleIntervalMs: 20000,
    isIdle: headerStats.runningNow === 0,
  });

  const window_ = useMemo(() => defaultWindow(now, windowHours), [now, windowHours]);

  const lanes = useMemo<ActivityLaneData[]>(() => {
    const visibleJobs = jobsInWindow(jobs, window_);
    const laneDefs = buildLanes(daemons, fleets, visibleJobs);
    const daemonIds = new Set(daemons.map((d) => d.id));
    const spanMs = Math.max(1, window_.endMs - window_.startMs);

    interface Prepared {
      jobId: string;
      label: string;
      status: string;
      isActive: boolean;
      startMs: number;
      endMs: number;
      segments: ActivityBar["segments"];
    }
    const byLane = new Map<string, Prepared[]>();

    for (const job of visibleJobs) {
      const laneId = laneForJob(job, daemonIds);
      const steps = stepsByJob.get(job.job_id) ?? [];
      const isActive = !isTerminalStatus(job.status);
      const rawSegments = buildPhaseSegments(steps, { nowMs: now, isActive });
      const extent = barExtent(job, rawSegments, window_, now);
      const barSpan = Math.max(1, extent.endMs - extent.startMs);

      const segments = rawSegments
        .map((seg) => {
          const segStart = Math.max(seg.startMs, extent.startMs);
          const segEnd = Math.min(seg.endMs, extent.endMs);
          if (segEnd <= segStart) return null;
          return {
            category: seg.category,
            startPct: ((segStart - extent.startMs) / barSpan) * 100,
            widthPct: ((segEnd - segStart) / barSpan) * 100,
          };
        })
        .filter((s): s is NonNullable<typeof s> => s !== null);

      const list = byLane.get(laneId) ?? [];
      list.push({
        jobId: job.job_id,
        label: job.task ? (job.task.length > 60 ? `${job.task.slice(0, 60)}…` : job.task) : job.job_id,
        status: job.status,
        isActive,
        startMs: extent.startMs,
        endMs: extent.endMs,
        segments,
      });
      byLane.set(laneId, list);
    }

    return laneDefs.map((lane) => {
      const prepared = byLane.get(lane.laneId) ?? [];
      const subRows = assignSubRows(prepared.map((p) => ({ id: p.jobId, startMs: p.startMs, endMs: p.endMs })));
      const rowCount = prepared.length > 0 ? Math.max(...prepared.map((p) => subRows.get(p.jobId) ?? 0)) + 1 : 1;

      const bars: ActivityBar[] = prepared.map((p) => ({
        jobId: p.jobId,
        label: p.label,
        status: p.status,
        subRow: subRows.get(p.jobId) ?? 0,
        startPct: ((p.startMs - window_.startMs) / spanMs) * 100,
        widthPct: (Math.max(1, p.endMs - p.startMs) / spanMs) * 100,
        segments: p.segments,
        isActive: p.isActive,
      }));

      return { lane, bars, rowCount };
    });
  }, [jobs, daemons, fleets, stepsByJob, window_, now]);

  // Filtered list of jobs for the activity stream table
  const filteredJobs = useMemo(() => {
    const q = searchQuery.trim().toLowerCase();
    return jobs.filter((j) => {
      const matchesQuery =
        !q ||
        (j.task && j.task.toLowerCase().includes(q)) ||
        j.job_id.toLowerCase().includes(q) ||
        (j.repo && j.repo.toLowerCase().includes(q));

      const matchesStatus =
        statusFilter === "all" ||
        (statusFilter === "running" && !isTerminalStatus(j.status)) ||
        (statusFilter === "succeeded" && j.status?.toUpperCase() === "SUCCEEDED") ||
        (statusFilter === "failed" && (j.status?.toUpperCase() === "FAILED" || j.status?.toUpperCase() === "CANCELLED"));

      return matchesQuery && matchesStatus;
    });
  }, [jobs, searchQuery, statusFilter]);

  return (
    <div className="max-w-6xl mx-auto flex flex-col gap-6 w-full font-sans text-stone-900">
      {/* ================= HERO HEADER WITH ANIMATED MASCOT ================= */}
      <div className="relative overflow-hidden p-6 rounded-3xl border border-sand-200 bg-gradient-to-r from-sand-100/90 via-white to-emerald-50/70 backdrop-blur-xl flex flex-wrap items-center justify-between gap-4 shadow-2xs group">
        <div
          className="absolute inset-0 opacity-[0.035] pointer-events-none"
          style={{
            backgroundImage: `url("data:image/svg+xml,%3Csvg viewBox='0 0 200 200' xmlns='http://www.w3.org/2000/svg'%3E%3Cfilter id='n'%3E%3CfeTurbulence type='fractalNoise' baseFrequency='0.8' numOctaves='3'/%3E%3C/filter%3E%3Crect width='100%25' height='100%25' filter='url(%23n)'/%3E%3C/svg%3E")`,
          }}
        />
        <div className="absolute -top-12 -right-12 w-36 h-36 bg-emerald-400/20 rounded-full blur-3xl group-hover:scale-110 transition-transform" />

        <div className="relative z-10 flex items-center gap-3.5">
          <div className="w-12 h-12 rounded-2xl bg-white border border-sand-200/90 shadow-2xs flex items-center justify-center shrink-0">
            <Logo variant="full-color" pose="vibing" animated={true} className="w-8 h-8" />
          </div>
          <div>
            <h1 className="text-xl font-bold tracking-tight text-stone-900 flex items-center gap-2.5">
              <span>Activity Log</span>
              <span className="text-xs font-mono font-bold bg-emerald-100 text-emerald-900 border border-emerald-200 px-2 py-0.5 rounded-md flex items-center gap-1.5">
                <span className="w-2 h-2 rounded-full bg-emerald-600 animate-pulse" />
                <span>Live Fleet Stream</span>
              </span>
            </h1>
            <p className="text-xs text-stone-600 mt-0.5 max-w-2xl leading-relaxed">
              Live execution traces and telemetry across your private daemons and managed fleets. Phase segments highlight Architect planning, Implementer coding, and sandboxed Verification.
            </p>
          </div>
        </div>

        {/* Time Window Controls & Refresh */}
        <div className="relative z-10 flex items-center gap-2 flex-wrap">
          <div className="flex items-center gap-1 bg-sand-100/90 p-0.5 rounded-xl border border-sand-200 text-xs">
            {[1, 2, 6, 24].map((h) => (
              <button
                key={h}
                onClick={() => setWindowHours(h)}
                className={`px-2.5 py-1 rounded-lg font-semibold transition-all cursor-pointer ${
                  windowHours === h ? "bg-white text-stone-900 shadow-2xs" : "text-stone-600 hover:text-stone-900"
                }`}
              >
                {h}h Window
              </button>
            ))}
          </div>

          <button
            onClick={load}
            disabled={isRefreshing}
            className="px-3.5 py-2 rounded-xl bg-white hover:bg-sand-100 border border-sand-200 text-stone-700 font-semibold text-xs shadow-2xs flex items-center gap-1.5 transition-all cursor-pointer"
            title="Refresh activity data"
          >
            <RefreshCw className={`w-3.5 h-3.5 ${isRefreshing ? "animate-spin text-emerald-600" : "text-stone-500"}`} />
            <span>Refresh</span>
          </button>
        </div>
      </div>

      {/* ================= 4 KPI METRIC TILES (HYBRID FROSTED + LIGHT AURA + SPARKLINES) ================= */}
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
            <span>Running Tasks</span>
            <Play className="w-4 h-4 text-sky-500 fill-current" />
          </div>
          <div className="relative z-10 mt-2">
            <div className="text-2xl font-bold font-mono text-stone-900">{headerStats.runningNow}</div>
            <div className="text-[10px] text-stone-400 font-mono mt-0.5">
              {headerStats.runningNow > 0 ? "Active in sandbox" : "Fleet idle"}
            </div>
          </div>
          <div className="relative z-10 mt-2 flex items-end gap-1 h-3.5">
            {[20, 40, 30, 60, 50, 70, headerStats.runningNow > 0 ? 90 : 30].map((h, i) => (
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
            <span>Completed Today</span>
            <CheckCircle2 className="w-4 h-4 text-emerald-500" />
          </div>
          <div className="relative z-10 mt-2">
            <div className="text-2xl font-bold font-mono text-emerald-800">{headerStats.completedToday}</div>
            <div className="text-[10px] text-stone-400 font-mono mt-0.5">Successful verification runs</div>
          </div>
          <div className="relative z-10 mt-2 flex items-end gap-1 h-3.5">
            {[40, 55, 65, 75, 80, 90, 100].map((h, i) => (
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
            <span>Failed Today</span>
            <XCircle className="w-4 h-4 text-rose-500" />
          </div>
          <div className="relative z-10 mt-2">
            <div className="text-2xl font-bold font-mono text-rose-800">{headerStats.failedToday}</div>
            <div className="text-[10px] text-stone-400 font-mono mt-0.5">Requires engineer review</div>
          </div>
          <div className="relative z-10 mt-2 flex items-end gap-1 h-3.5">
            {[0, 0, 0, 0, 0, 0, headerStats.failedToday > 0 ? 80 : 0].map((h, i) => (
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
            <span>Success Rate Today</span>
            <Zap className="w-4 h-4 text-amber-500" />
          </div>
          <div className="relative z-10 mt-2">
            <div className="text-2xl font-bold font-mono text-stone-900">
              {headerStats.successRatePct != null ? `${headerStats.successRatePct.toFixed(0)}%` : "N/A"}
            </div>
            <div className="text-[10px] text-stone-400 font-mono mt-0.5">Rolling 24h average</div>
          </div>
          <div className="relative z-10 mt-2 flex items-end gap-1 h-3.5">
            {[80, 85, 90, 95, 95, 98, headerStats.successRatePct ?? 95].map((h, i) => (
              <div key={i} className="flex-1 bg-amber-200 rounded-2xs" style={{ height: `${h}%` }} />
            ))}
          </div>
        </div>
      </div>

      {/* ================= GANTT ACTIVITY TIMELINE ================= */}
      <ActivityTimeline lanes={lanes} window={window_} onSelectJob={setSelectedJobId} />

      {/* ================= RECENT ACTIVITY STREAM & SEARCH ================= */}
      <div className="bg-white border border-sand-200 rounded-2xl shadow-2xs p-5 sm:p-6 space-y-4">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <div>
            <h3 className="text-sm font-bold text-stone-900 uppercase tracking-wider flex items-center gap-2">
              <Clock className="w-4 h-4 text-stone-600" />
              <span>Execution Stream ({filteredJobs.length})</span>
            </h3>
            <p className="text-xs text-stone-500 mt-0.5">Chronological execution history across all active daemons.</p>
          </div>

          {/* Search & Filters */}
          <div className="flex items-center gap-2 flex-wrap text-xs">
            <div className="relative">
              <Search className="w-3.5 h-3.5 text-stone-400 absolute left-3 top-2.5 pointer-events-none" />
              <input
                type="text"
                value={searchQuery}
                onChange={(e) => setSearchQuery(e.target.value)}
                placeholder="Search prompt, repo, or job ID..."
                className="bg-sand-50/70 border border-sand-200 rounded-xl pl-8 pr-3 py-1.5 text-xs placeholder:text-stone-400 focus:outline-none focus:border-stone-400 focus:bg-white transition-all font-mono min-w-[200px]"
              />
            </div>

            <div className="flex items-center gap-1 bg-sand-100 p-0.5 rounded-xl border border-sand-200">
              <button
                onClick={() => setStatusFilter("all")}
                className={`px-2.5 py-1 rounded-lg font-semibold transition-all cursor-pointer ${
                  statusFilter === "all" ? "bg-white text-stone-900 shadow-2xs" : "text-stone-600 hover:text-stone-900"
                }`}
              >
                All
              </button>
              <button
                onClick={() => setStatusFilter("running")}
                className={`px-2.5 py-1 rounded-lg font-semibold transition-all cursor-pointer ${
                  statusFilter === "running" ? "bg-white text-stone-900 shadow-2xs" : "text-stone-600 hover:text-stone-900"
                }`}
              >
                Running
              </button>
              <button
                onClick={() => setStatusFilter("succeeded")}
                className={`px-2.5 py-1 rounded-lg font-semibold transition-all cursor-pointer ${
                  statusFilter === "succeeded" ? "bg-white text-emerald-800 shadow-2xs" : "text-stone-600 hover:text-stone-900"
                }`}
              >
                Succeeded
              </button>
              <button
                onClick={() => setStatusFilter("failed")}
                className={`px-2.5 py-1 rounded-lg font-semibold transition-all cursor-pointer ${
                  statusFilter === "failed" ? "bg-white text-rose-800 shadow-2xs" : "text-stone-600 hover:text-stone-900"
                }`}
              >
                Failed
              </button>
            </div>
          </div>
        </div>

        {/* Task List */}
        {filteredJobs.length === 0 ? (
          <div className="p-8 text-center text-stone-400 font-mono bg-sand-50/40 rounded-xl border border-sand-200">
            No matching execution jobs found.
          </div>
        ) : (
          <div className="divide-y divide-sand-150 border border-sand-200 rounded-xl overflow-hidden">
            {filteredJobs.slice(0, 30).map((job) => {
              const s = statusOf(job.status);
              const isActive = !isTerminalStatus(job.status);
              const daemonName = job.daemon_id
                ? daemons.find((d) => d.id === job.daemon_id)?.id.slice(0, 10) || job.daemon_id.slice(0, 10)
                : "Shared Fleet";

              return (
                <div
                  key={job.job_id}
                  onClick={() => setSelectedJobId(job.job_id)}
                  className="p-3.5 bg-white hover:bg-sand-50/80 transition-colors flex items-center justify-between gap-4 cursor-pointer group"
                >
                  <div className="min-w-0 flex items-start gap-3">
                    <div className="w-8 h-8 rounded-lg bg-sand-100 border border-sand-200 flex items-center justify-center shrink-0 mt-0.5">
                      {isActive ? (
                        <RotateCcw className="w-4 h-4 text-sky-600 animate-spin" />
                      ) : job.status?.toUpperCase() === "SUCCEEDED" ? (
                        <Check className="w-4 h-4 text-emerald-600" />
                      ) : (
                        <AlertCircle className="w-4 h-4 text-rose-600" />
                      )}
                    </div>

                    <div className="min-w-0 space-y-0.5">
                      <div className="text-xs font-bold text-stone-900 truncate group-hover:text-stone-950">
                        {job.task || job.job_id}
                      </div>
                      <div className="flex items-center gap-3 text-[11px] font-mono text-stone-400 flex-wrap">
                        <span>ID: <span className="text-stone-600 font-semibold">{job.job_id.slice(0, 12)}</span></span>
                        {job.repo && <span>Repo: <span className="text-stone-600">{job.repo}</span></span>}
                        <span>Runner: <span className="text-stone-600">{daemonName}</span></span>
                      </div>
                    </div>
                  </div>

                  <div className="flex items-center gap-3 shrink-0">
                    <span
                      className="px-2 py-0.5 rounded-full text-[10px] font-mono font-bold uppercase border"
                      style={{
                        background: s.wash || "#F5F3EF",
                        color: s.color || "#44403C",
                        borderColor: s.border || "#E2DFD9",
                      }}
                    >
                      {s.label}
                    </span>

                    <ArrowRight className="w-3.5 h-3.5 text-stone-400 group-hover:translate-x-0.5 group-hover:text-stone-700 transition-all" />
                  </div>
                </div>
              );
            })}
          </div>
        )}
      </div>

      {/* Task Drawer overlay for in-depth step logs */}
      <TaskDrawer taskId={selectedJobId} onClose={() => setSelectedJobId(null)} />
    </div>
  );
}
