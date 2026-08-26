"use client";

import { FaSlack } from "react-icons/fa6";
import type { JobTask } from "@/lib/api";

/**
 * RunDetail is the right-hand pane: what one run in a thread was asked to do,
 * and what happened.
 *
 * For a continuation the instruction is a review comment somebody wrote, so it
 * is rendered as a quote rather than as a task title. That distinction is the
 * whole point of the view: it is the evidence that a comment on a pull request
 * turned into work.
 */

function statusLabel(status: string): { text: string; className: string } {
  switch (status) {
    case "SUCCEEDED":
      return { text: "succeeded", className: "text-emerald-300 bg-emerald-500/10 border-emerald-500/20" };
    case "FAILED":
      return { text: "failed", className: "text-rose-300 bg-rose-500/10 border-rose-500/20" };
    case "CANCELLED":
      return { text: "cancelled", className: "text-zinc-400 bg-white/5 border-white/10" };
    case "LEASED":
      return { text: "running", className: "text-amber-300 bg-amber-500/10 border-amber-500/20" };
    default:
      return { text: "queued", className: "text-zinc-400 bg-white/5 border-white/10" };
  }
}

export function RunDetail({ task, children }: { task: JobTask; children?: React.ReactNode }) {
  const status = statusLabel(task.status);
  const fromComment = task.origin === "pr_comment";
  const fromSlack = task.origin === "slack";

  return (
    <section className="min-w-0 flex-1 pl-4">
      <header className="mb-3 flex flex-wrap items-center gap-2">
        <h2 className="text-[13px] text-zinc-100">{task.task || task.id}</h2>
        <span className={`rounded border px-1.5 py-0.5 text-[10px] uppercase tracking-widest ${status.className}`}>
          {status.text}
        </span>
        {fromSlack && (
          <span className="flex items-center gap-1 rounded border border-[#4A154B]/40 bg-[#4A154B]/10 px-1.5 py-0.5 text-[10px] uppercase tracking-widest text-[#ecd9ee]">
            <FaSlack className="w-2.5 h-2.5" aria-hidden="true" />
            Slack
          </span>
        )}
        {task.result_url && (
          <a
            href={task.result_url}
            target="_blank"
            rel="noreferrer"
            className="ml-auto text-[11px] text-blue-400 hover:underline"
          >
            View pull request
          </a>
        )}
      </header>

      {(fromComment || fromSlack) && (
        <blockquote className={`mb-3 border-l-2 pl-3 text-[12px] text-zinc-400 ${fromSlack ? "border-[#4A154B]/60" : "border-blue-500/50"}`}>
          {task.task}
          <span className="mt-0.5 block text-[11px] text-zinc-600">{fromSlack ? "from Slack" : "from a review comment"}</span>
        </blockquote>
      )}

      {task.blocked_detail && (
        <p className="mb-3 text-[11px] text-amber-300/90">{task.blocked_detail}</p>
      )}
      {task.result_detail && (
        <p className="mb-3 whitespace-pre-wrap text-[11px] leading-relaxed text-zinc-400">
          {task.result_detail}
        </p>
      )}

      {children}
    </section>
  );
}
