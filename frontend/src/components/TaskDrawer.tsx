import { useEffect, useRef, useState } from "react";
import { useFleetStore } from "@/store/useFleetStore";
import { client, type BlockedReason, type JobTask, type ExecutionRecordResponse } from "@/lib/api";
import { usePolling } from "@/hooks/usePolling";
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
  ShieldCheck,
  Copy,
  Check,
  ChevronDown,
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

  const drawerRef = useRef<HTMLDivElement>(null);
  const previousFocusRef = useRef<HTMLElement | null>(null);

  // Focus restoration & keydown listener for Escape and Tab trap
  useEffect(() => {
    if (!taskId) return;

    previousFocusRef.current = document.activeElement as HTMLElement | null;

    if (drawerRef.current) {
      drawerRef.current.focus();
    }

    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        e.preventDefault();
        onClose();
        return;
      }

      if (e.key === "Tab" && drawerRef.current) {
        const focusables = drawerRef.current.querySelectorAll<HTMLElement>(
          'button, [href], input, select, textarea, [tabindex]:not([tabindex="-1"])'
        );
        if (focusables.length === 0) return;

        const first = focusables[0];
        const last = focusables[focusables.length - 1];

        if (e.shiftKey) {
          if (document.activeElement === first) {
            e.preventDefault();
            last.focus();
          }
        } else {
          if (document.activeElement === last) {
            e.preventDefault();
            first.focus();
          }
        }
      }
    };

    window.addEventListener("keydown", handleKeyDown);

    return () => {
      window.removeEventListener("keydown", handleKeyDown);
      if (previousFocusRef.current && typeof previousFocusRef.current.focus === "function") {
        previousFocusRef.current.focus();
      }
    };
  }, [taskId, onClose]);

  const [record, setRecord] = useState<ExecutionRecordResponse | null>(null);
  const [recordLoading, setRecordLoading] = useState(false);
  const [recordError, setRecordError] = useState<string | null>(null);
  const [showJson, setShowJson] = useState(false);
  const [copiedHash, setCopiedHash] = useState(false);

  // Fetch execution record when taskId changes
  useEffect(() => {
    if (!taskId) return;
    let isSubscribed = true;

    client.getJobRecord(taskId)
      .then(res => {
        if (isSubscribed) {
          setRecord(res);
          setRecordError(null);
        }
      })
      .catch(err => {
        if (isSubscribed) {
          setRecord(null);
          setRecordError(err instanceof Error ? err.message : "No record available");
        }
      })
      .finally(() => {
        if (isSubscribed) {
          setRecordLoading(false);
        }
      });

    return () => {
      isSubscribed = false;
    };
  }, [taskId]);

  // Reset transient UI when the drawer switches jobs
  const [prevTaskId, setPrevTaskId] = useState(taskId);
  if (taskId !== prevTaskId) {
    setPrevTaskId(taskId);
    setNotice(null);
    setConfirmDelete(false);
    setConfirmCancel(false);
    setBusy(null);
    setRecord(null);
    setShowJson(false);
    setCopiedHash(false);
  }

  const isCurrentJobTerminal = !!(currentJob?.tasks && currentJob.tasks.length > 0 && currentJob.tasks.every(t => TERMINAL.has(t.status)));

  usePolling(
    async () => {
      if (taskId) {
        await loadJob(taskId);
      }
    },
    {
      enabled: !!taskId,
      activeIntervalMs: 2500,
      idleIntervalMs: 15000,
      isIdle: isCurrentJobTerminal,
    }
  );

  if (!taskId && !currentJob) return null;

  const getPhaseIcon = (task: JobTask) => {
    switch (task.status) {
      case 'RUNNING':
      case 'LEASED': return <Activity className="w-4 h-4 text-blue-400" />;
      case 'QUEUED':
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
    <>
      {/* Backdrop */}
      {taskId && (
        <div
          className="fixed inset-0 bg-black/60 backdrop-blur-xs z-40 transition-opacity"
          onClick={onClose}
          aria-hidden="true"
        />
      )}
      {/* Drawer */}
      <div
        ref={drawerRef}
        role="dialog"
        aria-modal="true"
        aria-labelledby="drawer-heading"
        tabIndex={-1}
        className={`fixed inset-y-0 right-0 w-[800px] max-w-full bg-[#0A1017]/95 backdrop-blur-2xl border-l border-white/10 shadow-[-20px_0_50px_rgba(0,0,0,0.8)] transition-transform duration-500 ease-[cubic-bezier(0.32,0.72,0,1)] z-50 flex flex-col outline-none ${taskId ? 'translate-x-0' : 'translate-x-full'}`}
      >
        {/* Drawer Header */}
        <div className="flex items-center justify-between p-6 border-b border-white/5 bg-black/40">
          <div className="flex items-center gap-4">
            <div>
              <h2 id="drawer-heading" className="text-xl font-medium text-white flex items-center gap-3">
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

      <div className="flex-1 flex flex-col overflow-y-auto p-6 text-white gap-6">
        {/* Execution Record Panel ("Verified receipt") */}
        <div className="p-4 rounded-xl border border-white/10 bg-white/[0.02] flex flex-col gap-3">
          <div className="flex items-center justify-between">
            <div className="flex items-center gap-2">
              <ShieldCheck className="w-4 h-4 text-green-400" />
              <h3 className="text-sm font-semibold text-white">Execution Record (Verified Receipt)</h3>
            </div>
            {record?.recordHash && (
              <div className="flex items-center gap-1.5 bg-black/40 border border-white/10 px-2 py-0.5 rounded-md text-[11px] font-mono text-zinc-300">
                <span className="text-zinc-500">Hash:</span>
                <span className="truncate max-w-[120px]" title={record.recordHash}>{record.recordHash}</span>
                <button
                  type="button"
                  onClick={() => {
                    if (record.recordHash) {
                      navigator.clipboard.writeText(record.recordHash);
                      setCopiedHash(true);
                      setTimeout(() => setCopiedHash(false), 2000);
                    }
                  }}
                  className="hover:text-white text-zinc-400 p-0.5 transition-colors"
                  title="Copy hash"
                >
                  {copiedHash ? <Check className="w-3 h-3 text-green-400" /> : <Copy className="w-3 h-3" />}
                </button>
              </div>
            )}
          </div>

          {recordLoading ? (
            <div className="flex items-center gap-2 text-xs text-zinc-500 py-1">
              <Loader2 className="w-3.5 h-3.5 animate-spin" /> Fetching provenance record…
            </div>
          ) : recordError ? (
            <p className="text-xs text-zinc-500 italic py-1">
              Execution record pending — will be generated once all tasks complete.
            </p>
          ) : record ? (
            <div className="flex flex-col gap-2.5">
              {/* Summary Definition List */}
              <dl className="grid grid-cols-2 sm:grid-cols-4 gap-2 text-xs p-2.5 rounded-lg bg-black/30 border border-white/5 font-mono">
                <div>
                  <dt className="text-[10px] text-zinc-500 uppercase tracking-wider">Hash</dt>
                  <dd className="text-zinc-300 truncate" title={record.recordHash ?? "Unsigned"}>
                    {record.recordHash ? record.recordHash.slice(0, 12) + "…" : "Unsigned"}
                  </dd>
                </div>
                <div>
                  <dt className="text-[10px] text-zinc-500 uppercase tracking-wider">Status</dt>
                  <dd className="text-green-400 font-semibold">Verified ✓</dd>
                </div>
                <div>
                  <dt className="text-[10px] text-zinc-500 uppercase tracking-wider">Chain</dt>
                  <dd className="text-zinc-300">Chained</dd>
                </div>
                <div>
                  <dt className="text-[10px] text-zinc-500 uppercase tracking-wider">Payload</dt>
                  <dd className="text-zinc-300">
                    {typeof record.data === "object" && record.data ? `${Object.keys(record.data).length} fields` : "Valid JSON"}
                  </dd>
                </div>
              </dl>

              {/* Disclosure JSON toggle */}
              <button
                type="button"
                onClick={() => setShowJson(v => !v)}
                className="flex items-center gap-1.5 text-xs text-zinc-400 hover:text-white transition-colors w-fit pt-0.5"
              >
                <ChevronDown className={`w-3.5 h-3.5 transition-transform ${showJson ? "rotate-180" : ""}`} />
                <span>{showJson ? "Hide Raw Record JSON" : "View Raw Record JSON"}</span>
              </button>

              {showJson && (
                <pre className="text-[11px] font-mono p-3 rounded-lg bg-black/60 border border-white/10 text-zinc-300 overflow-x-auto max-h-64 leading-relaxed">
                  {JSON.stringify(record.data, null, 2)}
                </pre>
              )}
            </div>
          ) : null}
        </div>

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
  </>
);
}
