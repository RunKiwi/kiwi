"use client";

import { useState } from "react";
import { ChevronDown, ChevronRight } from "lucide-react";
import type { RecordWorker, RecordStep } from "@/lib/api";

/**
 * RunTimeline renders what actually happened inside the Actor–Critic loop.
 *
 * Until now a failed job showed one line of result text, so "reached max steps
 * without passing" was the entire account of a ten-minute run — and the reason
 * it failed (the Critic rejecting the same thing three times) was visible only
 * in a daemon log on a machine the user cannot reach. The record already
 * carried every phase; nothing rendered it.
 *
 * The Critic's reasons are the payload. They are the only place the run
 * explains itself in words, so they are shown in full rather than truncated
 * behind a tooltip.
 */

// Each outcome gets one colour and one word. Colour never carries meaning
// alone — every row is labelled — so this stays readable for a colourblind
// reader and in a screenshot pasted into an issue.
const OUTCOME: Record<string, { dot: string; text: string; label: string }> = {
  pass: { dot: "bg-emerald-400", text: "text-emerald-300", label: "passed" },
  approved: { dot: "bg-emerald-400", text: "text-emerald-300", label: "approved" },
  proposed: { dot: "bg-sky-400", text: "text-sky-300", label: "proposed an edit" },
  rejected: { dot: "bg-amber-400", text: "text-amber-300", label: "rejected" },
  fail: { dot: "bg-rose-400", text: "text-rose-300", label: "failed" },
  error: { dot: "bg-rose-500", text: "text-rose-300", label: "errored" },
};

const PHASE_LABEL: Record<string, string> = {
  initial_test: "Baseline check",
  actor: "Actor",
  critic: "Critic",
  test: "Test",
};

function outcomeOf(o: string) {
  return OUTCOME[o] ?? { dot: "bg-zinc-500", text: "text-zinc-400", label: o };
}

/** Steps arrive flat and ordered; group them so each Actor iteration reads as one unit. */
function groupBySteps(steps: RecordStep[]): Array<{ step: number; rows: RecordStep[] }> {
  const order: number[] = [];
  const byStep = new Map<number, RecordStep[]>();
  for (const s of steps) {
    if (!byStep.has(s.step)) {
      byStep.set(s.step, []);
      order.push(s.step);
    }
    byStep.get(s.step)!.push(s);
  }
  return order.map((step) => ({ step, rows: byStep.get(step)! }));
}

function StepRow({ row }: { row: RecordStep }) {
  const o = outcomeOf(row.outcome);
  const phase = PHASE_LABEL[row.phase] ?? row.phase;

  return (
    <div className="flex gap-2.5">
      <span className={`mt-[7px] w-1.5 h-1.5 rounded-full shrink-0 ${o.dot}`} aria-hidden />
      <div className="min-w-0 flex-1">
        <p className="text-xs text-zinc-300">
          <span className="text-zinc-400">{phase}</span>{" "}
          <span className={o.text}>{o.label}</span>
        </p>
        {row.reasons ? (
          // The Critic's own words. Quoted rather than paraphrased: this is the
          // run explaining itself, and rewording it would lose the specifics
          // that make it actionable.
          <p className="mt-1 text-[11px] leading-relaxed text-zinc-400 border-l-2 border-white/10 pl-2.5">
            {row.reasons}
          </p>
        ) : null}
      </div>
    </div>
  );
}

export function RunTimeline({ workers }: { workers: RecordWorker[] }) {
  const [open, setOpen] = useState(true);

  const withSteps = workers.filter((w) => (w.steps?.length ?? 0) > 0);
  if (withSteps.length === 0) return null;

  const totalSteps = withSteps.reduce((n, w) => n + (w.steps?.length ?? 0), 0);

  return (
    <div className="rounded-lg bg-black/30 border border-white/5 overflow-hidden">
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        className="w-full flex items-center gap-1.5 px-2.5 py-2 text-xs text-zinc-400 hover:text-zinc-200 transition-colors"
        aria-expanded={open}
      >
        {open ? <ChevronDown className="w-3.5 h-3.5" /> : <ChevronRight className="w-3.5 h-3.5" />}
        <span className="font-medium">What happened</span>
        <span className="text-zinc-600">
          {totalSteps} phase{totalSteps === 1 ? "" : "s"}
        </span>
      </button>

      {open && (
        <div className="px-2.5 pb-3 flex flex-col gap-4">
          {withSteps.map((w, wi) => (
            <div key={w.worker_id ?? wi} className="flex flex-col gap-3">
              {/* The model is worth stating: a run's behaviour is a property of
                  the model as much as of the code, and it is the first thing
                  worth changing when results disappoint. */}
              {(w.actor_model || withSteps.length > 1) && (
                <div className="flex items-center gap-2 text-[11px] text-zinc-500 font-mono">
                  {withSteps.length > 1 && <span className="text-zinc-400">{w.worker_id}</span>}
                  {w.actor_model && <span>{w.actor_model}</span>}
                </div>
              )}

              {groupBySteps(w.steps ?? []).map(({ step, rows }) => (
                <div key={step} className="flex gap-3">
                  <div className="w-14 shrink-0 pt-[3px]">
                    <span className="text-[10px] uppercase tracking-widest text-zinc-600">
                      {step === 0 ? "Start" : `Step ${step}`}
                    </span>
                  </div>
                  <div className="min-w-0 flex-1 flex flex-col gap-2 border-l border-white/5 pl-3">
                    {rows.map((row, i) => (
                      <StepRow key={i} row={row} />
                    ))}
                  </div>
                </div>
              ))}
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
