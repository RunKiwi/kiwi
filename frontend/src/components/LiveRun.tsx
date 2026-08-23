"use client";

import { useState } from "react";
import { ChevronDown, ChevronRight, Terminal } from "lucide-react";
import { ThinkingOrb } from "@/components/ThinkingOrb";
import { RunTimeline } from "@/components/RunTimeline";
import { orbStateForPhase } from "@/lib/orbState";
import { elapsedSince } from "@/lib/progressTime";
import type { JobProgressTask } from "@/lib/api";

/**
 * LiveRun shows what a job is doing while it is still doing it.
 *
 * Updated with Enterprise SaaS light styling, high contrast colors, and ThinkingOrb glow.
 */

function splitPhase(phase: string): { kind: string; command: string } {
  const i = phase.indexOf(":");
  if (i === -1) return { kind: phase, command: "" };
  return { kind: phase.slice(0, i), command: phase.slice(i + 1).trim() };
}

function staleness(progressAt?: string): number | null {
  if (!progressAt) return null;
  const t = Date.parse(progressAt);
  if (Number.isNaN(t)) return null;
  return Math.max(0, Math.round((Date.now() - t) / 1000));
}

function formatElapsed(seconds: number): string {
  if (seconds < 60) return `${seconds}s`;
  const m = Math.floor(seconds / 60);
  const s = seconds % 60;
  return `${m}m${s.toString().padStart(2, "0")}s`;
}

function OutputTail({ text }: { text: string }) {
  const [open, setOpen] = useState(true);
  const lines = text.replace(/\s+$/, "").split("\n");

  return (
    <div className="rounded-xl bg-stone-950 border border-stone-800 overflow-hidden shadow-inner">
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        className="w-full flex items-center justify-between px-3 py-1.5 text-[11px] font-mono text-stone-400 hover:text-stone-200 bg-stone-900 border-b border-stone-800 transition-colors"
        aria-expanded={open}
      >
        <div className="flex items-center gap-1.5">
          {open ? <ChevronDown className="w-3.5 h-3.5" /> : <ChevronRight className="w-3.5 h-3.5" />}
          <Terminal className="w-3.5 h-3.5 text-stone-400" />
          <span>Live Runner Output</span>
        </div>
        <span className="text-stone-500 text-[10px]">last {lines.length} lines</span>
      </button>
      {open && (
        <pre className="p-3 text-[11px] leading-relaxed text-stone-200 font-mono overflow-x-auto whitespace-pre max-h-60">
          {lines.join("\n")}
        </pre>
      )}
    </div>
  );
}

export function LiveRun({ tasks }: { tasks: JobProgressTask[] }) {
  const running = tasks.filter((t) => t.status === "LEASED" || t.status === "RUNNING");
  const anySteps = tasks.some((t) => (t.steps?.length ?? 0) > 0);
  if (running.length === 0 && !anySteps) return null;

  return (
    <div className="flex flex-col gap-3">
      {/* Phases completed so far */}
      {anySteps && (
        <RunTimeline
          workers={tasks.map((t) => ({
            worker_id: t.task_id,
            actor_model: t.actor_model,
            steps: t.steps ?? [],
          }))}
        />
      )}

      {running.map((t) => {
        const { kind, command } = splitPhase(t.phase ?? "");
        const since = staleness(t.progress_at);
        const elapsed = elapsedSince(t.phase_since);
        return (
          <div key={t.task_id} className="rounded-2xl bg-white border border-sand-200 p-4 flex flex-col gap-2.5 shadow-2xs">
            <div className="flex items-center gap-2.5 text-xs">
              <ThinkingOrb
                state={orbStateForPhase(t.phase)}
                size={24}
                className="shrink-0"
                aria-label={`${kind || "working"}${command ? `: ${command}` : ""}`}
              />
              <span className="font-bold text-stone-900">{kind || "working"}</span>
              {command && (
                <code className="text-[11px] text-stone-700 bg-sand-100 px-2 py-0.5 rounded-lg border border-sand-200 font-mono truncate max-w-sm">
                  {command}
                </code>
              )}
              {elapsed !== null && (
                <span className="text-[11px] text-stone-500 font-mono tabular-nums shrink-0">
                  {formatElapsed(elapsed)}
                </span>
              )}
              {since !== null && since > 30 && (
                <span className="ml-auto text-[11px] text-amber-700 font-semibold shrink-0 bg-amber-50 px-2 py-0.5 rounded border border-amber-200">
                  no update for {since}s
                </span>
              )}
            </div>
            {t.output_tail ? <OutputTail text={t.output_tail} /> : null}
          </div>
        );
      })}
    </div>
  );
}

