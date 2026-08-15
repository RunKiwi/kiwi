"use client";

import { useCallback, useMemo, useState } from "react";
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

const WINDOW_HOURS = 2;
const POLL_MS = 5000;

export default function ActivityPage() {
  const [fleets, setFleets] = useState<Fleet[]>([]);
  const [daemons, setDaemons] = useState<Daemon[]>([]);
  const [jobs, setJobs] = useState<JobSummary[]>([]);
  const [stepsByJob, setStepsByJob] = useState<Map<string, RecordStep[]>>(new Map());
  const [selectedJobId, setSelectedJobId] = useState<string | null>(null);
  const [now, setNow] = useState(() => Date.now());

  const load = useCallback(async () => {
    const [fleetsRes, daemonsRes, jobsRes] = await Promise.all([
      client.listFleets().catch(() => ({ fleets: [] as Fleet[] })),
      client.listDaemons().catch(() => [] as Daemon[]),
      client.listJobs().catch(() => ({ jobs: [] as JobSummary[] })),
    ]);
    setFleets(fleetsRes.fleets);
    setDaemons(daemonsRes);
    setJobs(jobsRes.jobs);
    setNow(Date.now());

    // Phase detail is fetched only for jobs actually on screen, mirroring the
    // same N+1 pattern TaskDrawer already relies on for a single job — bounded
    // here by the lookback window instead of by one drawer being open.
    const inWindow = jobsInWindow(jobsRes.jobs, defaultWindow(Date.now(), WINDOW_HOURS));
    const entries = await Promise.all(
      inWindow.map(async (job) => {
        const progress = await client.getJobProgress(job.job_id).catch(() => null);
        const steps = (progress?.tasks ?? []).flatMap((t) => t.steps ?? []);
        return [job.job_id, steps] as const;
      }),
    );
    setStepsByJob(new Map(entries));
  }, []);

  const headerStats = useMemo(() => computeHeaderStats(jobs), [jobs]);

  // Idle means nothing is running: back off the poll rate the same way the
  // main job board does, rather than hammering the API on a quiet fleet.
  usePolling(load, { activeIntervalMs: POLL_MS, idleIntervalMs: 20000, isIdle: headerStats.runningNow === 0 });

  const window_ = useMemo(() => defaultWindow(now, WINDOW_HOURS), [now]);

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

  const tiles = [
    { label: "Running now", value: String(headerStats.runningNow) },
    { label: "Completed today", value: String(headerStats.completedToday) },
    { label: "Failed today", value: String(headerStats.failedToday) },
    { label: "Success rate today", value: headerStats.successRatePct === null ? "—" : `${headerStats.successRatePct}%` },
  ];

  return (
    <div className="p-8 max-w-7xl mx-auto h-full flex flex-col text-white">
      <div className="mb-6">
        <h1 className="text-3xl font-light tracking-tight mb-2">Activity</h1>
        <p className="text-zinc-400">
          Live view of what your fleet is doing right now, one lane per daemon. Each bar is a job, segmented by
          who&apos;s driving it — Architect, Implementer, or the sandboxed verify step. Click a bar for the full run.
        </p>
      </div>

      <div className="grid grid-cols-2 lg:grid-cols-4 gap-4 mb-4">
        {tiles.map((t) => (
          <div key={t.label} className="glass-panel p-5">
            <div className="text-2xl font-light text-white">{t.value}</div>
            <div className="text-xs text-zinc-500 uppercase tracking-widest mt-1">{t.label}</div>
          </div>
        ))}
      </div>

      <ActivityTimeline lanes={lanes} window={window_} onSelectJob={setSelectedJobId} />

      <TaskDrawer taskId={selectedJobId} onClose={() => setSelectedJobId(null)} />
    </div>
  );
}
