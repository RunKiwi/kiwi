"use client";

import type { ThreadNode } from "@/lib/thread";

/**
 * ThreadRail lists the runs that make up one task's thread.
 *
 * A task used to be a row, so the dashboard showed a row. Since a review
 * comment on a Kiwi pull request continues the task that opened it, a task is
 * several runs sharing one branch, one pull request and one session — and a
 * user who comments and comes back needs to see that the comment did
 * something.
 *
 * The rail is a list rather than a timeline because a fork is coming. A fork
 * is a second child of the same parent, which a rail draws as indentation and
 * a single scrolling timeline cannot draw at all.
 */

const ORIGIN_LABEL: Record<string, string> = {
  submit: "submitted",
  pr_comment: "comment",
  fork: "fork",
  postmerge_remediation: "auto-fix",
};

// Colour never carries meaning alone — every node is labelled — so this stays
// readable for a colourblind reader and in a screenshot pasted into an issue.
function statusDot(status: string): string {
  switch (status) {
    case "SUCCEEDED":
      return "bg-emerald-400";
    case "FAILED":
      return "bg-rose-400";
    case "CANCELLED":
      return "bg-zinc-500";
    case "LEASED":
      return "bg-amber-400 animate-pulse";
    default:
      return "bg-stone-300";
  }
}

export function ThreadRail({
  nodes,
  selectedId,
  onSelect,
}: {
  nodes: ThreadNode[];
  selectedId?: string;
  onSelect: (taskId: string) => void;
}) {
  return (
    <nav aria-label="Runs in this thread" className="w-[230px] shrink-0 border-r border-sand-200 pr-2">
      <div className="px-2 py-2 text-[10px] uppercase tracking-widest text-stone-400">Thread</div>
      <ul className="space-y-0.5">
        {nodes.map((node) => {
          const selected = node.task.id === selectedId;
          return (
            <li key={node.task.id} style={{ marginLeft: node.depth * 16 }}>
              <button
                type="button"
                onClick={() => onSelect(node.task.id)}
                aria-current={selected ? "true" : undefined}
                className={`w-full text-left flex gap-2 items-start rounded-md px-2 py-1.5 transition-colors ${
                  selected ? "bg-sand-50 shadow-[inset_2px_0_0_#60a5fa]" : "hover:bg-sand-50"
                }`}
              >
                <span className={`mt-1.5 h-2 w-2 shrink-0 rounded-full ${statusDot(node.task.status)}`} />
                <span className="min-w-0">
                  <span className="block truncate text-[12px] text-stone-800">
                    {node.task.task || node.task.id}
                  </span>
                  <span className="block text-[11px] text-stone-400">
                    {ORIGIN_LABEL[node.task.origin ?? "submit"] ?? "run"}
                    {node.task.status === "LEASED" ? " · running" : ""}
                  </span>
                </span>
              </button>
            </li>
          );
        })}
      </ul>
    </nav>
  );
}
