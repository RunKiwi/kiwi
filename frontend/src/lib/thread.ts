import type { JobTask } from "./api.ts";

/**
 * A task's thread: the run that was submitted, plus every run a review comment
 * continued it with.
 *
 * A task used to be a row — submitted, run, delivered as a pull request, done.
 * Since a comment on that pull request can continue it, several runs share one
 * branch, one pull request and one session, and the dashboard has to be able
 * to show that as one thing.
 *
 * The shape is a tree rather than a list because forking any task is next, and
 * a fork is simply a second child of the same parent. Nothing creates one yet;
 * this already draws it, so forking needs an entry point and not a rewrite.
 */

export interface ThreadNode {
  task: JobTask;
  /**
   * How far this node sits from the main line. Zero for the run that was
   * submitted and for each continuation of it; one for a fork, which diverges
   * from a parent that already had a child.
   *
   * A linear thread deliberately stays flat: indenting every continuation
   * would make an ordinary back-and-forth look like a tree of decisions it
   * never was.
   */
  depth: number;
}

export interface ThreadSummary {
  runs: number;
  /** True only when a comment actually continued the work. */
  continued: boolean;
  /** The newest run's status, because that is what is happening now. */
  status: string;
  latestOrigin: string;
}

function queuedAt(task: JobTask): number {
  const t = Date.parse(task.queued_at);
  return Number.isNaN(t) ? 0 : t;
}

/**
 * buildThread orders a job's tasks oldest-first and works out each one's
 * depth.
 *
 * Every task is kept, including one whose parent is missing from the payload.
 * Dropping a node because a row was filtered upstream would hide work that
 * really ran, which is worse than drawing it in the wrong place.
 */
export function buildThread(tasks: JobTask[]): ThreadNode[] {
  const ordered = [...tasks].sort((a, b) => {
    const diff = queuedAt(a) - queuedAt(b);
    return diff !== 0 ? diff : a.id.localeCompare(b.id);
  });

  // The first child continues its parent's line; any later child diverged from
  // it, which is what a fork is.
  const childCount = new Map<string, number>();
  const depthOf = new Map<string, number>();

  for (const task of ordered) {
    const parent = task.parent_task_id;
    if (!parent || !depthOf.has(parent)) {
      depthOf.set(task.id, 0);
      continue;
    }
    const seen = childCount.get(parent) ?? 0;
    childCount.set(parent, seen + 1);
    depthOf.set(task.id, seen === 0 ? depthOf.get(parent)! : depthOf.get(parent)! + 1);
  }

  return ordered.map((task) => ({ task, depth: depthOf.get(task.id) ?? 0 }));
}

/**
 * selectedNode resolves the node a URL points at.
 *
 * The page is reachable by any member of a thread, so an unknown id is not an
 * error worth showing a blank pane for — it lands on the newest run, which is
 * what someone opening a thread almost always wants to see.
 */
export function selectedNode(nodes: ThreadNode[], id: string | undefined): ThreadNode | undefined {
  if (nodes.length === 0) return undefined;
  return nodes.find((n) => n.task.id === id) ?? nodes[nodes.length - 1];
}

export function threadSummary(nodes: ThreadNode[]): ThreadSummary {
  if (nodes.length === 0) {
    return { runs: 0, continued: false, status: "", latestOrigin: "" };
  }
  const newest = nodes[nodes.length - 1].task;
  // A run "continued" the thread if it wasn't the thread's own root — origin
  // alone under-counts: pr_comment and slack are the two origins a
  // continuation actually carries (see pkg/store/lineage.go), but a fork also
  // adds a run to the same list without being either. Checking parent_task_id
  // instead of enumerating origins is what keeps this correct as new origins
  // are added, rather than needing a matching update here every time.
  const continuations = nodes.filter((n) => !!n.task.parent_task_id).length;
  return {
    runs: nodes.length,
    continued: continuations > 0,
    status: newest.status,
    latestOrigin: newest.origin ?? "submit",
  };
}
