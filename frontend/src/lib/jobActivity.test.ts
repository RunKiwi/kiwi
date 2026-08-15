import { describe, it } from "node:test";
import assert from "node:assert";
import {
  categorizePhase,
  buildPhaseSegments,
  laneForJob,
  buildLanes,
  defaultWindow,
  jobsInWindow,
  barExtent,
  assignSubRows,
  computeHeaderStats,
} from "./jobActivity.ts";
import type { Daemon, Fleet, JobSummary, RecordStep } from "./api.ts";

describe("categorizePhase", () => {
  it("maps critic to architect", () => {
    assert.equal(categorizePhase("critic"), "architect");
  });
  it("maps test and initial_test to verify", () => {
    assert.equal(categorizePhase("test"), "verify");
    assert.equal(categorizePhase("initial_test"), "verify");
  });
  it("maps actor, with or without a tool suffix, to implementer", () => {
    assert.equal(categorizePhase("actor"), "implementer");
    assert.equal(categorizePhase("actor:read_file"), "implementer");
  });
  it("falls back to other for round markers", () => {
    assert.equal(categorizePhase("round_start"), "other");
    assert.equal(categorizePhase("session_end"), "other");
  });
});

function step(phase: string, at: string): RecordStep {
  return { step: 0, phase, outcome: "pass", at };
}

describe("buildPhaseSegments", () => {
  it("merges consecutive same-category steps into one segment", () => {
    const steps = [
      step("critic", "2026-01-01T00:00:00.000Z"),
      step("actor:read_file", "2026-01-01T00:00:01.000Z"),
      step("actor:edit_file", "2026-01-01T00:00:02.000Z"),
      step("test", "2026-01-01T00:00:03.000Z"),
    ];
    const segments = buildPhaseSegments(steps, { nowMs: Date.parse("2026-01-01T00:00:03.000Z"), isActive: false });
    assert.equal(segments.length, 3);
    assert.equal(segments[0].category, "architect");
    assert.equal(segments[1].category, "implementer");
    assert.equal(segments[1].startMs, Date.parse("2026-01-01T00:00:01.000Z"));
    assert.equal(segments[1].endMs, Date.parse("2026-01-01T00:00:02.000Z"));
    assert.equal(segments[2].category, "verify");
  });

  it("drops steps with no parseable timestamp", () => {
    const steps = [step("critic", ""), step("actor", "2026-01-01T00:00:01.000Z")];
    const segments = buildPhaseSegments(steps, { nowMs: 0, isActive: false });
    assert.equal(segments.length, 1);
    assert.equal(segments[0].category, "implementer");
  });

  it("stretches the final segment to now when the job is still active", () => {
    const steps = [step("actor", "2026-01-01T00:00:01.000Z")];
    const now = Date.parse("2026-01-01T00:05:00.000Z");
    const segments = buildPhaseSegments(steps, { nowMs: now, isActive: true });
    assert.equal(segments[0].endMs, now);
  });

  it("returns an empty list for no timed steps", () => {
    assert.deepEqual(buildPhaseSegments([], { nowMs: 0, isActive: false }), []);
  });
});

function job(overrides: Partial<JobSummary>): JobSummary {
  return {
    job_id: "job_1",
    created_at: new Date().toISOString(),
    task_count: 1,
    status: "RUNNING",
    pr_urls: [],
    ...overrides,
  };
}

describe("laneForJob / buildLanes", () => {
  it("prefers the daemon lane when the daemon is known", () => {
    const laneId = laneForJob(job({ daemon_id: "d1", fleet_id: "f1" }), new Set(["d1"]));
    assert.equal(laneId, "daemon:d1");
  });

  it("falls back to the fleet lane when the daemon is unknown", () => {
    const laneId = laneForJob(job({ daemon_id: "ghost", fleet_id: "f1" }), new Set(["d1"]));
    assert.equal(laneId, "fleet:f1");
  });

  it("falls back to unassigned when neither is known", () => {
    const laneId = laneForJob(job({}), new Set());
    assert.equal(laneId, "unassigned");
  });

  it("builds one lane per known daemon plus fleet/unassigned lanes only when referenced", () => {
    const daemons: Daemon[] = [
      { id: "d1", online: true, created_at: "2026-01-01T00:00:00.000Z" },
      { id: "d2", online: false, created_at: "2026-01-01T00:00:01.000Z" },
    ];
    const fleets: Fleet[] = [{ id: "f1", org_id: "o", name: "Prod fleet", type: "byoc", created_at: "2026-01-01T00:00:00.000Z" }];
    const jobs = [job({ daemon_id: "d1" }), job({ fleet_id: "f1" }), job({})];

    const lanes = buildLanes(daemons, fleets, jobs);
    assert.deepEqual(
      lanes.map((l) => l.laneId),
      ["daemon:d1", "daemon:d2", "fleet:f1", "unassigned"],
    );
    assert.equal(lanes[2].label, "Prod fleet");
  });

  it("omits the unassigned lane when no job needs it", () => {
    const lanes = buildLanes([{ id: "d1", online: true, created_at: "2026-01-01T00:00:00.000Z" }], [], [job({ daemon_id: "d1" })]);
    assert.ok(!lanes.some((l) => l.laneId === "unassigned"));
  });
});

describe("jobsInWindow", () => {
  const window = defaultWindow(Date.parse("2026-01-01T12:00:00.000Z"), 2);

  it("includes a terminal job that started inside the window", () => {
    const jobs = [job({ status: "SUCCEEDED", created_at: "2026-01-01T11:00:00.000Z" })];
    assert.equal(jobsInWindow(jobs, window).length, 1);
  });

  it("excludes a terminal job that started before the window", () => {
    const jobs = [job({ status: "SUCCEEDED", created_at: "2026-01-01T08:00:00.000Z" })];
    assert.equal(jobsInWindow(jobs, window).length, 0);
  });

  it("keeps a still-running job regardless of when it started", () => {
    const jobs = [job({ status: "RUNNING", created_at: "2026-01-01T01:00:00.000Z" })];
    assert.equal(jobsInWindow(jobs, window).length, 1);
  });
});

describe("barExtent", () => {
  const window: { startMs: number; endMs: number } = { startMs: 0, endMs: 1000 };

  it("clamps a running job's end to now, not the window edge, when now is inside the window", () => {
    const j = job({ status: "RUNNING", created_at: new Date(100).toISOString() });
    const extent = barExtent(j, [], window, 500);
    assert.equal(extent.startMs, 100);
    assert.equal(extent.endMs, 500);
  });

  it("clamps a job that started before the window to the window's start", () => {
    const j = job({ status: "RUNNING", created_at: new Date(-500).toISOString() });
    const extent = barExtent(j, [], window, 500);
    assert.equal(extent.startMs, 0);
  });

  it("uses the last segment's end for a terminal job", () => {
    const j = job({ status: "SUCCEEDED", created_at: new Date(100).toISOString() });
    const segments = [{ category: "verify" as const, startMs: 100, endMs: 400 }];
    const extent = barExtent(j, segments, window, 999);
    assert.equal(extent.endMs, 400);
  });
});

describe("assignSubRows", () => {
  it("keeps non-overlapping bars on the same row", () => {
    const bars = [
      { id: "a", startMs: 0, endMs: 100 },
      { id: "b", startMs: 100, endMs: 200 },
    ];
    const rows = assignSubRows(bars);
    assert.equal(rows.get("a"), 0);
    assert.equal(rows.get("b"), 0);
  });

  it("pushes overlapping bars onto separate rows", () => {
    const bars = [
      { id: "a", startMs: 0, endMs: 200 },
      { id: "b", startMs: 50, endMs: 150 },
    ];
    const rows = assignSubRows(bars);
    assert.notEqual(rows.get("a"), rows.get("b"));
  });

  it("reuses a freed row for a third bar that starts after the first ends", () => {
    const bars = [
      { id: "a", startMs: 0, endMs: 100 },
      { id: "b", startMs: 10, endMs: 200 },
      { id: "c", startMs: 150, endMs: 250 },
    ];
    const rows = assignSubRows(bars);
    assert.equal(rows.get("a"), 0);
    assert.equal(rows.get("b"), 1);
    assert.equal(rows.get("c"), 0);
  });
});

describe("computeHeaderStats", () => {
  const today = new Date("2026-01-02T12:00:00.000Z");

  it("counts non-terminal jobs as running regardless of date", () => {
    const jobs = [job({ status: "RUNNING", created_at: "2026-01-01T00:00:00.000Z" })];
    assert.equal(computeHeaderStats(jobs, today).runningNow, 1);
  });

  it("only counts terminal jobs from today toward the success rate", () => {
    const jobs = [
      job({ status: "SUCCEEDED", created_at: "2026-01-02T01:00:00.000Z" }),
      job({ status: "FAILED", created_at: "2026-01-02T02:00:00.000Z" }),
      job({ status: "SUCCEEDED", created_at: "2026-01-01T01:00:00.000Z" }), // yesterday, excluded
    ];
    const stats = computeHeaderStats(jobs, today);
    assert.equal(stats.completedToday, 1);
    assert.equal(stats.failedToday, 1);
    assert.equal(stats.successRatePct, 50);
  });

  it("returns null success rate when nothing finished today", () => {
    const stats = computeHeaderStats([job({ status: "RUNNING" })], today);
    assert.equal(stats.successRatePct, null);
  });
});
