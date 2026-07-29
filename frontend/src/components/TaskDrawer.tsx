"use client";

import { useEffect, useState } from "react";
import { useFleetStore } from "@/store/useFleetStore";
import { client, type BlockedReason, type JobTask } from "@/lib/api";
import {
  X,
  Activity,
  Loader2,
  CheckCircle2,
  GitPullRequest,
  AlertTriangle,
  Clock,
  ServerCrash,
  Ban,
  RotateCcw,
  Trash2,
} from "lucide-react";

/**
 * How each blocked reason is presented. The split that matters is severity:
 * `waiting` reasons resolve on their own and should read as patience, while
 * `problem` reasons will never resolve without someone acting, and must not be
 * dressed up as progress — that ambiguity is exactly what made a job with no
 * runner look identical to one about to start.
 */
const BLOCKED_PRESENTATION: Record<
  BlockedReason,
  { label: string; tone: "waiting" | "problem" }
> = {
  awaiting_runner: { label: "Waiting for a runner", tone: "waiting" },
  provisioning: { label: "Starting your runner", tone: "waiting" },
  waiting_on_dependencies: { label: "Waiting on earlier tasks", tone: "waiting" },
  concurrency_cap: { label: "At your concurrent-task limit", tone: "waiting" },
  compute_cap: { label: "Out of agent-minutes this month", tone: "problem" },
  provision_failed: { label: "Runner failed to start", tone: "problem" },
  no_runner: { label: "No runner connected", tone: "problem" },
  runner_offline: { label: "Runner offline", tone: "problem" },
};

/** Compact age of an ISO timestamp: "12s", "4m", "2h", "3d". */
function since(iso: string): string {
  const ms = Date.now() - new Date(iso).getTime();
  if (!Number.isFinite(ms) || ms < 0) return "";
  const s = Math.floor(ms / 1000);
  if (s < 60) return `${s}s`;
  const m = Math.floor(s / 60);
  if (m < 60) return `${m}m`;
  const h = Math.floor(m / 60);
  if (h < 24) return `${h}h`;
  return `${Math.floor(h / 24)}d`;
}

/** The one-line timing summary under a task: how long it has been where it is. */
function timingLabel(task: JobTask): string {
  const parts: string[] = [];
  if (task.status === "QUEUED" && task.queued_at) {
    parts.push(`queued ${since(task.queued_at)}`);
  } else if (task.status === "LEASED" && task.started_at) {
    parts.push(`running ${since(task.started_at)}`);
  }
  if (task.attempts > 1) parts.push(`attempt ${task.attempts}`);
  if (task.leased_by) parts.push(task.leased_by);
  return parts.join(" · ");
}

function BlockedBanner({ task }: { task: JobTask }) {
  if (!task.blocked_reason) return null;
  const p = BLOCKED_PRESENTATION[task.blocked_reason];
  // An unrecognised code (a newer backend) still shows its detail rather than
  // silently rendering nothing.
  const label = p?.label ?? "Not started";
  const problem = p?.tone === "problem";

  return (
    <div
      className={`mt-1 flex items-start gap-2.5 rounded-lg border px-3 py-2 ${
        problem
          ? "border-red-500/30 bg-red-500/10"
          : "border-amber-500/25 bg-amber-500/5"
      }`}
    >
      {problem ? (
        <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0 text-red-400" />
      ) : (
        <Clock className="mt-0.5 h-4 w-4 shrink-0 text-amber-400" />
      )}
      <div className="min-w-0">
        <div
          className={`text-xs font-medium ${problem ? "text-red-300" : "text-amber-300"}`}
        >
          {label}
        </div>
        {task.blocked_detail && (
          <div className="mt-0.5 text-xs text-zinc-400">{task.blocked_detail}</div>
        )}
      </div>
    </div>
  );
}

interface TaskDrawerProps {
  taskId: string | null;
  onClose: () => void;
}

/** Terminal statuses — polling stops once every task reaches one of these. */
const TERMINAL = new Set(["SUCCEEDED", "FAILED", "CANCELLED"]);

export function TaskDrawer({ taskId, onClose }: TaskDrawerProps) {
  const { currentJob, loadJob } = useFleetStore();
  const [busy, setBusy] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);
  const [confirmDelete, setConfirmDelete] = useState(false);
  const [confirmCancel, setConfirmCancel] = useState(false);

  // Reset transient UI when the drawer switches jobs, so a notice or a primed
  // delete confirmation cannot leak onto a different job. Adjusting during
  // render rather than in an effect is React's documented pattern for
  // prop-derived state: it re-renders before committing, so the stale notice is
  // never painted at all.
  const [prevTaskId, setPrevTaskId] = useState(taskId);
  if (taskId !== prevTaskId) {
    setPrevTaskId(taskId);
    setNotice(null);
    setConfirmDelete(false);
    setConfirmCancel(false);
    setBusy(null);
  }

  useEffect(() => {
    if (!taskId) return;

    let isPolling = true;

    const fetchAndCheck = async () => {
      if (!isPolling) return;
      await loadJob(taskId);
      const state = useFleetStore.getState();
      if (state.currentJob && state.currentJob.tasks && state.currentJob.tasks.length > 0) {
        const isTerminal = state.currentJob.tasks.every(t => TERMINAL.has(t.status));
        if (isTerminal) {
          isPolling = false;
        }
      }
    };

    fetchAndCheck();
    
    const interval = setInterval(() => {
      if (isPolling) {
        fetchAndCheck();
      } else {
        clearInterval(interval);
      }
    }, 2500);

    return () => {
      isPolling = false;
      clearInterval(interval);
    };
  }, [taskId, loadJob]);

  if (!taskId && !currentJob) return (
    <div className={`fixed inset-y-0 right-0 w-[800px] max-w-full bg-[#0A1017]/95 backdrop-blur-2xl border-l border-white/10 shadow-[-20px_0_50px_rgba(0,0,0,0.8)] transition-transform duration-500 ease-[cubic-bezier(0.32,0.72,0,1)] z-50 flex flex-col translate-x-full`}></div>
  );

  const getPhaseIcon = (task: JobTask) => {
    switch (task.status) {
      case 'RUNNING':
      case 'LEASED': return <Activity className="w-4 h-4 text-blue-400" />;
      case 'QUEUED':
        // A task nobody can run is not "in progress". Swapping the spinner for a
        // static warning is the difference between the UI implying work is
        // happening and admitting that none is.
        return BLOCKED_PRESENTATION[task.blocked_reason!]?.tone === "problem"
          ? <ServerCrash className="w-4 h-4 text-red-400" />
          : <Loader2 className="w-4 h-4 text-amber-400 animate-spin" />;
      case 'SUCCEEDED': return <CheckCircle2 className="w-4 h-4 text-green-400" />;
      case 'FAILED': return <AlertTriangle className="w-4 h-4 text-red-400" />;
      case 'CANCELLED': return <Ban className="w-4 h-4 text-zinc-400" />;
      default: return null;
    }
  };

  const tasks = currentJob?.tasks ?? [];
  // What is actionable depends on where the job is. Offering "stop" on a
  // finished job or "retry" on a running one invites a click that does nothing.
  const canCancel = tasks.some(t => t.status === "QUEUED" || t.status === "LEASED");
  const canRetry = tasks.some(t => t.status === "FAILED" || t.status === "CANCELLED");

  const act = async (
    label: string,
    fn: () => Promise<{ message?: string; tasks_affected: number }>,
  ) => {
    setBusy(label);
    setNotice(null);
    try {
      const res = await fn();
      setNotice(res.message ?? `${label}: ${res.tasks_affected} task(s)`);
      if (taskId) await loadJob(taskId);
      await useFleetStore.getState().loadJobs();
    } catch (e) {
      setNotice(e instanceof Error ? e.message : `${label} failed`);
    } finally {
      setBusy(null);
      setConfirmDelete(false);
      setConfirmCancel(false);
    }
  };

  return (
    <div className={`fixed inset-y-0 right-0 w-[800px] max-w-full bg-[#0A1017]/95 backdrop-blur-2xl border-l border-white/10 shadow-[-20px_0_50px_rgba(0,0,0,0.8)] transition-transform duration-500 ease-[cubic-bezier(0.32,0.72,0,1)] z-50 flex flex-col ${taskId ? 'translate-x-0' : 'translate-x-full'}`}>
      
      {/* Drawer Header */}
      <div className="flex items-center justify-between p-6 border-b border-white/5 bg-black/40">
        <div className="flex items-center gap-4">
          <div>
            <h2 className="text-xl font-medium text-white flex items-center gap-3">
              {/* The goal, not the id — an opaque job id says nothing about what
                  is running, which is the first thing anyone opening this wants. */}
              {currentJob?.task || "Job Details"}
              {currentJob && (
                <span className="px-2 py-0.5 rounded-full text-[10px] uppercase font-bold tracking-wider bg-white/10 text-white shrink-0">
                  {currentJob.tasks.length} {currentJob.tasks.length === 1 ? "task" : "tasks"}
                </span>
              )}
            </h2>
            <p className="text-sm text-zinc-400 font-mono mt-1">
              {currentJob?.repo && <span className="text-zinc-300">{currentJob.repo} · </span>}
              {taskId}
            </p>
          </div>
        </div>
        <button onClick={onClose} className="p-2 hover:bg-white/10 rounded-full transition-colors text-zinc-400 hover:text-white">
          <X className="w-6 h-6" />
        </button>
      </div>

      {/* Job actions */}
      {currentJob && (
        <div className="flex items-center gap-2 px-6 py-3 border-b border-white/5 bg-black/20">
          <button
            onClick={() => {
              if (!confirmCancel) {
                setConfirmCancel(true);
                return;
              }
              act("Stopped", () => client.cancelJob(currentJob.job_id));
            }}
            onBlur={() => setConfirmCancel(false)}
            disabled={!canCancel || busy !== null}
            className={`flex items-center gap-2 px-3 py-1.5 rounded-lg text-xs font-medium border transition-colors disabled:opacity-40 disabled:cursor-not-allowed ${
              confirmCancel
                ? "border-amber-500/50 bg-amber-500/20 text-amber-300"
                : "border-white/10 text-zinc-300 hover:bg-white/10"
            }`}
          >
            <Ban className="w-3.5 h-3.5" />
            {confirmCancel ? "Click again to confirm" : "Stop"}
          </button>
          <button
            onClick={() => act("Retried", () => client.retryJob(currentJob.job_id))}
            disabled={!canRetry || busy !== null}
            className="flex items-center gap-2 px-3 py-1.5 rounded-lg text-xs font-medium border border-white/10 text-zinc-300 hover:bg-white/10 disabled:opacity-40 disabled:cursor-not-allowed transition-colors"
          >
            <RotateCcw className="w-3.5 h-3.5" /> Retry
          </button>

          <div className="flex-1" />

          {/* Delete is irreversible, so it takes two clicks rather than a modal:
              the second click is the confirmation. */}
          <button
            onClick={() => {
              if (!confirmDelete) {
                setConfirmDelete(true);
                return;
              }
              act("Deleted", () => client.deleteJob(currentJob.job_id)).then(onClose);
            }}
            onBlur={() => setConfirmDelete(false)}
            disabled={busy !== null}
            className={`flex items-center gap-2 px-3 py-1.5 rounded-lg text-xs font-medium border transition-colors disabled:opacity-40 ${
              confirmDelete
                ? "border-red-500/50 bg-red-500/20 text-red-300"
                : "border-white/10 text-zinc-400 hover:bg-red-500/10 hover:text-red-300"
            }`}
          >
            <Trash2 className="w-3.5 h-3.5" />
            {confirmDelete ? "Click again to confirm" : "Delete"}
          </button>

          {busy && <Loader2 className="w-4 h-4 animate-spin text-zinc-400" />}
        </div>
      )}

      {notice && (
        <div className="px-6 py-2 text-xs text-zinc-300 bg-white/5 border-b border-white/5">
          {notice}
        </div>
      )}

      <div className="flex-1 flex overflow-hidden p-6 text-white overflow-y-auto">
         {currentJob ? (
           <div className="w-full flex flex-col gap-4">
             <h3 className="text-lg font-semibold">Tasks</h3>
             {currentJob.tasks.map(task => (
               <div key={task.id} className="p-4 glass-panel flex flex-col gap-2 border border-white/10 rounded-xl">
                 <div className="flex justify-between gap-4">
                   <div className="min-w-0">
                     {task.task && <div className="text-sm text-white">{task.task}</div>}
                     <span className="font-mono text-xs text-zinc-500 break-all">{task.id}</span>
                   </div>
                   <span className="text-xs px-2 py-1 bg-white/10 rounded-md flex items-center gap-2 h-fit shrink-0">
                     {getPhaseIcon(task)} {task.status}
                   </span>
                 </div>
                 {timingLabel(task) && (
                   <div className="text-xs text-zinc-500 font-mono">{timingLabel(task)}</div>
                 )}
                 <BlockedBanner task={task} />
                 {task.result_url && (
                   <a href={task.result_url} target="_blank" rel="noreferrer" className="text-blue-400 text-sm hover:underline flex items-center gap-2 mt-2">
                     <GitPullRequest className="w-4 h-4" /> View PR
                   </a>
                 )}
                 {task.result_detail && (
                   <div className={`text-xs mt-2 ${task.status === 'FAILED' ? 'text-red-400' : 'text-zinc-400'}`}>{task.result_detail}</div>
                 )}
               </div>
             ))}
           </div>
         ) : (
           <div className="text-zinc-500">Loading...</div>
         )}
      </div>
    </div>
  );
}
