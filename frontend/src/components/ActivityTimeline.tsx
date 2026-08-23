"use client";

import React from "react";
import type { PhaseCategory, JobLane, TimeWindow } from "@/lib/jobActivity";
import { statusOf } from "@/lib/statusColors";
import { Activity, Clock, Server } from "lucide-react";

const PHASE_COLOR: Record<PhaseCategory, string> = {
  architect: "#8B5CF6", // Royal Purple
  implementer: "#3B82F6", // Bright Blue
  verify: "#10B981", // Emerald Green
  other: "#9CA3AF", // Slate
};

const PHASE_LABEL: Record<PhaseCategory, string> = {
  architect: "Architect",
  implementer: "Implementer",
  verify: "Verify",
  other: "Other",
};

export interface ActivityBarSegment {
  category: PhaseCategory;
  startPct: number; // position within the bar itself, 0-100
  widthPct: number;
}

export interface ActivityBar {
  jobId: string;
  label: string;
  status: string;
  subRow: number;
  startPct: number; // position within the window, 0-100
  widthPct: number;
  segments: ActivityBarSegment[];
  isActive: boolean;
}

export interface ActivityLaneData {
  lane: JobLane;
  bars: ActivityBar[];
  rowCount: number;
}

const ROW_H = 32;
const ROW_GAP = 6;
const LANE_PAD = 8;

function fmtTick(ms: number): string {
  return new Date(ms).toLocaleTimeString(undefined, { hour: "2-digit", minute: "2-digit" });
}

function TimeRuler({ window }: { window: TimeWindow }) {
  const steps = 5;
  const ticks = Array.from({ length: steps + 1 }, (_, i) => window.startMs + (i * (window.endMs - window.startMs)) / steps);
  return (
    <div className="flex justify-between text-[10px] font-mono text-stone-400 py-2 border-b border-sand-200 bg-sand-50/50 pl-[160px] pr-4">
      {ticks.map((t, i) => (
        <span key={i} className="flex items-center gap-1">
          <Clock className="w-2.5 h-2.5 text-stone-300" />
          <span>{fmtTick(t)}</span>
        </span>
      ))}
    </div>
  );
}

function JobBarView({ bar, onSelect }: { bar: ActivityBar; onSelect: (jobId: string) => void }) {
  const s = statusOf(bar.status);
  const top = bar.subRow * (ROW_H + ROW_GAP);
  const durationLabel = bar.segments
    .map((seg) => PHASE_LABEL[seg.category])
    .filter((v, i, arr) => arr.indexOf(v) === i)
    .join(" → ");

  return (
    <button
      type="button"
      onClick={() => onSelect(bar.jobId)}
      title={`${bar.label} — ${s.label}${durationLabel ? ` (${durationLabel})` : ""}`}
      className="absolute rounded-lg overflow-hidden flex border transition-all duration-150 hover:shadow-md hover:scale-[1.01] focus-visible:outline focus-visible:outline-2 focus-visible:outline-stone-800 group shadow-2xs"
      style={{
        left: `${Math.max(0, Math.min(99, bar.startPct))}%`,
        width: `max(10px, ${Math.min(100 - bar.startPct, bar.widthPct)}%)`,
        top,
        height: ROW_H,
        borderColor: bar.isActive ? "#3B82F6" : s.border || "#E2DFD9",
        background: "#FFFFFF",
      }}
    >
      {bar.segments.length > 0 ? (
        bar.segments.map((seg, i) => (
          <span
            key={i}
            className="absolute inset-y-0 opacity-85 hover:opacity-100 transition-opacity"
            style={{
              left: `${seg.startPct}%`,
              width: `${Math.max(seg.widthPct, 2)}%`,
              background: PHASE_COLOR[seg.category],
            }}
          />
        ))
      ) : (
        <span className="absolute inset-0" style={{ background: s.wash || "#F5F3EF" }} />
      )}

      {/* Task text inside bar if wide enough */}
      <span className="relative z-10 text-[10px] font-mono font-bold text-white px-2 flex items-center truncate drop-shadow-xs">
        {bar.label}
      </span>

      {bar.isActive && (
        <span
          className="absolute right-0 top-0 bottom-0 w-[4px] bg-sky-400 animate-pulse z-20"
        />
      )}
    </button>
  );
}

function LaneRow({ data, onSelect }: { data: ActivityLaneData; onSelect: (jobId: string) => void }) {
  const height = Math.max(1, data.rowCount) * ROW_H + Math.max(0, data.rowCount - 1) * ROW_GAP + LANE_PAD * 2;
  return (
    <div className="flex items-stretch border-b border-sand-150 last:border-b-0 hover:bg-sand-50/30 transition-colors">
      {/* Lane Label */}
      <div className="w-[160px] shrink-0 flex flex-col justify-center px-3.5 py-2 border-r border-sand-200 bg-sand-50/40">
        <div className="text-xs font-bold text-stone-900 truncate flex items-center gap-1.5 font-mono">
          {data.lane.kind === "daemon" ? (
            <span
              className={`w-2 h-2 rounded-full shrink-0 ${data.lane.online ? "bg-emerald-500 ring-2 ring-emerald-200" : "bg-stone-400"}`}
              title={data.lane.online ? "Daemon Online" : "Daemon Offline"}
            />
          ) : (
            <Server className="w-3 h-3 text-stone-400 shrink-0" />
          )}
          <span className="truncate">{data.lane.label}</span>
        </div>
        <div className="text-[10px] font-mono text-stone-400 uppercase tracking-wider mt-0.5">
          {data.lane.kind === "daemon" ? "Private Daemon" : data.lane.kind === "fleet" ? "Shared Fleet" : "Unassigned"}
        </div>
      </div>

      {/* Lane Timeline Track */}
      <div className="relative flex-1" style={{ height }}>
        <div className="absolute inset-0" style={{ padding: `${LANE_PAD}px 12px` }}>
          <div className="relative w-full h-full">
            {data.bars.map((bar) => (
              <JobBarView key={bar.jobId} bar={bar} onSelect={onSelect} />
            ))}
          </div>
        </div>
      </div>
    </div>
  );
}

export function ActivityTimeline({
  lanes,
  window,
  onSelectJob,
}: {
  lanes: ActivityLaneData[];
  window: TimeWindow;
  onSelectJob: (jobId: string) => void;
}) {
  if (lanes.length === 0) {
    return (
      <div className="bg-white shadow-2xs border border-sand-200 rounded-2xl min-h-[280px] flex flex-col items-center justify-center text-center p-8 space-y-2">
        <Activity className="w-8 h-8 text-stone-300 mx-auto" />
        <div className="text-stone-800 text-sm font-bold">No Fleet Activity Recorded in this Time Window</div>
        <div className="text-stone-400 text-xs max-w-sm">
          Tasks and review watchdogs will plot along daemon execution tracks in real-time as they run.
        </div>
      </div>
    );
  }

  return (
    <div className="bg-white shadow-2xs border border-sand-200 rounded-2xl overflow-hidden flex flex-col font-sans">
      {/* Timeline Header & Legend */}
      <div className="flex flex-wrap items-center justify-between gap-3 px-4 py-3 border-b border-sand-200 bg-sand-50/70 text-xs">
        <div className="flex items-center gap-2">
          <Activity className="w-4 h-4 text-stone-700" />
          <span className="font-bold text-stone-900 text-xs uppercase tracking-wider">Fleet Execution Lanes</span>
        </div>

        <div className="flex items-center gap-4 flex-wrap text-[11px] font-mono text-stone-600">
          <span className="flex items-center gap-1.5">
            <span className="w-2.5 h-2.5 rounded-sm bg-[#8B5CF6]" />
            <span>Architect</span>
          </span>
          <span className="flex items-center gap-1.5">
            <span className="w-2.5 h-2.5 rounded-sm bg-[#3B82F6]" />
            <span>Implementer</span>
          </span>
          <span className="flex items-center gap-1.5">
            <span className="w-2.5 h-2.5 rounded-sm bg-[#10B981]" />
            <span>Verify &amp; Tests</span>
          </span>
        </div>
      </div>

      <TimeRuler window={window} />

      <div className="overflow-y-auto no-scrollbar max-h-[420px]">
        {lanes.map((data) => (
          <LaneRow key={data.lane.laneId} data={data} onSelect={onSelectJob} />
        ))}
      </div>
    </div>
  );
}
