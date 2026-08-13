"use client";

import { useState } from "react";
import { ChevronDown, ChevronRight, Terminal } from "lucide-react";
import { ThinkingOrb } from "thinking-orbs";
import { RunTimeline } from "@/components/RunTimeline";
import { orbStateForPhase } from "@/lib/orbState";
import { elapsedSince } from "@/lib/progressTime";
import type { JobProgressTask } from "@/lib/api";

/**
 * LiveRun shows what a job is doing while it is still doing it.
 *
 * Before this, a running task was a spinner and an elapsed clock. A run that
 * was stuck looked exactly like one working hard, and the only way to tell was
 * to wait ten minutes for the outcome — or to read a daemon log on a machine
 * the user cannot reach.
 *
 * It reuses RunTimeline for the completed phases, so a run does not change
 * appearance the moment it finishes: the same rows stay, and the record panel
 * takes over rendering them.
 */

/** A phase string is "install: npm ci" or "test: npm test" — split for display. */
function splitPhase(phase: string): { kind: string; command: string } {
  const i = phase.indexOf(":");
  if (i === -1) return { kind: phase, command: "" };
  return { kind: phase.slice(0, i), command: phase.slice(i + 1).trim() };
}

/** Seconds since the daemon last reported, or null when it never has. */
function staleness(progressAt?: string): number | null {
  if (!progressAt) return null;
  const t = Date.parse(progressAt);
  if (Number.isNaN(t)) return null;
  return Math.max(0, Math.round((Date.now() - t) / 1000));
}

/** Seconds as "12s" or "4m32s" — short enough for an inline badge. */
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
    <div className="rounded-md bg-black/40 border border-white/5 overflow-hidden">
      <button
        type="button"
        onClick={() => setOpen(v => !v)}
        className="w-full flex items-center gap-1.5 px-2.5 py-1.5 text-[11px] text-zinc-500 hover:text-zinc-300 transition-colors"
        aria-expanded={open}
      >
        {open ? <ChevronDown className="w-3 h-3" /> : <ChevronRight className="w-3 h-3" />}
        <Terminal className="w-3 h-3" />
        <span>Output</span>
        <span className="text-zinc-600">last {lines.length} line{lines.length === 1 ? "" : "s"}</span>
      </button>
      {open && (
        // Scrolls inside its own box: command output is arbitrarily wide and
        // must never make the drawer scroll sideways.
        <pre className="px-2.5 pb-2 text-[11px] leading-relaxed text-zinc-400 font-mono overflow-x-auto whitespace-pre">
          {lines.join("\n")}
        </pre>
      )}
    </div>
  );
}

export function LiveRun({ tasks }: { tasks: JobProgressTask[] }) {
  const running = tasks.filter(t => t.status === "LEASED" || t.status === "RUNNING");
  const anySteps = tasks.some(t => (t.steps?.length ?? 0) > 0);
  if (running.length === 0 && !anySteps) return null;

  return (
    <div className="flex flex-col gap-3">
      {/* Phases completed so far. The same component the finished job uses, so
          nothing shifts when the run ends. */}
      {anySteps && (
        <RunTimeline
          workers={tasks.map(t => ({
            worker_id: t.task_id,
            actor_model: t.actor_model,
            steps: t.steps ?? [],
          }))}
        />
      )}

      {running.map(t => {
        const { kind, command } = splitPhase(t.phase ?? "");
        const since = staleness(t.progress_at);
        const elapsed = elapsedSince(t.phase_since);
        return (
          <div key={t.task_id} className="rounded-lg bg-black/30 border border-white/5 p-2.5 flex flex-col gap-2">
            <div className="flex items-center gap-2 text-xs">
              {/* The animation names the phase rather than merely asserting that
                  one exists: reading the tree does not look like installing
                  dependencies. Under prefers-reduced-motion the orb paints a
                  single static frame, and the label beside it carries the same
                  information either way. */}
              <ThinkingOrb
                state={orbStateForPhase(t.phase)}
                size={20}
                className="shrink-0"
                aria-label={`${kind || "working"}${command ? `: ${command}` : ""}`}
              />
              <span className="text-zinc-300">{kind || "working"}</span>
              {command && <code className="text-[11px] text-zinc-500 font-mono truncate">{command}</code>}
              {/* How long the CURRENT phase has taken — distinct from the
                  staleness warning below, which is about whether the feed
                  itself is still arriving. */}
              {elapsed !== null && (
                <span className="text-[11px] text-zinc-500 font-mono tabular-nums shrink-0">
                  {formatElapsed(elapsed)}
                </span>
              )}
              {/* A timestamp that stops advancing is how a hung run tells itself
                  apart from a slow one, so it is stated rather than hidden. */}
              {since !== null && since > 30 && (
                <span className="ml-auto text-[11px] text-amber-400/80 shrink-0">
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
