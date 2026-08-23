"use client";

import { useState } from "react";
import { ChevronDown, ChevronRight } from "lucide-react";
import type { RecordWorker, RecordStep } from "@/lib/api";
import { formatDuration, formatCost, formatTokens } from "@/lib/datetime";
import { parseToolArgs, parseNumberedFile, languageOf, editDiff, parseUnifiedDiff } from "@/lib/toolContent";
import { CodeBlock, DiffView } from "@/components/CodeView";

/**
 * RunTimeline renders what actually happened inside the Actor–Critic loop.
 *
 * Designed with Enterprise SaaS light theme: crisp contrast, readable typography,
 * clear colored badges, and zero low-contrast text.
 */

const OUTCOME: Record<string, { dot: string; text: string; label: string }> = {
  pass: { dot: "bg-emerald-500", text: "text-emerald-700 font-semibold", label: "passed" },
  approved: { dot: "bg-emerald-500", text: "text-emerald-700 font-semibold", label: "approved" },
  proposed: { dot: "bg-sky-500", text: "text-sky-700 font-semibold", label: "proposed an edit" },
  rejected: { dot: "bg-amber-500", text: "text-amber-800 font-semibold", label: "rejected" },
  fail: { dot: "bg-rose-500", text: "text-rose-700 font-semibold", label: "failed" },
  error: { dot: "bg-rose-600", text: "text-rose-800 font-semibold", label: "errored" },
  tools: { dot: "bg-sky-500", text: "text-sky-700 font-semibold", label: "called tools" },
  done: { dot: "bg-stone-400", text: "text-stone-600 font-semibold", label: "ended its turn" },
  ok: { dot: "bg-stone-400", text: "text-stone-600 font-semibold", label: "ok" },
};

function summariseInput(input?: string): string {
  if (!input) return "";
  try {
    const o = JSON.parse(input) as Record<string, unknown>;
    for (const k of ["command", "path", "pattern"]) {
      if (typeof o[k] === "string" && o[k]) {
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
  clone: "Cloning repository",
  install: "Installing dependencies",
  round_start: "Round started",
  session_end: "Session ended",
  implementer: "Implementer",
  compaction: "Compacted context",
};

function outcomeOf(o: string) {
  return OUTCOME[o] ?? { dot: "bg-stone-400", text: "text-stone-600 font-semibold", label: o };
}

function labelPhase(phase: string): { label: string; tool: string } {
  const colon = phase.indexOf(":");
  if (colon !== -1) {
    const head = phase.slice(0, colon);
    return { label: PHASE_LABEL[head] ?? head, tool: phase.slice(colon + 1).trim() };
  }
  return { label: PHASE_LABEL[phase] ?? phase, tool: "" };
}

function workerTotals(w: RecordWorker): string[] {
  const steps = w.steps ?? [];
  const sum = (pick: (s: RecordStep) => number | undefined) =>
    steps.reduce((n, s) => n + (pick(s) ?? 0), 0);

  const recordTokens = (w.input_tokens ?? 0) + (w.output_tokens ?? 0);
  const tokens = recordTokens > 0
    ? recordTokens
    : sum((s) => s.input_tokens) + sum((s) => s.output_tokens);

  const recordCost = w.cost_usd ?? 0;
  const cost = recordCost > 0 ? recordCost : sum((s) => s.cost_usd);

  const elapsed = sum((s) => s.duration_ms);

  const out: string[] = [];
  if (elapsed > 0) out.push(formatDuration(elapsed));
  if (tokens > 0) out.push(`${formatTokens(tokens)} tok`);
  if (cost > 0) out.push(formatCost(cost));
  if (w.critic_rejections) out.push(`${w.critic_rejections} rejected`);
  return out;
}

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

  const toolArgs = parseToolArgs(row.input);
  const lang = languageOf(toolArgs.path);
  const parsedFile = row.detail ? parseNumberedFile(row.detail) : null;
  const file = parsedFile ? { ...parsedFile, lang } : null;

  const reported = row.detail ? parseUnifiedDiff(row.detail) : null;
  const edit = reported
    ? { lines: reported.lines, lang: languageOf(reported.path ?? toolArgs.path), truncated: reported.truncated }
    : toolArgs.oldString !== undefined && toolArgs.newString !== undefined
      ? { lines: editDiff(toolArgs.oldString, toolArgs.newString), lang, truncated: false }
      : null;

  const meta: string[] = [];
  if (row.duration_ms) meta.push(formatDuration(row.duration_ms));
  const tokens = (row.input_tokens ?? 0) + (row.output_tokens ?? 0);
  if (tokens > 0) meta.push(`${formatTokens(tokens)} tok`);
  if (row.cost_usd) meta.push(formatCost(row.cost_usd));

  return (
    <div className="flex gap-2.5 text-stone-900">
      <span className={`mt-[7px] w-2 h-2 rounded-full shrink-0 ${o.dot}`} aria-hidden />
      <div className="min-w-0 flex-1">
        <p className="text-xs text-stone-800 flex items-center gap-1.5 flex-wrap">
          <span className="font-semibold text-stone-700">{label}</span>
          {tool && (
            <code className="text-[10px] font-mono px-1 py-px rounded bg-sand-100 text-stone-800 border border-sand-200">
              {tool}
            </code>
          )}
          <span className={o.text}>{o.label}</span>
          {meta.length > 0 && (
            <span className="ml-auto text-[10px] text-stone-400 font-mono shrink-0 tabular-nums">
              {meta.join(" · ")}
            </span>
          )}
        </p>
        {args && (
          <p className="mt-1 text-[11px] font-mono text-stone-700 break-all bg-sand-50/90 px-2 py-1 rounded-lg border border-sand-200">
            <span className="text-stone-400 select-none font-bold">$ </span>
            {args}
          </p>
        )}
        {edit && (
          <>
            <DiffView lines={edit.lines} lang={edit.lang} />
            {edit.truncated && (
              <p className="mt-0.5 text-[10px] text-stone-400">
                Diff truncated — the change is larger than what is recorded here.
              </p>
            )}
          </>
        )}

        {file ? (
          <CodeBlock code={file.code} lang={file.lang} startLine={file.startLine} note={file.note} />
        ) : reported ? null : (row.reasons || row.detail) ? (
          <pre className="mt-1.5 text-[11px] leading-relaxed text-stone-800 bg-sand-50 border border-sand-200 p-3 rounded-xl whitespace-pre-wrap break-words font-mono">
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
    <div className="rounded-2xl bg-white border border-sand-200 overflow-hidden shadow-2xs">
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        className="w-full flex items-center justify-between px-4 py-2.5 text-xs text-stone-700 hover:bg-sand-50 transition-colors border-b border-sand-150"
        aria-expanded={open}
      >
        <div className="flex items-center gap-1.5 font-bold text-stone-900">
          {open ? <ChevronDown className="w-4 h-4 text-stone-500" /> : <ChevronRight className="w-4 h-4 text-stone-500" />}
          <span>What happened</span>
          <span className="text-stone-400 font-mono text-[11px] font-normal">
            ({totalSteps} phase{totalSteps === 1 ? "" : "s"})
          </span>
        </div>
      </button>

      {open && (
        <div className="p-4 flex flex-col gap-4">
          {withSteps.map((w, wi) => (
            <div key={w.worker_id ?? wi} className="flex flex-col gap-3">
              {(w.actor_model || withSteps.length > 1 || workerTotals(w).length > 0) && (
                <div className="flex items-center gap-2 text-[11px] text-stone-600 font-mono flex-wrap bg-sand-50 p-2 rounded-xl border border-sand-200">
                  {withSteps.length > 1 && <span className="font-bold text-stone-800">{w.worker_id}</span>}
                  {w.actor_model && <span className="text-stone-700 font-medium">{w.actor_model}</span>}
                  {w.critic_model && w.critic_model !== w.actor_model && (
                    <span className="text-stone-500">critic {w.critic_model}</span>
                  )}
                  {workerTotals(w).length > 0 && (
                    <span className="ml-auto font-bold text-stone-900 tabular-nums">{workerTotals(w).join(" · ")}</span>
                  )}
                </div>
              )}

              {groupBySteps(w.steps ?? []).map(({ step, rows }) => (
                <div key={step} className="flex gap-3">
                  <div className="w-14 shrink-0 pt-1">
                    <span className="text-[10px] font-mono font-bold uppercase tracking-wider text-stone-400">
                      {step === 0 ? "START" : `STEP ${step}`}
                    </span>
                  </div>
                  <div className="min-w-0 flex-1 flex flex-col gap-3 border-l-2 border-sand-200 pl-3.5">
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

