"use client";

import { useState } from "react";
import { ChevronDown, ChevronRight } from "lucide-react";
import type { RecordWorker, RecordStep } from "@/lib/api";
import { formatDuration, formatCost, formatTokens } from "@/lib/datetime";

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
  // Session mode: an Implementer turn either asks for tools or stops. Neither
  // is a failure, so neither gets a failure colour.
  tools: { dot: "bg-sky-400", text: "text-sky-300", label: "called tools" },
  done: { dot: "bg-zinc-400", text: "text-zinc-400", label: "ended its turn" },
  ok: { dot: "bg-zinc-400", text: "text-zinc-400", label: "ok" },
};

/**
 * A tool call's arguments, rendered as the one line that matters.
 *
 * The raw value is the JSON the model emitted, which is the honest thing to
 * keep but a poor thing to show: `{"command":"npm test"}` reads worse than
 * `npm test`. So the common single-meaning arguments are unwrapped and anything
 * unrecognised falls back to the JSON, which is still better than nothing.
 */
function summariseInput(input?: string): string {
  if (!input) return "";
  try {
    const o = JSON.parse(input) as Record<string, unknown>;
    for (const k of ["command", "path", "pattern"]) {
      if (typeof o[k] === "string" && o[k]) {
        // edit_file and read_file both key on `path`; naming the other argument
        // is what distinguishes "read this" from "change this".
        if (k === "path" && typeof o.old_string === "string") {
          return `${o[k]} — replacing ${JSON.stringify(shorten(o.old_string, 40))}`;
        }
        return o[k] as string;
      }
    }
    return input;
  } catch {
    return input;
  }
}

function shorten(s: string, n: number): string {
  const flat = s.replace(/\s+/g, " ").trim();
  return flat.length > n ? flat.slice(0, n) + "…" : flat;
}

const PHASE_LABEL: Record<string, string> = {
  initial_test: "Baseline check",
  actor: "Actor",
  critic: "Critic",
  test: "Test",
  // Session mode emits these raw (pkg/daemon/session_run.go sessionPhase), so
  // without them a session run showed bare snake_case rows.
  round_start: "Round started",
  session_end: "Session ended",
  implementer: "Implementer",
  compaction: "Compacted context",
};

function outcomeOf(o: string) {
  return OUTCOME[o] ?? { dot: "bg-zinc-500", text: "text-zinc-400", label: o };
}

/**
 * Split a phase into its label and the tool it ran, if any.
 *
 * Session mode encodes the tool into the phase as `actor:read_file`. Rendering
 * that verbatim was most of why a session run read as a black box — the row
 * said `actor:read_file` with no indication of what was read.
 */
function labelPhase(phase: string): { label: string; tool: string } {
  const colon = phase.indexOf(":");
  if (colon !== -1) {
    const head = phase.slice(0, colon);
    return { label: PHASE_LABEL[head] ?? head, tool: phase.slice(colon + 1).trim() };
  }
  return { label: PHASE_LABEL[phase] ?? phase, tool: "" };
}

/**
 * Per-worker totals for the header row.
 *
 * The signed record carries these on the worker; the live feed carries the same
 * quantities per step instead. So a finished run reads the record's totals and a
 * running one sums what has arrived so far — which is why the numbers climb
 * during a run rather than appearing at the end.
 */
function workerTotals(w: RecordWorker): string[] {
  const steps = w.steps ?? [];
  const sum = (pick: (s: RecordStep) => number | undefined) =>
    steps.reduce((n, s) => n + (pick(s) ?? 0), 0);

  // Prefer the record's own totals; fall back to summing the live steps.
  const recordTokens = (w.input_tokens ?? 0) + (w.output_tokens ?? 0);
  const tokens = recordTokens > 0
    ? recordTokens
    : sum(s => s.input_tokens) + sum(s => s.output_tokens);

  const recordCost = w.cost_usd ?? 0;
  const cost = recordCost > 0 ? recordCost : sum(s => s.cost_usd);

  const elapsed = sum(s => s.duration_ms);

  const out: string[] = [];
  if (elapsed > 0) out.push(formatDuration(elapsed));
  if (tokens > 0) out.push(`${formatTokens(tokens)} tok`);
  if (cost > 0) out.push(formatCost(cost));
  if (w.critic_rejections) out.push(`${w.critic_rejections} rejected`);
  return out;
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
  const { label, tool } = labelPhase(row.phase);
  const args = summariseInput(row.input);

  // Cost and tokens ride the live feed, not the signed record, so a running job
  // shows them and a finished one falls back to the worker totals. Rendered
  // only when non-zero — a "$0.0000 · 0 tok" row on a phase that made no model
  // call is noise dressed up as information.
  const meta: string[] = [];
  if (row.duration_ms) meta.push(formatDuration(row.duration_ms));
  const tokens = (row.input_tokens ?? 0) + (row.output_tokens ?? 0);
  if (tokens > 0) meta.push(`${formatTokens(tokens)} tok`);
  if (row.cost_usd) meta.push(formatCost(row.cost_usd));

  return (
    <div className="flex gap-2.5">
      <span className={`mt-[7px] w-1.5 h-1.5 rounded-full shrink-0 ${o.dot}`} aria-hidden />
      <div className="min-w-0 flex-1">
        <p className="text-xs text-zinc-300 flex items-center gap-1.5 flex-wrap">
          <span className="text-zinc-400">{label}</span>
          {tool && (
            <code className="text-[10px] font-mono px-1 py-px rounded bg-white/5 text-zinc-300">
              {tool}
            </code>
          )}
          <span className={o.text}>{o.label}</span>
          {meta.length > 0 && (
            <span className="ml-auto text-[10px] text-zinc-500 font-mono shrink-0 tabular-nums">
              {meta.join(" · ")}
            </span>
          )}
        </p>
        {/* What the tool was actually asked to do. Without this a row could say
            that `run` was called and show what it printed, but never the
            command — so "is it rewriting whole files or editing them?" was not
            answerable from the timeline, only guessable from the output. */}
        {args && (
          <p className="mt-0.5 text-[11px] font-mono text-zinc-500 break-all">
            <span className="text-zinc-600 select-none">$ </span>
            {args}
          </p>
        )}
        {/* `reasons` is the Critic's verdict on the signed record; `detail` is
            what the live feed carries for the same row (a tool's output tail, a
            test result). Either way it is the run explaining itself in its own
            words, so it is quoted rather than paraphrased. */}
        {(row.reasons || row.detail) ? (
          <pre className="mt-1 text-[11px] leading-relaxed text-zinc-400 border-l-2 border-white/10 pl-2.5 whitespace-pre-wrap break-words font-sans">
            {row.reasons || row.detail}
          </pre>
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
              {(w.actor_model || withSteps.length > 1 || workerTotals(w).length > 0) && (
                <div className="flex items-center gap-2 text-[11px] text-zinc-500 font-mono flex-wrap">
                  {withSteps.length > 1 && <span className="text-zinc-400">{w.worker_id}</span>}
                  {w.actor_model && <span>{w.actor_model}</span>}
                  {/* The critic model is often a different (cheaper or dearer)
                      one than the actor, and which pairing ran is exactly what
                      you need when comparing two runs of the same task. */}
                  {w.critic_model && w.critic_model !== w.actor_model && (
                    <span className="text-zinc-600">critic {w.critic_model}</span>
                  )}
                  {workerTotals(w).length > 0 && (
                    <span className="ml-auto tabular-nums">{workerTotals(w).join(" · ")}</span>
                  )}
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
