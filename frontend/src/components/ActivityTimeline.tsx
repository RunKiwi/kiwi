import type { PhaseCategory, JobLane, TimeWindow } from "@/lib/jobActivity";
import { statusOf } from "@/lib/statusColors";

const PHASE_COLOR: Record<PhaseCategory, string> = {
  architect: "#A78BFA",
  implementer: "#5A9DF5",
  verify: "#2DD4BF",
  other: "rgba(255,255,255,0.18)",
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

const ROW_H = 34;
const ROW_GAP = 6;
const LANE_PAD = 10;

function fmtTick(ms: number): string {
  return new Date(ms).toLocaleTimeString(undefined, { hour: "2-digit", minute: "2-digit" });
}

function TimeRuler({ window }: { window: TimeWindow }) {
  const steps = 4;
  const ticks = Array.from({ length: steps + 1 }, (_, i) => window.startMs + (i * (window.endMs - window.startMs)) / steps);
  return (
    <div className="flex justify-between text-[10px] text-stone-400 mb-2 pl-[132px] pr-3">
      {ticks.map((t, i) => (
        <span key={i}>{fmtTick(t)}</span>
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
      className="absolute rounded-md overflow-hidden flex border transition-[filter] hover:brightness-125 focus-visible:outline focus-visible:outline-2 focus-visible:outline-white/60"
      style={{
        left: `${bar.startPct}%`,
        width: `max(6px, ${bar.widthPct}%)`,
        top,
        height: ROW_H,
        borderColor: s.border,
        background: "rgba(255,255,255,0.04)",
      }}
    >
      {bar.segments.length > 0 ? (
        bar.segments.map((seg, i) => (
          <span
            key={i}
            className="absolute inset-y-0"
            style={{ left: `${seg.startPct}%`, width: `${Math.max(seg.widthPct, 1)}%`, background: PHASE_COLOR[seg.category] }}
          />
        ))
      ) : (
        <span className="absolute inset-0" style={{ background: s.wash }} />
      )}
      {bar.isActive && (
        <span
          className="absolute right-0 top-0 bottom-0 w-[3px] animate-pulse"
          style={{ background: s.color }}
        />
      )}
    </button>
  );
}

function LaneRow({ data, onSelect }: { data: ActivityLaneData; onSelect: (jobId: string) => void }) {
  const height = Math.max(1, data.rowCount) * ROW_H + Math.max(0, data.rowCount - 1) * ROW_GAP + LANE_PAD * 2;
  return (
    <div className="flex items-stretch border-b border-sand-150 last:border-b-0">
      <div className="w-[132px] shrink-0 flex flex-col justify-center px-3 py-2 border-r border-sand-150">
        <div className="text-xs text-stone-700 truncate flex items-center gap-1.5">
          {data.lane.kind === "daemon" && (
            <span
              className="w-1.5 h-1.5 rounded-full shrink-0"
              style={{ background: data.lane.online ? "#22c55e" : "#555" }}
            />
          )}
          {data.lane.label}
        </div>
        <div className="text-[10px] text-stone-400 uppercase tracking-widest mt-0.5">{data.lane.kind}</div>
      </div>
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
      <div className="bg-white shadow-2xs border border-sand-200 rounded-2xl flex-1 min-h-[320px] flex items-center justify-center text-center px-8">
        <div>
          <div className="text-stone-700 text-sm mb-1">No fleet activity yet</div>
          <div className="text-stone-400 text-xs">Submit a task and its run will show up here as it happens.</div>
        </div>
      </div>
    );
  }

  return (
    <div className="bg-white shadow-2xs border border-sand-200 rounded-2xl flex-1 overflow-hidden flex flex-col">
      <div className="flex items-center gap-4 px-4 py-2.5 border-b border-sand-150 text-[10px] uppercase tracking-widest text-stone-400">
        <span>Legend</span>
        {(Object.keys(PHASE_COLOR) as PhaseCategory[])
          .filter((c) => c !== "other")
          .map((c) => (
            <span key={c} className="flex items-center gap-1.5 normal-case tracking-normal text-stone-500">
              <span className="w-2 h-2 rounded-sm" style={{ background: PHASE_COLOR[c] }} />
              {PHASE_LABEL[c]}
            </span>
          ))}
      </div>
      <TimeRuler window={window} />
      <div className="flex-1 overflow-y-auto">
        {lanes.map((data) => (
          <LaneRow key={data.lane.laneId} data={data} onSelect={onSelectJob} />
        ))}
      </div>
    </div>
  );
}
