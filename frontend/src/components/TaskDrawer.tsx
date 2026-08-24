"use client";

import { useEffect, useRef, useState } from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useFleetStore } from "@/store/useFleetStore";
import { client, type BlockedReason, type JobTask, type ExecutionRecordResponse, type ExecutionRecordBody, type Job, type JobProgressTask, type RecordStep, type RecordWorker } from "@/lib/api";
import { durationBetween, formatCost, formatTokens } from "@/lib/datetime";
import { ThinkingOrb } from "@/components/ThinkingOrb";
import { RunTimeline } from "@/components/RunTimeline";
import { LiveRun } from "@/components/LiveRun";
import { LoadingState } from "@/components/LoadingState";
import { jobOrbState } from "@/lib/orbState";
import { usePolling } from "@/hooks/usePolling";
import { parseActionableError } from "@/lib/errors";
import { jobTitle } from "@/lib/taskTitle";
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
  Compass,
  Hammer,
} from "lucide-react";
import { JobGraph } from "@/components/JobGraph";
import { PlanApprovalCard } from "@/components/PlanApprovalCard";
import { api, type JobPlan } from "@/lib/api";
import { GroupedDiffViewer } from "@/components/CodeView";
import { parseToolArgs, languageOf, editDiff, parseUnifiedDiff, groupDiffsByFile } from "@/lib/toolContent";

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
      className={`mt-1 flex items-start gap-2.5 rounded-xl border px-3 py-2 ${
        problem
          ? "border-rose-200 bg-rose-50"
          : "border-amber-200 bg-amber-50"
      }`}
    >
      {problem ? (
        <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0 text-rose-600" />
      ) : (
        <Clock className="mt-0.5 h-4 w-4 shrink-0 text-amber-600" />
      )}
      <div className="min-w-0">
        <div
          className={`text-xs font-medium ${problem ? "text-rose-800" : "text-amber-800"}`}
        >
          {label}
        </div>
        {task.blocked_detail && (
          <div className="mt-0.5 text-xs text-stone-500">{task.blocked_detail}</div>
        )}
      </div>
    </div>
  );
}

function shorten(s: string, n: number): string {
  const flat = s.replace(/\s+/g, " ").trim();
  return flat.length > n ? flat.slice(0, n) + "…" : flat;
}

const LOG_TONE_CLASS: Record<string, string> = {
  info: "text-stone-300",
  pass: "text-emerald-400",
  fail: "text-rose-400",
  warn: "text-amber-400",
};

/** Every step's phase/outcome, plus whatever raw output a running task has
 * reported, read straight off the same JobProgressTask data LiveRun already
 * renders — this is a plain-text reformat of real data, not a second feed. */
function LiveLogsTab({ progress }: { progress: JobProgressTask[] }) {
  const lines: { key: string; text: string; tone: keyof typeof LOG_TONE_CLASS }[] = [];
  progress.forEach((t) => {
    (t.steps ?? []).forEach((s, i) => {
      const tone: keyof typeof LOG_TONE_CLASS =
        s.outcome === "fail" || s.outcome === "error" ? "fail"
        : s.outcome === "pass" || s.outcome === "approved" ? "pass"
        : s.outcome === "rejected" ? "warn"
        : "info";
      const detail = s.detail || s.reasons;
      lines.push({
        key: `${t.task_id}-step-${i}`,
        text: `[step ${s.step}] ${s.phase}${s.outcome ? ` → ${s.outcome}` : ""}${detail ? `: ${shorten(detail, 200)}` : ""}`,
        tone,
      });
    });
    if (t.output_tail) {
      lines.push({ key: `${t.task_id}-tail`, text: t.output_tail, tone: "info" });
    }
  });

  if (lines.length === 0) {
    return (
      <div className="p-8 rounded-2xl border border-sand-200 bg-white shadow-2xs text-center text-xs text-stone-400">
        No log output yet.
      </div>
    );
  }

  return (
    <div className="rounded-2xl overflow-hidden border border-stone-800 shadow-sm">
      <div className="px-3 py-2 bg-stone-900 border-b border-stone-800 text-[11px] font-mono text-stone-400 flex items-center justify-between">
        <span>Runner output</span>
        <span className="flex items-center gap-1.5 text-emerald-400">
          <span className="w-1.5 h-1.5 rounded-full bg-emerald-500 animate-pulse" />
          live
        </span>
      </div>
      <div className="bg-stone-950 font-mono text-[11px] leading-relaxed p-4 max-h-[420px] overflow-y-auto whitespace-pre-wrap break-words">
        {lines.map((l) => (
          <div key={l.key} className={LOG_TONE_CLASS[l.tone]}>{l.text}</div>
        ))}
      </div>
    </div>
  );
}

/** Every edit_file call's diff, pulled out of the same steps RunTimeline shows
 * inline — a filtered view onto real diffs, not a second diffing pipeline. */
function CodeDiffTab({
  progress,
  record,
  jobFinished,
}: {
  progress: JobProgressTask[];
  record: ExecutionRecordResponse | null;
  jobFinished: boolean;
}) {
  const liveSteps = progress.flatMap((t) => t.steps ?? []);
  const recordWorkers: RecordWorker[] = jobFinished && record
    ? ((record.data as ExecutionRecordBody | undefined)?.execution?.workers ?? [])
    : [];
  const recordSteps = recordWorkers.flatMap((w) => w.steps ?? []);
  const steps: RecordStep[] = liveSteps.length > 0 ? liveSteps : recordSteps;

  const rawEdits = steps
    .map((row) => {
      const toolArgs = parseToolArgs(row.input);
      const lang = languageOf(toolArgs.path);
      const reported = row.detail ? parseUnifiedDiff(row.detail) : null;
      if (reported) {
        return {
          path: reported.path ?? toolArgs.path,
          lines: reported.lines,
          hunks: reported.hunks,
          lang: languageOf(reported.path ?? toolArgs.path),
          truncated: reported.truncated,
        };
      }
      if (toolArgs.oldString !== undefined && toolArgs.newString !== undefined) {
        return {
          path: toolArgs.path,
          lines: editDiff(toolArgs.oldString, toolArgs.newString),
          lang,
          truncated: false,
        };
      }
      if (toolArgs.content !== undefined && toolArgs.path) {
        return {
          path: toolArgs.path,
          lines: editDiff("", toolArgs.content),
          lang,
          truncated: false,
        };
      }
      return null;
    })
    .filter((d): d is NonNullable<typeof d> => d !== null);

  const fileGroups = groupDiffsByFile(rawEdits);

  return <GroupedDiffViewer files={fileGroups} />;
}

interface TaskDrawerProps {
  taskId: string | null;
  onClose: () => void;
  onRerunWithEdits?: (job: Job) => void;
}

/** Terminal statuses — polling stops once every task reaches one of these. */
const TERMINAL = new Set(["SUCCEEDED", "FAILED", "CANCELLED"]);

export function TaskDrawer({ taskId, onClose, onRerunWithEdits }: TaskDrawerProps) {
  const router = useRouter();
  const { currentJob: storeJob, loadJob, jobs } = useFleetStore();
  const currentJob = storeJob?.job_id === taskId ? storeJob : null;
  const summaryJob = jobs.find((j) => j.job_id === taskId);
  const [busy, setBusy] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);
  const [confirmDelete, setConfirmDelete] = useState(false);
  const [confirmCancel, setConfirmCancel] = useState(false);
  const [jobPlan, setJobPlan] = useState<JobPlan | null>(null);

  const handleRerunWithEdits = () => {
    if (!currentJob && !summaryJob) return;
    const j = currentJob || summaryJob;
    if (onRerunWithEdits && currentJob) {
      onRerunWithEdits(currentJob);
      onClose();
      return;
    }
    const params = new URLSearchParams();
    const taskPrompt = j?.task || currentJob?.tasks?.[0]?.task || summaryJob?.task || "";
    if (taskPrompt) params.set("task", taskPrompt);

    const repoName = j?.repo || summaryJob?.repo || "";
    if (repoName) params.set("repo", repoName);

    const archModel =
      j?.architect_model ||
      summaryJob?.architect_model ||
      jobPlan?.architect_model ||
      currentJob?.tasks?.[0]?.architect_model ||
      "claude-sonnet-5";
    params.set("architect_model", archModel);

    const workerModel =
      j?.worker_model ||
      summaryJob?.worker_model ||
      currentJob?.tasks?.[0]?.model ||
      "claude-haiku-4-5-20251001";
    params.set("worker_model", workerModel);

    const spendCap = j?.spend_cap_usd ?? summaryJob?.spend_cap_usd;
    if (spendCap != null) params.set("spend_cap", String(spendCap));

    const isDryRun = j?.is_dry_run ?? summaryJob?.is_dry_run;
    if (isDryRun) params.set("dry_run", "true");

    const requiresPlan = j?.requires_plan_approval || j?.plan_status || summaryJob?.requires_plan_approval || summaryJob?.plan_status;
    if (requiresPlan) params.set("strategy", "plan");

    const jid = j?.job_id || summaryJob?.job_id || taskId;
    if (jid) params.set("job_id", jid);

    router.push(`/composer?${params.toString()}`);
    onClose();
  };

  useEffect(() => {
    if (!taskId) return;
    api.getJobPlan(taskId).then(setJobPlan).catch(() => setJobPlan(null));
  }, [taskId]);

  const drawerRef = useRef<HTMLDivElement>(null);
  const previousFocusRef = useRef<HTMLElement | null>(null);

  const isOpen = !!taskId;

  // onClose is typically redefined on every parent render, so it cannot sit in a
  // dependency array here: the dashboard re-renders on each poll, which would
  // re-run the focus effect every few seconds — stealing focus back to the drawer
  // mid-interaction and overwriting the element we promised to restore focus to.
  const onCloseRef = useRef(onClose);
  useEffect(() => { onCloseRef.current = onClose; }, [onClose]);

  // Capture and restore focus across the open/close transition only. Keyed on
  // isOpen rather than taskId so switching between jobs does not bounce focus
  // out to the card and back.
  useEffect(() => {
    if (!isOpen) return;
    previousFocusRef.current = document.activeElement as HTMLElement | null;
    drawerRef.current?.focus();
    return () => {
      previousFocusRef.current?.focus?.();
    };
  }, [isOpen]);

  // Escape to close, Tab to cycle within the dialog.
  useEffect(() => {
    if (!isOpen) return;

    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        e.preventDefault();
        onCloseRef.current();
        return;
      }

      if (e.key === "Tab" && drawerRef.current) {
        // Disabled controls are not focusable, so including them would hand the
        // trap a first/last element that silently refuses focus and let Tab
        // escape the dialog. Stop/Retry/Delete are all conditionally disabled.
        const focusables = Array.from(
          drawerRef.current.querySelectorAll<HTMLElement>(
            'button:not([disabled]), [href], input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])',
          ),
        ).filter(el => el.offsetParent !== null || el === document.activeElement);
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
    return () => window.removeEventListener("keydown", handleKeyDown);
  }, [isOpen]);

  // Live progress, polled while the job runs. Cleared on job switch below so
  // one run's output can never appear under another's header.
  const [progress, setProgress] = useState<JobProgressTask[]>([]);
  // Set once the post-finish fetch has landed, so the trailing read happens
  // exactly once rather than on every idle tick for the life of the drawer.
  const [finalProgress, setFinalProgress] = useState(false);
  const [record, setRecord] = useState<ExecutionRecordResponse | null>(null);
  // Which job's title is expanded, rather than a bare boolean. Opening a
  // different job must not inherit the previous one's expanded state, and
  // deriving that from the id is what avoids resetting it from an effect.
  const [expandedFor, setExpandedFor] = useState<string | null>(null);
  const [recordError, setRecordError] = useState<string | null>(null);
  const [showJson, setShowJson] = useState(false);
  const [copiedHash, setCopiedHash] = useState(false);
  const [drawerTab, setDrawerTab] = useState<"timeline" | "logs" | "diff" | "verification">("timeline");

  // A record is only assembled once a job reaches a terminal state, so asking
  // for one mid-run just buys a guaranteed 404 on every drawer open. Wait until
  // the job has actually finished.
  const jobFinished =
    !!currentJob && currentJob.tasks.length > 0 && currentJob.tasks.every(t => TERMINAL.has(t.status));

  // Derived rather than held in state: the fetch is in flight exactly while the
  // job is finished and neither a record nor an error has landed. Holding a
  // separate flag would mean setting state from inside the effect body.
  const recordPending = jobFinished && !record && !recordError;

  useEffect(() => {
    if (!taskId || !jobFinished) return;
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
          // A job with no record is the normal case, not a failure worth
          // reporting — the panel simply does not appear.
          setRecordError(err instanceof Error ? err.message : "No record available");
        }
      })

    return () => {
      isSubscribed = false;
    };
  }, [taskId, jobFinished]);

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
    setRecord(null);
    setRecordError(null);
    setShowJson(false);
    setCopiedHash(false);
    setProgress([]);
    setFinalProgress(false);
    setDrawerTab("timeline");
    setJobPlan(null);
  }

  usePolling(
    async () => {
      if (!taskId) return;
      await loadJob(taskId);
      // Progress is fetched while the job runs AND once more after it ends.
      //
      // It used to stop at the finish line, on the assumption that a finished
      // job has a record instead. It does — but the record deliberately carries
      // `detail_hash` rather than `detail` (pkg/ver/record.go), and quotes
      // prose only for the Critic. So for a session run, whose phases are
      // `actor:<tool>`, everything the run said about itself disappeared the
      // moment it succeeded: the tool output, the commands, the whole account.
      //
      // The rows are still in task_events and still served here. One trailing
      // fetch keeps them, and finalProgress stops it repeating forever.
      if (!jobFinished || !finalProgress) {
        try {
          const res = await client.getJobProgress(taskId);
          setProgress(res.tasks ?? []);
          if (jobFinished) setFinalProgress(true);
        } catch {
          // Best-effort, exactly as it is on the daemon side: a run must never
          // look broken because its commentary did not load.
        }
      }
    },
    {
      enabled: !!taskId,
      activeIntervalMs: 2500,
      idleIntervalMs: 15000,
      // A finished job still gets an occasional check rather than none: the PR
      // URL and result detail can land moments after the last task reports.
      isIdle: jobFinished,
    }
  );

  if (!taskId && !currentJob) return null;

  // Null unless a task is actually executing or its runner is coming up, so the
  // header orb appears only while there is something to think about. Both feeds
  // are passed: a job still cold-starting its runner has no progress at all.
  const headerOrb = jobOrbState(progress, currentJob?.tasks ?? []);

  // The drawer's title. Once the Architect has written its opening objective
  // that becomes the title; until then the task's own first sentence stands in.
  // Neither costs a model call — see lib/taskTitle.ts.
  const titleExpanded = expandedFor !== null && expandedFor === taskId;

  const heading = jobTitle(
    currentJob?.task ?? "",
    progress.flatMap(p => p.steps ?? []),
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

  // Compute top telemetry metrics
  const recordBody = (record?.data ?? {}) as ExecutionRecordBody;
  const workers = recordBody.execution?.workers ?? [];
  const sumSteps = (pick: (s: RecordStep) => number | undefined) =>
    progress.reduce((n, p) => n + (p.steps ?? []).reduce((m, s) => m + (pick(s) ?? 0), 0), 0);

  const recordTokens = workers.reduce(
    (n, w) => n + (w.input_tokens ?? 0) + (w.output_tokens ?? 0), 0);
  const totalTokens = recordTokens > 0
    ? recordTokens
    : sumSteps((s) => s.input_tokens) + sumSteps((s) => s.output_tokens);

  const recordCost = workers.reduce((n, w) => n + (w.cost_usd ?? 0), 0);
  const totalCost = recordCost > 0 ? recordCost : sumSteps((s) => s.cost_usd);

  const tasksList = currentJob?.tasks ?? [];
  const queuedAts = tasksList.map((t) => t.queued_at).filter(Boolean).sort() as string[];
  const startedAts = tasksList.map((t) => t.started_at).filter(Boolean).sort() as string[];
  const submitted = queuedAts[0];
  const started = startedAts[0];
  const endpoint = jobFinished
    ? tasksList.map((t) => t.updated_at).filter(Boolean).sort().pop()
    : new Date().toISOString();

  const totalRan = durationBetween(started || submitted, endpoint);

  // Overall status classification
  const isAwaitingApproval = jobPlan?.plan_status === "pending_review";
  const isFailed = tasksList.some((t) => t.status === "FAILED") || jobPlan?.plan_status === "rejected";
  const isSucceeded = tasksList.length > 0 && tasksList.every((t) => t.status === "SUCCEEDED");
  const isCancelled = tasksList.length > 0 && tasksList.every((t) => t.status === "CANCELLED");
  const isRunning = tasksList.some((t) => t.status === "LEASED" || t.status === "RUNNING");
  const isQueued = tasksList.some((t) => t.status === "QUEUED");

  let statusBadge = { text: "QUEUED", bg: "bg-sand-100 text-stone-700 border-sand-200" };
  if (isAwaitingApproval) {
    statusBadge = { text: "AWAITING APPROVAL", bg: "bg-indigo-50 text-indigo-800 border-indigo-200" };
  } else if (isRunning) {
    statusBadge = { text: "RUNNING", bg: "bg-emerald-50 text-emerald-700 border-emerald-200" };
  } else if (isSucceeded) {
    statusBadge = { text: "SUCCEEDED", bg: "bg-emerald-50 text-emerald-800 border-emerald-200" };
  } else if (isFailed) {
    statusBadge = { text: "FAILED", bg: "bg-rose-50 text-rose-700 border-rose-200" };
  } else if (isCancelled) {
    statusBadge = { text: "CANCELLED", bg: "bg-sand-100 text-stone-600 border-sand-200" };
  } else if (isQueued) {
    statusBadge = { text: "QUEUED", bg: "bg-amber-50 text-amber-800 border-amber-200" };
  }

  // Verification / sandbox outcome
  let testOutcome = "—";
  let testOutcomeClass = "text-stone-500";
  if (recordBody.verification?.duration_ms || isSucceeded) {
    testOutcome = "PASSED";
    testOutcomeClass = "text-emerald-700 font-bold";
  } else if (isFailed) {
    testOutcome = "FAILED";
    testOutcomeClass = "text-rose-600 font-bold";
  } else if (isRunning) {
    testOutcome = "TESTING";
    testOutcomeClass = "text-amber-700 font-bold";
  }

  // Model names
  const architectModel =
    currentJob?.architect_model ||
    tasksList.find((t) => t.architect_model)?.architect_model ||
    workers.find((w) => w.critic_model)?.critic_model ||
    null;

  const workerModel =
    currentJob?.worker_model ||
    tasksList.find((t) => t.model)?.model ||
    workers.find((w) => w.actor_model)?.actor_model ||
    progress.find((p) => p.actor_model)?.actor_model ||
    null;

  const modelsUsed = new Set<string>();
  if (architectModel) modelsUsed.add(architectModel);
  if (workerModel) modelsUsed.add(workerModel);
  for (const w of workers) {
    if (w.actor_model) modelsUsed.add(w.actor_model);
    if (w.critic_model) modelsUsed.add(w.critic_model);
  }
  for (const p of progress) if (p.actor_model) modelsUsed.add(p.actor_model);
  for (const t of tasksList) {
    if (t.model) modelsUsed.add(t.model);
    if (t.architect_model) modelsUsed.add(t.architect_model);
  }

  // Result PR Link if available
  const prResultUrl = tasksList.find((t) => t.result_url)?.result_url ||
    (currentJob?.repo && currentJob?.pr_number ? `https://github.com/${currentJob.repo}/pull/${currentJob.pr_number}` : null);

  return (
    <>
      {/* Backdrop */}
      {taskId && (
        <div
          className="fixed inset-0 bg-stone-900/30 backdrop-blur-xs z-40 transition-opacity"
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
        className={`fixed inset-y-0 right-0 w-full sm:w-[800px] max-w-full bg-white border-l border-sand-200 shadow-popover transition-transform duration-300 ease-[cubic-bezier(0.32,0.72,0,1)] z-50 flex flex-col outline-none ${
          taskId ? "translate-x-0" : "translate-x-full"
        }`}
      >
        {/* ================= DRAWER HEADER ================= */}
        <div className="flex items-center justify-between p-4 sm:p-5 border-b border-sand-200 bg-sand-50/60 shrink-0">
          <div className="flex items-center gap-3.5 min-w-0">
            {headerOrb && (
              <ThinkingOrb
                state={headerOrb}
                size={42}
                className="shrink-0"
                aria-label="Job running"
              />
            )}
            <div className="min-w-0">
              <div className="flex items-center gap-2 flex-wrap">
                <span
                  className={`font-mono text-xs font-bold px-2 py-0.5 rounded-md border shadow-2xs ${
                    isAwaitingApproval
                      ? "bg-indigo-50 text-indigo-900 border-indigo-200"
                      : isRunning
                      ? "bg-emerald-50 text-emerald-900 border-emerald-200"
                      : isSucceeded
                      ? "bg-purple-50 text-purple-900 border-purple-200"
                      : isFailed
                      ? "bg-rose-50/80 text-rose-900 border-rose-200"
                      : isCancelled
                      ? "bg-sand-100 text-stone-500 border-sand-200"
                      : "bg-sand-100 text-stone-900 border-sand-200"
                  }`}
                >
                  #{taskId?.slice(0, 14)}
                </span>
                <span className={`px-2 py-0.5 rounded-full text-[10px] font-mono font-bold border ${statusBadge.bg}`}>
                  {statusBadge.text}
                </span>
                {currentJob && (
                  <span className="px-2 py-0.5 rounded-full text-[10px] uppercase font-bold tracking-wider bg-sand-150 text-stone-700 border border-sand-200 shrink-0">
                    {currentJob.tasks.length} {currentJob.tasks.length === 1 ? "task" : "tasks"}
                  </span>
                )}
              </div>

              <h2 id="drawer-heading" className="text-sm font-bold text-stone-900 line-clamp-2 mt-1">
                {heading.title}
              </h2>

              {heading.truncated && (
                <button
                  type="button"
                  onClick={() => setExpandedFor(titleExpanded ? null : taskId ?? null)}
                  aria-expanded={titleExpanded}
                  className="mt-0.5 inline-flex items-center gap-1 text-[11px] text-stone-500 hover:text-stone-900 transition-colors"
                >
                  {titleExpanded ? "Show less" : "Show full task"}
                  {heading.fromArchitect && !titleExpanded && (
                    <span className="text-stone-400">· summarised by the Architect</span>
                  )}
                </button>
              )}

              {titleExpanded && (
                <p className="mt-2 text-xs text-stone-700 whitespace-pre-wrap break-words max-h-48 overflow-y-auto bg-white p-3 rounded-xl border border-sand-200">
                  {currentJob?.task}
                </p>
              )}

              <div className="flex items-center gap-2 text-xs text-stone-500 font-mono mt-1.5 flex-wrap">
                {currentJob?.repo && <span className="text-stone-700 font-semibold">{currentJob.repo}</span>}
                {architectModel && workerModel && architectModel !== workerModel ? (
                  <div className="flex items-center gap-2 flex-wrap">
                    <span className="text-stone-300">•</span>
                    <span className="flex items-center gap-1 text-stone-700 font-medium bg-sand-100/90 px-2 py-0.5 rounded-md border border-sand-200 text-[11px]">
                      <Compass className="w-3 h-3 text-indigo-600" />
                      <span>Architect: {architectModel.split("/").pop()}</span>
                    </span>
                    <span className="flex items-center gap-1 text-stone-700 font-medium bg-sand-100/90 px-2 py-0.5 rounded-md border border-sand-200 text-[11px]">
                      <Hammer className="w-3 h-3 text-emerald-600" />
                      <span>Implementer: {workerModel.split("/").pop()}</span>
                    </span>
                  </div>
                ) : workerModel ? (
                  <div className="flex items-center gap-1.5">
                    <span className="text-stone-300">•</span>
                    <span className="flex items-center gap-1 text-stone-700 font-medium bg-sand-100/90 px-2 py-0.5 rounded-md border border-sand-200 text-[11px]">
                      <Hammer className="w-3 h-3 text-emerald-600" />
                      <span>Model: {workerModel.split("/").pop()}</span>
                    </span>
                  </div>
                ) : modelsUsed.size > 0 ? (
                  <span className="text-stone-400">• {[...modelsUsed].map((m) => m.split("/").pop()).join(", ")}</span>
                ) : null}

                {(currentJob?.tasks.some((t) => t.origin === "pr_comment") ?? false) && taskId && (
                  <Link
                    href={`/tasks/${taskId}`}
                    className="text-sky-700 hover:underline font-sans ml-1"
                  >
                    View thread →
                  </Link>
                )}
              </div>
            </div>
          </div>

          <button
            onClick={onClose}
            className="p-1.5 hover:bg-sand-150 rounded-xl transition-colors text-stone-400 hover:text-stone-800 shrink-0"
            aria-label="Close Drawer"
          >
            <X className="w-5 h-5" />
          </button>
        </div>

        {/* ================= 4-COLUMN TELEMETRY SUMMARY STRIP ================= */}
        <div className="grid grid-cols-4 border-b border-sand-200 bg-sand-50/70 text-center py-2 text-xs font-mono shrink-0">
          <div className="border-r border-sand-200 px-1">
            <span className="text-[10px] text-stone-400 block font-semibold uppercase">ELAPSED</span>
            <span className="text-stone-900 font-bold">{totalRan || "0s"}</span>
          </div>
          <div className="border-r border-sand-200 px-1">
            <span className="text-[10px] text-stone-400 block font-semibold uppercase">COST</span>
            <span className="text-kiwi-700 font-bold">{formatCost(totalCost)}</span>
          </div>
          <div className="border-r border-sand-200 px-1">
            <span className="text-[10px] text-stone-400 block font-semibold uppercase">TOKENS</span>
            <span className="text-sky-700 font-bold">{totalTokens > 0 ? formatTokens(totalTokens) : "—"}</span>
          </div>
          <div className="px-1">
            <span className="text-[10px] text-stone-400 block font-semibold uppercase">TEST STATUS</span>
            <span className={testOutcomeClass}>{testOutcome}</span>
          </div>
        </div>

        {/* ================= ACTION TOOLBAR ================= */}
        {currentJob && (
          <div className="flex items-center gap-2 px-3.5 sm:px-5 py-2 sm:py-2.5 border-b border-sand-200 bg-white shrink-0 overflow-x-auto no-scrollbar">
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
              className={`flex items-center gap-1.5 px-3 py-1.5 rounded-xl text-xs font-semibold border transition-colors disabled:opacity-40 disabled:cursor-not-allowed ${
                confirmCancel
                  ? "border-amber-300 bg-amber-50 text-amber-800"
                  : "border-sand-200 text-stone-700 hover:bg-sand-100"
              }`}
            >
              <Ban className="w-3.5 h-3.5" />
              {confirmCancel ? "Confirm Stop" : "Stop"}
            </button>

            <button
              onClick={() => act("Retried", () => client.retryJob(currentJob.job_id))}
              disabled={!canRetry || busy !== null}
              className="flex items-center gap-1.5 px-3 py-1.5 rounded-xl text-xs font-semibold border border-sand-200 text-stone-700 hover:bg-sand-100 disabled:opacity-40 disabled:cursor-not-allowed transition-colors"
            >
              <RotateCcw className="w-3.5 h-3.5" /> Retry
            </button>

            <button
              type="button"
              onClick={handleRerunWithEdits}
              className="flex items-center gap-1.5 px-3 py-1.5 rounded-xl text-xs font-semibold border border-sand-200 text-stone-700 hover:bg-sand-100 cursor-pointer transition-colors"
            >
              <Copy className="w-3.5 h-3.5 text-kiwi-600" /> Re-run with edits
            </button>

            <div className="flex-1" />

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
              className={`flex items-center gap-1.5 px-3 py-1.5 rounded-xl text-xs font-semibold border transition-colors disabled:opacity-40 ${
                confirmDelete
                  ? "border-rose-300 bg-rose-50 text-rose-700"
                  : "border-sand-200 text-stone-500 hover:bg-rose-50 hover:text-rose-700"
              }`}
            >
              <Trash2 className="w-3.5 h-3.5" />
              {confirmDelete ? "Confirm Delete" : "Delete"}
            </button>

            {busy && <Loader2 className="w-4 h-4 animate-spin text-stone-400" />}
          </div>
        )}

        {notice && (() => {
          const err = parseActionableError(notice);
          return (
            <div className="px-5 py-2 text-xs text-stone-700 bg-sand-50 border-b border-sand-200 flex items-center justify-between gap-2 shrink-0">
              <span>{err.message}</span>
              {err.actionHref && err.actionLabel && (
                <Link href={err.actionHref} className="underline font-semibold text-amber-700 hover:text-stone-900 shrink-0">
                  {err.actionLabel} →
                </Link>
              )}
            </div>
          );
        })()}

        {/* ================= TAB NAVIGATION ================= */}
        <div className="flex items-center gap-4 border-b border-sand-200 px-5 text-xs font-semibold bg-white shrink-0">
          {([
            ["timeline", "Execution Plan"],
            ["logs", "Live Logs"],
            ["diff", "Code Diff"],
            ["verification", "Verification"],
          ] as const).map(([id, label]) => (
            <button
              key={id}
              type="button"
              onClick={() => setDrawerTab(id)}
              className={`py-3 border-b-2 transition-colors ${
                drawerTab === id
                  ? "text-stone-900 border-stone-900"
                  : "text-stone-500 hover:text-stone-800 border-transparent"
              }`}
            >
              {label}
            </button>
          ))}
        </div>

        {/* ================= MAIN CONTENT PANELS ================= */}
        <div className="flex-1 flex flex-col overflow-y-auto p-5 bg-sand-50/40 gap-5">
          {/* Plan Mode Review Box */}
          {jobPlan && jobPlan.plan_status === "pending_review" && (
            <PlanApprovalCard
              plan={jobPlan}
              onApproved={() => {
                loadJob(taskId!);
                api.getJobPlan(taskId!).then(setJobPlan).catch(() => {});
              }}
              onRejected={() => {
                loadJob(taskId!);
                api.getJobPlan(taskId!).then(setJobPlan).catch(() => {});
              }}
            />
          )}

          {/* Rejected Plan Notice */}
          {jobPlan?.plan_status === "rejected" && (
            <div className="p-4 rounded-2xl border border-amber-200 bg-amber-50 text-xs text-amber-900 font-medium shadow-2xs">
              This plan was rejected during review, so the job did not run.
            </div>
          )}

          {currentJob ? (
            <>
              {/* Tab 1: Execution Plan */}
              {drawerTab === "timeline" && (
                <div className="flex flex-col gap-4">
                  {/* Live Progress */}
                  {progress.length > 0 && (!jobFinished || !record) && (
                    <div className="p-4 rounded-2xl border border-sand-200 bg-white shadow-2xs flex flex-col gap-3">
                      <div className="flex items-center gap-2">
                        <Activity className={`w-4 h-4 ${jobFinished ? "text-stone-400" : "text-sky-600"}`} />
                        <h3 className="text-sm font-bold text-stone-900">
                          {jobFinished ? "What happened" : "Running now"}
                        </h3>
                      </div>
                      <LiveRun tasks={progress} />
                    </div>
                  )}

                  {/* Finished Run Timeline */}
                  {jobFinished && record && (() => {
                    const recordWorkers = recordBody.execution?.workers ?? [];
                    const liveWorkers = progress
                      .filter((t) => (t.steps?.length ?? 0) > 0)
                      .map((t) => ({
                        worker_id: t.task_id,
                        actor_model: t.actor_model,
                        steps: t.steps ?? [],
                      }));
                    const workersList = liveWorkers.length > 0 ? liveWorkers : recordWorkers;
                    return workersList.length > 0 ? (
                      <div className="p-4 rounded-2xl border border-sand-200 bg-white shadow-2xs">
                        <RunTimeline workers={workersList} />
                      </div>
                    ) : null;
                  })()}

                  {/* Task Graph & Sub-tasks */}
                  <div className="w-full flex flex-col gap-4">
                    <JobGraph jobId={currentJob.job_id} tasks={currentJob.tasks} />
                    <h3 className="text-xs font-bold text-stone-900 uppercase tracking-wider">Tasks ({currentJob.tasks.length})</h3>
                    {currentJob.tasks.map((task) => (
                      <div
                        key={task.id}
                        id={`task-${task.id}`}
                        className="p-4 bg-white flex flex-col gap-2.5 border border-sand-200 rounded-2xl shadow-2xs scroll-mt-24 target:ring-2 target:ring-kiwi-300 transition-all"
                      >
                        <div className="flex justify-between gap-4">
                          <div className="min-w-0">
                            {task.task && (
                              <div className="text-xs font-semibold text-stone-900 line-clamp-2" title={task.task}>
                                {task.task}
                              </div>
                            )}
                            <span className="font-mono text-[11px] text-stone-400 break-all">{task.id}</span>
                          </div>
                          <span className="text-[11px] font-mono px-2 py-0.5 bg-sand-50 border border-sand-200 rounded-lg flex items-center gap-1.5 h-fit shrink-0 text-stone-700">
                            {getPhaseIcon(task)} {task.status}
                          </span>
                        </div>
                        {timingLabel(task) && (
                          <div className="text-[11px] text-stone-400 font-mono">{timingLabel(task)}</div>
                        )}
                        <BlockedBanner task={task} />
                        {task.result_url && (
                          <a
                            href={task.result_url}
                            target="_blank"
                            rel="noreferrer"
                            className="text-sky-700 text-xs font-semibold hover:underline flex items-center gap-1.5 mt-1"
                          >
                            <GitPullRequest className="w-3.5 h-3.5" /> View PR in GitHub
                          </a>
                        )}
                        {task.result_detail && (
                          <div className={`text-xs mt-1 font-mono p-2 rounded-lg bg-sand-50 border border-sand-200 ${task.status === "FAILED" ? "text-rose-700" : "text-stone-600"}`}>
                            {task.result_detail}
                          </div>
                        )}
                      </div>
                    ))}
                  </div>
                </div>
              )}

              {/* Tab 2: Live Logs */}
              {drawerTab === "logs" && <LiveLogsTab progress={progress} />}

              {/* Tab 3: Code Diff */}
              {drawerTab === "diff" && (
                <CodeDiffTab
                  progress={progress}
                  record={record}
                  jobFinished={jobFinished}
                />
              )}

              {/* Tab 4: Verification */}
              {drawerTab === "verification" && (
                <div className="p-5 rounded-2xl border border-sand-200 bg-white shadow-2xs flex flex-col gap-3">
                  <div className="flex items-center justify-between">
                    <div className="flex items-center gap-2">
                      <ShieldCheck className="w-4 h-4 text-emerald-600" />
                      <h3 className="text-sm font-bold text-stone-900">Execution Record (Verified Receipt)</h3>
                    </div>
                    {record?.recordHash && (
                      <div className="flex items-center gap-1.5 bg-sand-50 border border-sand-200 px-2 py-0.5 rounded-md text-[11px] font-mono text-stone-700">
                        <span className="text-stone-400">Hash:</span>
                        <span className="truncate max-w-[120px]" title={record.recordHash}>{record.recordHash}</span>
                        <button
                          type="button"
                          onClick={() => {
                            if (record.recordHash) {
                              navigator.clipboard?.writeText(record.recordHash);
                              setCopiedHash(true);
                              setTimeout(() => setCopiedHash(false), 2000);
                            }
                          }}
                          className="hover:text-stone-900 text-stone-400 p-0.5 transition-colors"
                          title="Copy hash"
                        >
                          {copiedHash ? <Check className="w-3 h-3 text-emerald-600" /> : <Copy className="w-3 h-3" />}
                        </button>
                      </div>
                    )}
                  </div>

                  {!jobFinished ? (
                    <p className="text-xs text-stone-500 py-1">
                      A verification record is assembled once this job finishes.
                    </p>
                  ) : recordPending ? (
                    <div className="flex items-center gap-2 text-xs text-stone-500 py-1">
                      <Loader2 className="w-3.5 h-3.5 animate-spin" /> Loading record…
                    </div>
                  ) : record ? (
                    <div className="flex flex-col gap-2.5">
                      {(() => {
                        const signed = recordBody.attestation === "signed";
                        const cells: [string, string, string?][] = [
                          ["Record hash", record.recordHash ?? "—", record.recordHash ?? undefined],
                          [
                            "Previous hash",
                            recordBody.prev_record_hash ? recordBody.prev_record_hash : "genesis — first record for this org",
                            recordBody.prev_record_hash,
                          ],
                          ["Attestation", signed ? "Signed by Kiwi" : "Unsigned", recordBody.attestation],
                          ["Signing key", recordBody.record_signature?.key ?? "—", recordBody.record_signature?.key],
                        ];
                        return (
                          <dl className="grid grid-cols-2 sm:grid-cols-4 gap-2 text-xs p-2.5 rounded-xl bg-sand-50 border border-sand-200 font-mono">
                            {cells.map(([label, value, full]) => (
                              <div key={label} className="min-w-0">
                                <dt className="text-[10px] text-stone-400 uppercase tracking-wider">{label}</dt>
                                <dd
                                  className={`truncate ${label === "Attestation" && signed ? "text-emerald-700 font-semibold" : "text-stone-700"}`}
                                  title={full ?? value}
                                >
                                  {value.length > 14 ? value.slice(0, 12) + "…" : value}
                                </dd>
                              </div>
                            ))}
                          </dl>
                        );
                      })()}

                      <p className="text-[11px] text-stone-500 leading-relaxed">
                        A tamper-evident record of what ran: the plan, the commit, the test command
                        and its outcome, linked to the previous record in your organization&apos;s
                        chain. Review the pull request in GitHub for complete diff audit.
                      </p>

                      {/* Disclosure JSON toggle */}
                      <button
                        type="button"
                        onClick={() => setShowJson((v) => !v)}
                        aria-expanded={showJson}
                        className="flex items-center gap-1.5 text-xs text-stone-500 hover:text-stone-900 transition-colors w-fit pt-0.5"
                      >
                        <ChevronDown className={`w-3.5 h-3.5 transition-transform ${showJson ? "rotate-180" : ""}`} />
                        <span>{showJson ? "Hide raw record" : "View raw record"}</span>
                      </button>

                      {showJson && (
                        <pre className="text-[11px] font-mono p-3 rounded-lg bg-stone-900 border border-stone-800 text-stone-200 overflow-x-auto max-h-64 leading-relaxed">
                          {JSON.stringify(record.data, null, 2)}
                        </pre>
                      )}
                    </div>
                  ) : (
                    <p className="text-xs text-stone-500 py-1">No verification record for this job.</p>
                  )}
                </div>
              )}
            </>
          ) : (
            <LoadingState label="Loading job details…" state="connecting" />
          )}
        </div>

        {/* ================= STICKY FOOTER ACTIONS ================= */}
        <div className="p-3.5 sm:p-4 border-t border-sand-200/90 bg-white/95 backdrop-blur-md flex items-center justify-between shrink-0 shadow-2xs">
          <div>
            {canCancel ? (
              <button
                onClick={() => act("Stopped", () => client.cancelJob(currentJob!.job_id))}
                disabled={busy !== null}
                className="px-3.5 py-1.5 rounded-xl bg-rose-50 hover:bg-rose-100 text-rose-700 border border-rose-200 text-xs font-semibold transition-all disabled:opacity-40 cursor-pointer"
              >
                Cancel Task
              </button>
            ) : (
              <span className="text-[11px] font-mono text-stone-400">
                {isSucceeded ? "Execution finished successfully" : isFailed ? "Execution terminated with errors" : "Idle"}
              </span>
            )}
          </div>

          <div className="flex items-center gap-2">
            {prResultUrl ? (
              <a
                href={prResultUrl}
                target="_blank"
                rel="noreferrer"
                className="px-4 py-2 rounded-xl bg-charcoal-900 hover:bg-charcoal-800 text-white font-semibold text-xs flex items-center gap-1.5 shadow-2xs transition-all active:scale-[0.98]"
              >
                <GitPullRequest className="w-3.5 h-3.5 text-kiwi-400" />
                <span>Review PR in GitHub &rarr;</span>
              </a>
            ) : (
              <button
                onClick={onClose}
                className="px-4 py-1.5 rounded-xl bg-sand-50 hover:bg-sand-100 text-stone-700 border border-sand-200/90 font-semibold text-xs transition-all cursor-pointer shadow-2xs"
              >
                Close
              </button>
            )}
          </div>
        </div>
      </div>
    </>
  );
}
