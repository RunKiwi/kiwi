import type { Daemon, Fleet, JobSummary, RecordStep } from "./api";

const TERMINAL = new Set(["SUCCEEDED", "FAILED", "CANCELLED"]);
export const isTerminalStatus = (status: string) => TERMINAL.has(status?.toUpperCase());

/**
 * Every phase a step can carry, collapsed onto the three roles a viewer needs
 * to tell apart. Both the Architect's planning and its review land on
 * "critic" in the record (see sessionPhase in pkg/daemon/session_run.go), so
 * they share one category here too — the distinction that matters on a
 * timeline is who's acting, not which of the Architect's two jobs it's doing.
 */
export type PhaseCategory = "architect" | "implementer" | "verify" | "other";

export function categorizePhase(phase: string): PhaseCategory {
  const head = phase.includes(":") ? phase.slice(0, phase.indexOf(":")) : phase;
  if (head === "critic") return "architect";
  if (head === "test" || head === "initial_test") return "verify";
  if (head === "actor" || head === "implementer") return "implementer";
  return "other";
}

export interface PhaseSegment {
  category: PhaseCategory;
  startMs: number;
  endMs: number;
}

/**
 * Collapse a step list into contiguous same-category segments. Steps without
 * a parseable `at` are dropped rather than guessed at — a segment boundary
 * invented from nothing is worse than a shorter bar.
 *
 * When `isActive`, the final segment is stretched to `nowMs` so a running
 * job's bar visibly grows between polls instead of stalling at its last step.
 */
export function buildPhaseSegments(
  steps: RecordStep[],
  opts: { nowMs: number; isActive: boolean },
): PhaseSegment[] {
  const timed = steps
    .map((s) => ({ category: categorizePhase(s.phase), atMs: s.at ? Date.parse(s.at) : NaN }))
    .filter((s) => !Number.isNaN(s.atMs))
    .sort((a, b) => a.atMs - b.atMs);

  if (timed.length === 0) return [];

  const segments: PhaseSegment[] = [];
  for (const step of timed) {
    const last = segments[segments.length - 1];
    if (last && last.category === step.category) {
      last.endMs = step.atMs;
    } else {
      segments.push({ category: step.category, startMs: step.atMs, endMs: step.atMs });
    }
  }

  const last = segments[segments.length - 1];
  last.endMs = opts.isActive ? Math.max(opts.nowMs, last.endMs) : last.endMs;
  return segments;
}

export interface JobLane {
  laneId: string;
  label: string;
  kind: "daemon" | "fleet" | "unassigned";
  online?: boolean;
}

/** Which lane a job hangs off: its daemon, else its fleet, else the catch-all. */
export function laneForJob(job: JobSummary, daemonIds: Set<string>): string {
  if (job.daemon_id && daemonIds.has(job.daemon_id)) return `daemon:${job.daemon_id}`;
  if (job.fleet_id) return `fleet:${job.fleet_id}`;
  return "unassigned";
}

/**
 * The fixed, ordered set of lanes to draw: every known daemon (so idle
 * capacity is visible, not just busy capacity), plus a lane for any fleet a
 * job references that has no daemon of its own, plus "Unassigned" only if
 * some job actually lands there.
 */
export function buildLanes(daemons: Daemon[], fleets: Fleet[], jobs: JobSummary[]): JobLane[] {
  const daemonIds = new Set(daemons.map((d) => d.id));
  const fleetById = new Map(fleets.map((f) => [f.id, f]));

  const lanes: JobLane[] = [...daemons]
    .sort((a, b) => Date.parse(a.created_at) - Date.parse(b.created_at))
    .map((d) => ({
      laneId: `daemon:${d.id}`,
      label: d.id.length > 14 ? `${d.id.slice(0, 14)}…` : d.id,
      kind: "daemon" as const,
      online: d.online,
    }));

  const seenFleetLanes = new Set<string>();
  const seenUnassigned = { value: false };
  for (const job of jobs) {
    const laneId = laneForJob(job, daemonIds);
    if (laneId.startsWith("fleet:") && !seenFleetLanes.has(laneId)) {
      seenFleetLanes.add(laneId);
      const fleetId = laneId.slice("fleet:".length);
      lanes.push({ laneId, label: fleetById.get(fleetId)?.name ?? fleetId, kind: "fleet" });
    } else if (laneId === "unassigned") {
      seenUnassigned.value = true;
    }
  }
  if (seenUnassigned.value) {
    lanes.push({ laneId: "unassigned", label: "Unassigned", kind: "unassigned" });
  }

  return lanes;
}

export interface TimeWindow {
  startMs: number;
  endMs: number;
}

export function defaultWindow(nowMs: number, hours = 2): TimeWindow {
  return { startMs: nowMs - hours * 60 * 60 * 1000, endMs: nowMs };
}

/**
 * Jobs to draw: started inside the window, or still running regardless of
 * when they started — a long job that began before the window opened stays
 * visible (clipped to the window edge by barExtent) rather than vanishing.
 */
export function jobsInWindow(jobs: JobSummary[], window: TimeWindow): JobSummary[] {
  return jobs.filter((job) => {
    if (!isTerminalStatus(job.status)) return true;
    const createdMs = Date.parse(job.created_at);
    return !Number.isNaN(createdMs) && createdMs >= window.startMs && createdMs <= window.endMs;
  });
}

export interface BarExtent {
  startMs: number;
  endMs: number;
}

/** A job's bar span, clamped to the visible window. */
export function barExtent(
  job: JobSummary,
  segments: PhaseSegment[],
  window: TimeWindow,
  nowMs: number,
): BarExtent {
  const createdMs = Date.parse(job.created_at);
  const startMs = Math.min(Math.max(createdMs, window.startMs), window.endMs);
  const lastSegmentEnd = segments.length > 0 ? segments[segments.length - 1].endMs : createdMs;
  const naturalEnd = isTerminalStatus(job.status) ? lastSegmentEnd : nowMs;
  const endMs = Math.min(Math.max(naturalEnd, startMs), window.endMs);
  return { startMs, endMs };
}

/**
 * Greedy interval-scheduling stack: a daemon runs its leased tasks
 * concurrently (pkg/daemon/daemon.go fans out res.Specs in parallel
 * goroutines), so two bars in the same lane can overlap in time and need
 * separate sub-rows rather than being drawn on top of each other.
 */
export function assignSubRows<T extends { id: string; startMs: number; endMs: number }>(
  bars: T[],
): Map<string, number> {
  const rows: number[] = []; // last endMs placed in each row
  const assignment = new Map<string, number>();
  const sorted = [...bars].sort((a, b) => a.startMs - b.startMs);
  for (const bar of sorted) {
    let row = rows.findIndex((endMs) => endMs <= bar.startMs);
    if (row === -1) {
      row = rows.length;
      rows.push(bar.endMs);
    } else {
      rows[row] = bar.endMs;
    }
    assignment.set(bar.id, row);
  }
  return assignment;
}

export interface HeaderStats {
  runningNow: number;
  completedToday: number;
  failedToday: number;
  successRatePct: number | null;
}

export function computeHeaderStats(jobs: JobSummary[], referenceDate: Date = new Date()): HeaderStats {
  const startOfToday = new Date(referenceDate);
  startOfToday.setHours(0, 0, 0, 0);
  const startMs = startOfToday.getTime();

  let runningNow = 0;
  let completedToday = 0;
  let failedToday = 0;

  for (const job of jobs) {
    const status = job.status?.toUpperCase();
    if (!isTerminalStatus(status)) {
      runningNow++;
      continue;
    }
    const createdMs = Date.parse(job.created_at);
    if (Number.isNaN(createdMs) || createdMs < startMs) continue;
    if (status === "SUCCEEDED") completedToday++;
    else if (status === "FAILED") failedToday++;
  }

  const decided = completedToday + failedToday;
  const successRatePct = decided > 0 ? Math.round((completedToday / decided) * 100) : null;

  return { runningNow, completedToday, failedToday, successRatePct };
}
