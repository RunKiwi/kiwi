"use client";

import { useEffect, useMemo, useState, useRef, Suspense } from "react";
import { useFleetStore } from "@/store/useFleetStore";
import { Clock, CheckCircle2, Loader2, GitPullRequest, Bot, ArrowRight, FolderGit2, AlertCircle, ChevronDown, Server, ExternalLink, Ban, RotateCcw, Trash2, Info, Search, Filter, X, Gauge, Copy } from "lucide-react";
import { TaskDrawer } from "@/components/TaskDrawer";
import { Select } from "@/components/Select";
import { useRouter, useSearchParams } from "next/navigation";
import { client, BUILTIN_MODELS, DEFAULT_PLANNER_MODEL, DEFAULT_WORKER_MODEL, providerOf, type Fleet, type ModelEntry, type GithubRepo, type UsageResponse, type Integration, type PlanRequest, type ExecutionMode } from "@/lib/api";
import Link from "next/link";
import { TaskComposer } from "@/components/TaskComposer/TaskComposer";
import { filterJobs, sortJobs, groupJobsByDate, parseStatusParam, parseSortParam, FILTERABLE_STATUSES, type JobSortOption } from "@/lib/jobFilters";
import { usePolling } from "@/hooks/usePolling";
import { parseActionableError } from "@/lib/errors";
import { sendJobCompletionNotification } from "@/lib/notifications";
import { statusOf, CARD_BASE } from "@/lib/statusColors";
import { capture } from "@/lib/analytics";
import { LoadingState } from "@/components/LoadingState";
import { shortTime, exactTime } from "@/lib/datetime";

// How many jobs render before "Show more". Sized so a normal week fits in one
// screenful of scrolling rather than to any rendering limit.
const PAGE_SIZE = 60;

// Job statuses that cannot change on their own. Kept in step with the drawer's
// task-level TERMINAL set — a cancelled job is finished, and treating it as
// live would keep the board polling forever.
const TERMINAL_JOB_STATUSES = new Set(["SUCCEEDED", "FAILED", "CANCELLED"]);

function CommandCenterContent() {
  const { jobs, loadJobs } = useFleetStore();
  const searchParams = useSearchParams();
  const router = useRouter();

  const [activeDrawerTaskId, setActiveDrawerTaskId] = useState<string | null>(searchParams.get("job") || null);
  // Which job's PR list popover is open (job_id), if any.
  const [openPrJob, setOpenPrJob] = useState<string | null>(null);

  // Job lifecycle action states
  const [confirmCancelJob, setConfirmCancelJob] = useState<string | null>(null);
  const [confirmDeleteJob, setConfirmDeleteJob] = useState<string | null>(null);
  const [cardNotice, setCardNotice] = useState<{ jobId: string; message: string; tasksAffected: number } | null>(null);
  const [cardBusyJob, setCardBusyJob] = useState<string | null>(null);

  // Filter & Sort state initialized from URL query params. The URL is user-editable
  // input, so each value is validated against what the control can actually render —
  // an unchecked cast would leave a Select displaying a value absent from its options.
  const [statusFilter, setStatusFilter] = useState(() => parseStatusParam(searchParams.get("status")));
  const [repoFilter, setRepoFilter] = useState(searchParams.get("repo") || "all");
  const [searchQuery, setSearchQuery] = useState(searchParams.get("q") || "");
  const [sortBy, setSortBy] = useState<JobSortOption>(() => parseSortParam(searchParams.get("sort")));
  const [displayLimit, setDisplayLimit] = useState(PAGE_SIZE);

  const openJobDrawer = (jobId: string) => {
    setActiveDrawerTaskId(jobId);
  };

  const closeJobDrawer = () => {
    setActiveDrawerTaskId(null);
  };

  // Synchronize the filters and the open job with the URL query string. This goes
  // through the Next router rather than window.history so the router's own view of
  // the URL stays in step — useSearchParams does not observe a raw
  // history.replaceState, which would leave route state and the address bar
  // disagreeing. Both writers share one effect precisely so they cannot clobber
  // each other's params.
  useEffect(() => {
    const params = new URLSearchParams();
    if (statusFilter && statusFilter !== "all") params.set("status", statusFilter);
    if (repoFilter && repoFilter !== "all") params.set("repo", repoFilter);
    if (searchQuery.trim()) params.set("q", searchQuery.trim());
    if (sortBy && sortBy !== "newest") params.set("sort", sortBy);
    if (activeDrawerTaskId) params.set("job", activeDrawerTaskId);

    const queryString = params.toString();
    router.replace(queryString ? `/?${queryString}` : "/", { scroll: false });
  }, [statusFilter, repoFilter, searchQuery, sortBy, activeDrawerTaskId, router]);

  // A narrowed result set should start from the top of the page, not inherit an
  // expansion the user requested for a different set of jobs. Adjusted during
  // render rather than in an effect so the first paint after a filter change is
  // already correct — the same pattern TaskDrawer uses to reset per-job state.
  const filterSignature = `${statusFilter}|${repoFilter}|${searchQuery.trim()}`;
  const [prevFilterSignature, setPrevFilterSignature] = useState(filterSignature);
  if (prevFilterSignature !== filterSignature) {
    setPrevFilterSignature(filterSignature);
    setDisplayLimit(PAGE_SIZE);
  }

  // Form State — only task + repo are required. Everything else is a hint.
  // Hand-off from onboarding. The initializers only read — clearing happens in
  // the effect below. Consuming inside an initializer would break under
  // StrictMode, which invokes it twice in development: the first pass would
  // take and delete the value, the second would find nothing, and the starter
  // task would silently vanish.
  const starterOf = (key: string) =>
    typeof window === "undefined" ? "" : localStorage.getItem(key) ?? "";
  const [task, setTask] = useState(() => starterOf("kiwi_starter_task"));
  const [repoUrl, setRepoUrl] = useState(() => starterOf("kiwi_starter_repo"));
  // Whether onboarding pre-filled this composer. Read at mount because the
  // effect below clears the hand-off keys, and submit happens long after —
  // by then there is no way left to tell a starter task from a typed one.
  const cameFromStarter = useRef(task !== "");

  useEffect(() => {
    localStorage.removeItem("kiwi_starter_task");
    localStorage.removeItem("kiwi_starter_repo");
  }, []);
  const [fleetId, setFleetId] = useState("");
  const [plannerModel, setPlannerModel] = useState(DEFAULT_PLANNER_MODEL);
  const [workerModel, setWorkerModel] = useState(DEFAULT_WORKER_MODEL);
  const [ref, setRef] = useState("main");
  const [file, setFile] = useState("");
  const [testCmd, setTestCmd] = useState("");
  const [maxWorkers, setMaxWorkers] = useState(1);
  const [mode, setMode] = useState<ExecutionMode>("file_loop");
  const [architectModel, setArchitectModel] = useState("");
  const [showAdvanced, setShowAdvanced] = useState(false);
  // Shared context off by default: it's opt-in, so a task never silently spends
  // extra tokens on prior-work retrieval unless the user turns it on.
  const [referenceMode, setReferenceMode] = useState("off");
  const [referenceJobIds, setReferenceJobIds] = useState<string[]>([]);
  const [inlineData, setInlineData] = useState<Partial<PlanRequest>>({});

  // Options loaded from the control plane.
  const [fleets, setFleets] = useState<Fleet[]>([]);
  const [customModels, setCustomModels] = useState<ModelEntry[]>([]);
  const [repos, setRepos] = useState<GithubRepo[]>([]);
  const [u, setU] = useState<UsageResponse | null>(null);
  const [integrations, setIntegrations] = useState<Integration[] | null>(null);

  const [isSubmitting, setIsSubmitting] = useState(false);
  const [submitError, setSubmitError] = useState("");
  const [submitSuccess, setSubmitSuccess] = useState<string | null>(null);

  // Two different ceilings with two different meanings. Running out of
  // agent-minutes stops work until the month rolls over or the plan changes;
  // hitting the concurrency limit only means the next task waits its turn.
  const outOfMinutes = !!u && u.agent_minutes_limit > 0 && u.agent_minutes_used >= u.agent_minutes_limit;
  const atConcurrencyLimit =
    !!u && u.max_concurrent_jobs > 0 && u.concurrent_jobs_running >= u.max_concurrent_jobs;

  // Idle means nothing can change without a user action, so the board can back
  // off. An empty board counts as idle: there is nothing to watch.
  const allJobsTerminal = useMemo(
    () => jobs.every(j => TERMINAL_JOB_STATUSES.has(j.status)),
    [jobs],
  );

  useEffect(() => {
    loadJobs();
  }, [loadJobs]);

  // Track job transitions to trigger completion notifications
  const prevJobsRef = useRef<Map<string, string>>(new Map());

  useEffect(() => {
    if (!jobs || jobs.length === 0) return;
    const prevMap = prevJobsRef.current;
    const nextMap = new Map<string, string>();

    for (const j of jobs) {
      nextMap.set(j.job_id, j.status);
      const prevStatus = prevMap.get(j.job_id);

      if (prevStatus && (prevStatus === "QUEUED" || prevStatus === "RUNNING")) {
        if (j.status === "SUCCEEDED" || j.status === "FAILED") {
          sendJobCompletionNotification(j.job_id, j.status, j.task);
        }
      }
    }

    prevJobsRef.current = nextMap;
  }, [jobs]);

  usePolling(loadJobs, {
    activeIntervalMs: 2500,
    idleIntervalMs: 15000,
    isIdle: allJobsTerminal,
  });

  useEffect(() => {
    client.listFleets().then(r => setFleets(r.fleets)).catch(() => {});
    client.listModels().then(r => setCustomModels(r.models)).catch(() => {});
    client.getUsage().then(setU).catch(() => setU(null));
    // GitHub repos are best-effort — only available once the integration is connected.
    client.listGithubRepos().then(r => setRepos(r.repos)).catch(() => {});

    // Load integrations once, then use them for two things: the first-run
    // onboarding redirect, and the M14 model default (prefer the provider the
    // org actually has a key for, so a BYOK user isn't defaulted to a model
    // they can't call). Jobs are only needed for the first-run check.
    const firstRun = typeof window !== "undefined" && !localStorage.getItem("onboarded");
    Promise.all([client.listIntegrations(), firstRun ? client.listJobs() : Promise.resolve(null)])
      .then(([ints, jbs]) => {
        setIntegrations(ints.integrations);
        const connected = (key: string) =>
          ints.integrations.some((i: Integration) => i.key === key && i.connected);
        // The defaults are Anthropic models, so when Anthropic is the one
        // provider NOT connected, fall to a provider the org can actually call.
        // Without this a BYOK user's first task fails on a key they never added.
        if (!connected("anthropic")) {
          if (connected("gemini")) {
            setPlannerModel("gemini-2.0-flash");
            setWorkerModel("gemini-flash-latest");
          } else if (connected("openai")) {
            setPlannerModel("gpt-5");
            setWorkerModel("gpt-5-mini");
          }
        }
        if (firstRun) {
          const hasInt = ints.integrations.some((i: Integration) => i.connected);
          const hasJob = (jbs?.jobs.length ?? 0) > 0;
          if (!hasInt && !hasJob) router.push("/onboarding");
          localStorage.setItem("onboarded", "1");
        }
      }).catch(() => {});
  }, [router]);

  // Show the fleet selector only once we positively know the org is not Free
  // (Free work always routes to the shared fleet, so the control is a no-op there).
  const showFleetSelector = !!u && u.plan !== "free";

  // Close the PR popover on Escape or any click outside the popover / its trigger.
  useEffect(() => {
    if (!openPrJob) return;
    const onKey = (e: KeyboardEvent) => { if (e.key === "Escape") setOpenPrJob(null); };
    const onDown = (e: MouseEvent) => {
      const t = e.target as HTMLElement;
      if (t.closest(".pr-popover") || t.closest("[data-pr-trigger]")) return;
      setOpenPrJob(null);
    };
    document.addEventListener("keydown", onKey);
    document.addEventListener("mousedown", onDown);
    return () => {
      document.removeEventListener("keydown", onKey);
      document.removeEventListener("mousedown", onDown);
    };
  }, [openPrJob]);

  // Stand a primed confirm down on Escape or any click outside the primed button.
  // This deliberately does not use onBlur: clicking a <button> does not focus it
  // in every browser, so a blur-based reset can leave a destructive action armed
  // indefinitely after the pointer has moved on.
  useEffect(() => {
    if (!confirmCancelJob && !confirmDeleteJob) return;
    const standDown = () => { setConfirmCancelJob(null); setConfirmDeleteJob(null); };
    const onKey = (e: KeyboardEvent) => { if (e.key === "Escape") standDown(); };
    const onDown = (e: MouseEvent) => {
      if ((e.target as HTMLElement).closest("[data-confirm-action]")) return;
      standDown();
    };
    document.addEventListener("keydown", onKey);
    document.addEventListener("mousedown", onDown);
    return () => {
      document.removeEventListener("keydown", onKey);
      document.removeEventListener("mousedown", onDown);
    };
  }, [confirmCancelJob, confirmDeleteJob]);

  const handleRerunWithEdits = (job: { task?: string; repo?: string; fleet_id?: string }) => {
    setTask(job.task || "");
    if (job.repo) {
      // JobSummary.repo is "owner/name", but the composer submits repo_url and
      // matches the repo chip on url. Assigning the short form straight through
      // leaves the chip showing nothing and submits an unusable repository.
      const known = repos.find(r => r.full_name === job.repo);
      setRepoUrl(known?.url ?? (job.repo.includes("://") ? job.repo : `https://github.com/${job.repo}`));
      if (known?.default_branch) setRef(known.default_branch);
    }
    if (job.fleet_id) setFleetId(job.fleet_id);

    // A re-run is a fresh attempt; carrying the previous outcome into it just
    // leaves a stale success or error sitting under the composer.
    setSubmitError("");
    setSubmitSuccess(null);

    if (typeof window !== "undefined") {
      window.scrollTo({ top: 0, behavior: "smooth" });
    }
  };

  const allModels = Array.from(new Set([...BUILTIN_MODELS, ...customModels.map(m => m.name)]));
  // The worker runs on the org's own provider key — only offer models it can
  // actually reach, so a task can't be launched with an unrunnable worker model.
  let workerOptions = allModels;
  let showIntegrationsHint = false;

  if (integrations !== null) {
    const connected = (prov: string) => integrations.some(i => i.key === prov && i.connected);
    const filteredOptions = allModels.filter(m => {
      const isCustom = customModels.find(cm => cm.name === m);
      const prov = (isCustom && isCustom.provider && isCustom.provider !== "auto") ? isCustom.provider : providerOf(m);
      return connected(prov);
    });
    if (filteredOptions.length > 0) {
      workerOptions = filteredOptions;
    } else {
      showIntegrationsHint = true;
    }
  }

  // Planning now runs on the org's own key too, so the planner is gated by the
  // same connected-provider filter as the worker. Offering a model the org
  // cannot reach would fail at submit, after the user had already committed to
  // the task.
  const plannerOptions = workerOptions;

  const handleSubmit = async () => {
    setSubmitError("");
    setSubmitSuccess(null);
    if (!task.trim() || !repoUrl.trim()) {
      capture("task_submit_failed", { reason: "missing_task_or_repo" });
      setSubmitError("A task and a repository are required.");
      return;
    }
    setIsSubmitting(true);
    try {
      const resp = await client.submitPlan({
        task,
        repo_url: repoUrl,
        ref: inlineData.ref || ref || "main",
        file: (inlineData.files && inlineData.files.length > 0) ? inlineData.files[0] : file,
        test_cmd: inlineData.test_cmd || testCmd,
        model: inlineData.model || workerModel,
        // Omitted in session mode rather than sent and ignored. The Control
        // Plane discards it there, so sending it put a value in the request
        // that no part of the run would honour.
        ...(mode === "session" ? {} : { planner_model: plannerModel }),
        max_workers: inlineData.max_workers || maxWorkers,
        fleet_id: fleetId,
        reference_mode: inlineData.reference_mode || referenceMode,
        reference_job_ids: inlineData.reference_mode === "manual" ? inlineData.reference_job_ids : (referenceMode === "manual" ? referenceJobIds : undefined),
        // Sent only when session is chosen. Omitting the key entirely on the
        // default path keeps every existing submission byte-identical to what
        // it was before this control existed.
        ...(mode === "session" ? { mode, architect_model: architectModel || undefined } : {}),
      });
      setSubmitSuccess(resp.job_id);
      // Task text, repository and branch are deliberately absent — see the
      // note at the top of lib/analytics.ts. The model ids are ours, not the
      // customer's, and which of them people actually pick is the reason this
      // event exists.
      capture("task_submitted", {
        mode,
        worker_model: inlineData.model || workerModel,
        planner_model: mode === "session" ? undefined : plannerModel,
        max_workers: inlineData.max_workers || maxWorkers,
        has_test_cmd: Boolean(inlineData.test_cmd || testCmd),
        from_starter: cameFromStarter.current,
      });
      // The launch just spent budget; refresh so the meter beside this button
      // reflects it rather than the figure from page load.
      client.getUsage().then(setU).catch(() => {});
      setTask("");
      setInlineData({});
      loadJobs();
    } catch (err) {
      capture("task_submit_failed", {
        reason: err instanceof Error ? err.message : "unknown",
      });
      setSubmitError(err instanceof Error ? err.message : "Failed to submit plan");
    } finally {
      setIsSubmitting(false);
    }
  };

  const onPickRepo = (fullName: string) => {
    const repo = repos.find(r => r.full_name === fullName);
    if (repo) {
      setRepoUrl(repo.url);
      if (repo.default_branch) setRef(repo.default_branch);
    }
  };

  const handleCardCancel = async (e: React.MouseEvent, jobId: string) => {
    e.stopPropagation();
    if (confirmCancelJob !== jobId) {
      setConfirmCancelJob(jobId);
      setConfirmDeleteJob(null);
      return;
    }
    setConfirmCancelJob(null);
    setCardBusyJob(jobId);
    setCardNotice(null);
    try {
      const res = await client.cancelJob(jobId);
      setCardNotice({ jobId, message: res.message || "Cancelled", tasksAffected: res.tasks_affected });
      await loadJobs();
    } catch (err) {
      setCardNotice({ jobId, message: err instanceof Error ? err.message : "Cancel failed", tasksAffected: 0 });
    } finally {
      setCardBusyJob(null);
    }
  };

  const handleCardRetry = async (e: React.MouseEvent, jobId: string) => {
    e.stopPropagation();
    setCardBusyJob(jobId);
    setCardNotice(null);
    try {
      const res = await client.retryJob(jobId);
      setCardNotice({ jobId, message: res.message || "Retried", tasksAffected: res.tasks_affected });
      await loadJobs();
    } catch (err) {
      setCardNotice({ jobId, message: err instanceof Error ? err.message : "Retry failed", tasksAffected: 0 });
    } finally {
      setCardBusyJob(null);
    }
  };

  const handleCardDelete = async (e: React.MouseEvent, jobId: string) => {
    e.stopPropagation();
    if (confirmDeleteJob !== jobId) {
      setConfirmDeleteJob(jobId);
      setConfirmCancelJob(null);
      return;
    }
    setConfirmDeleteJob(null);
    setCardBusyJob(jobId);
    setCardNotice(null);
    try {
      const res = await client.deleteJob(jobId);
      setCardNotice({ jobId, message: res.message || "Deleted", tasksAffected: res.tasks_affected });
      await loadJobs();
    } catch (err) {
      setCardNotice({ jobId, message: err instanceof Error ? err.message : "Delete failed", tasksAffected: 0 });
    } finally {
      setCardBusyJob(null);
    }
  };

  // Derived filter calculations
  const counts = useMemo(() => {
    const c: Record<string, number> = { all: jobs.length };
    for (const s of FILTERABLE_STATUSES) c[s] = 0;
    for (const j of jobs) {
      const s = j.status?.toUpperCase();
      if (s && s in c) {
        c[s]++;
      }
    }
    return c;
  }, [jobs]);

  const availableRepos = useMemo(() => {
    const set = new Set<string>();
    for (const j of jobs) {
      if (j.repo) set.add(j.repo);
    }
    return Array.from(set);
  }, [jobs]);

  const repoOptions = useMemo(() => {
    return [
      { value: "all", label: "All repositories" },
      ...availableRepos.map(r => ({ value: r, label: r })),
    ];
  }, [availableRepos]);

  const filteredJobs = useMemo(() => {
    return filterJobs(jobs, {
      status: statusFilter,
      repo: repoFilter,
      query: searchQuery,
    });
  }, [jobs, statusFilter, repoFilter, searchQuery]);

  const sortedJobs = useMemo(() => {
    return sortJobs(filteredJobs, sortBy);
  }, [filteredJobs, sortBy]);

  const displayedJobs = useMemo(() => {
    return sortedJobs.slice(0, displayLimit);
  }, [sortedJobs, displayLimit]);

  const groupedJobs = useMemo(() => {
    return groupJobsByDate(displayedJobs);
  }, [displayedJobs]);

  const dateGroups = [
    { label: "Today", items: groupedJobs.today },
    { label: "Yesterday", items: groupedJobs.yesterday },
    { label: "This Week", items: groupedJobs.thisWeek },
    { label: "Older", items: groupedJobs.older },
  ];

  const prLabel = (url: string) => {
    // Render a compact "owner/repo#123" from a GitHub PR URL when possible.
    const m = url.match(/github\.com\/([^/]+)\/([^/]+)\/pull\/(\d+)/);
    return m ? `${m[1]}/${m[2]}#${m[3]}` : url.replace(/^https?:\/\//, "");
  };

  // Job ids are `job_` + 16 hex; show a friendly short form (job_a3f19c…).
  const shortId = (id: string) => (id.length > 12 ? id.slice(0, 10) : id);

  const formatRepoName = (repo: string) => {
    if (!repo.includes("/")) return repo;
    const [org, name] = repo.split("/");
    return org.length > 3 ? `${org.slice(0, 3)}../${name}` : repo;
  };

  const fieldClass = "field text-sm";
  const labelClass = "block text-[10px] font-bold text-zinc-500 uppercase tracking-widest mb-2";
  // Which repo (full_name) the current repoUrl corresponds to, for the select.
  const selectedRepo = repos.find(r => r.url === repoUrl)?.full_name ?? "";

  return (
    <div className="p-8 max-w-6xl mx-auto h-full flex flex-col">
      <div className="mb-8">
        <p className="eyebrow mb-3"><span className="dot"></span> Tasks</p>
        <h1 className="text-[32px] font-semibold tracking-tight text-white mb-2">What should the swarm build?</h1>
        <p className="text-zinc-400 max-w-2xl">Describe the goal in plain English. Kiwi plans it, runs a swarm of agents, and opens one verified pull request — everything else is optional.</p>
      </div>

      {/* Composer — one compact input with an inline control rail underneath. */}
      <div className="glass-panel mb-6 flex flex-col relative z-30 overflow-visible p-4">
        <TaskComposer
          value={task}
          onChange={(p) => {
            setTask(p.task ?? "");
            setInlineData(p);
          }}
          repos={repos}
          jobs={jobs}
          models={allModels}
          repoSelected={!!repoUrl}
        />

        {/* Control rail: repo · plan · worker chips, then Launch. */}
        <div className="flex flex-wrap items-center gap-2 pt-3 mt-1 border-t border-white/5">
          {/* Repository — searchable when repos are available, else a URL input. */}
          {repos.length > 0 ? (
            <Select
              variant="chip" searchable label="Repo" ariaLabel="Repository"
              icon={<FolderGit2 className="w-3.5 h-3.5 text-zinc-400 shrink-0" />}
              value={selectedRepo} onChange={onPickRepo} placeholder="Select…"
              options={repos.map(r => ({ value: r.full_name, label: r.full_name, hint: r.private ? "private" : undefined }))}
            />
          ) : (
            <label className="chip">
              <FolderGit2 className="w-3.5 h-3.5 text-zinc-400 shrink-0" />
              <span className="k">Repo</span>
              <input type="text" value={repoUrl} onChange={e => setRepoUrl(e.target.value)} placeholder="github.com/you/repo"
                className="bg-transparent outline-none border-0 text-sm font-mono text-white placeholder:text-zinc-600 w-[190px]" />
            </label>
          )}

          {/* Planner & verifier.

              Hidden in session mode, where it decides nothing. SubmitPlan takes
              the session branch (pkg/planner/service.go) and never reads
              planner_model: SessionPlanner makes no LLM call at all, because
              the Architect does the planning inside the daemon. Worse, the
              manifest then records planner_model as the ARCHITECT model
              (service.go, "planner_model": actualModel), so leaving this chip
              lit did not merely mislead — it advertised a choice that was
              silently replaced by a different one.

              The Architect model control in Advanced is the real successor, so
              the setting is not lost, only moved to where it applies. */}
          {mode !== "session" && (
            <Select
              variant="chip" searchable label="Plan" ariaLabel="Planner & verifier model"
              icon={<span className="pdot" style={{ background: "#93C645" }} />}
              value={plannerModel} onChange={setPlannerModel}
              options={plannerOptions.map(m => ({ value: m, label: m }))}
            />
          )}

          {/* Worker */}
          <Select
            variant="chip" searchable label="Work" ariaLabel="Worker model"
            icon={<span className="pdot" style={{ background: "#E8A153" }} />}
            value={workerModel} onChange={setWorkerModel}
            options={workerOptions.map(m => ({ value: m, label: m }))}
          />

          {/* Advanced toggle */}
          <button type="button" onClick={() => setShowAdvanced(v => !v)}
            className="chip cursor-pointer text-zinc-400 hover:text-white">
            <ChevronDown className={`w-3.5 h-3.5 transition-transform ${showAdvanced ? "rotate-180" : ""}`} />
            <span className="text-xs">Advanced</span>
          </button>

          {showIntegrationsHint && (
            <Link href="/integrations" className="text-xs text-amber-500/90 hover:text-amber-400 ml-2 transition-colors">
              Connect a provider key in Integrations to run tasks.
            </Link>
          )}

          <div className="flex-1" />

          {/* Usage Meter next to Launch button */}
          {u && u.agent_minutes_limit > 0 && (() => {
            const isOverCap = u.agent_minutes_used >= u.agent_minutes_limit;
            const usagePct = Math.min(100, Math.round((u.agent_minutes_used / u.agent_minutes_limit) * 100));
            return (
              <div
                className={`flex items-center gap-2 px-2.5 py-1 rounded-xl bg-black/40 border text-xs font-mono shrink-0 ${
                  isOverCap
                    ? "border-red-500/40 text-red-300 bg-red-950/20"
                    : usagePct > 80
                    ? "border-amber-500/40 text-amber-300"
                    : "border-white/10 text-zinc-300"
                }`}
                title={`${u.agent_minutes_used.toFixed(1)} of ${u.agent_minutes_limit} agent-minutes used this month (${usagePct}%)`}
              >
                <Gauge className={`w-3.5 h-3.5 ${isOverCap ? "text-red-400" : usagePct > 80 ? "text-amber-400" : "text-green-400"}`} />
                {/* Metered as a float; unrounded it renders as 12.333333333. */}
                <span>{u.agent_minutes_used.toFixed(1)}/{u.agent_minutes_limit}m</span>
                <div className="w-10 h-1.5 rounded-full bg-white/10 overflow-hidden">
                  <div
                    className={`h-full transition-all ${isOverCap ? "bg-red-500" : usagePct > 80 ? "bg-amber-500" : "bg-green-500"}`}
                    style={{ width: `${usagePct}%` }}
                  />
                </div>
              </div>
            );
          })()}

          <button
            onClick={handleSubmit}
            disabled={isSubmitting || outOfMinutes}
            // A control that refuses to work has to say so. Without this the
            // button just goes dim and the reason lives only in a colour.
            title={outOfMinutes ? "Out of agent-minutes for this month" : undefined}
            className="btn-primary px-5 py-2 shrink-0 disabled:opacity-40 disabled:cursor-not-allowed"
          >
            {isSubmitting ? <><Loader2 className="w-4 h-4 animate-spin" /> Launching…</> : <>Launch <ArrowRight className="w-4 h-4" /></>}
          </button>
        </div>

        {/* Advanced options — hidden by default to keep the composer compact. */}
        {showAdvanced && (
          <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4 pt-4 mt-3 border-t border-white/5">
            {showFleetSelector && (
              <div>
                <label className={labelClass}>Fleet</label>
                <Select
                  ariaLabel="Fleet" value={fleetId} onChange={setFleetId}
                  options={[{ value: "", label: "Any available fleet" }, ...fleets.map(f => ({ value: f.id, label: f.name, hint: f.type === "byoc" ? "BYOC" : "Managed" }))]}
                />
              </div>
            )}
            {repos.length > 0 && (
              <div>
                <label className={labelClass}>Repository URL <span className="text-zinc-600 normal-case font-normal">(override)</span></label>
                <input type="text" value={repoUrl} onChange={e => setRepoUrl(e.target.value)} placeholder="…or paste a URL" className={fieldClass} />
              </div>
            )}
            <div>
              <label className={labelClass}>Git ref {inlineData.ref && <span className="text-green-500 normal-case font-normal ml-1">(set inline ✓)</span>}</label>
              <input type="text" value={inlineData.ref || ref} onChange={e => setRef(e.target.value)} disabled={!!inlineData.ref} placeholder="main" className={`${fieldClass} ${inlineData.ref ? 'opacity-50 cursor-not-allowed' : ''}`} />
            </div>
            <div>
              <label className={labelClass}>Target file <span className="text-zinc-600 normal-case font-normal">(optional)</span> {(inlineData.files && inlineData.files.length > 0) && <span className="text-green-500 normal-case font-normal ml-1">(set inline ✓)</span>}</label>
              <input type="text" value={(inlineData.files && inlineData.files.length > 0) ? inlineData.files[0] : file} onChange={e => setFile(e.target.value)} disabled={!!(inlineData.files && inlineData.files.length > 0)} placeholder="let the agent decide" className={`${fieldClass} ${(inlineData.files && inlineData.files.length > 0) ? 'opacity-50 cursor-not-allowed' : ''}`} />
            </div>
            <div>
              <label className={labelClass}>Test command <span className="text-zinc-600 normal-case font-normal">(optional)</span> {inlineData.test_cmd && <span className="text-green-500 normal-case font-normal ml-1">(set inline ✓)</span>}</label>
              <input type="text" value={inlineData.test_cmd || testCmd} onChange={e => setTestCmd(e.target.value)} disabled={!!inlineData.test_cmd} placeholder="e.g. go test ./..." className={`${fieldClass} ${inlineData.test_cmd ? 'opacity-50 cursor-not-allowed' : ''}`} />
            </div>
            <div>
              <label className={labelClass}>Max workers {inlineData.max_workers && <span className="text-green-500 normal-case font-normal ml-1">(set inline ✓)</span>}</label>
              <input type="number" min="1" max="10" value={inlineData.max_workers || maxWorkers} onChange={e => setMaxWorkers(parseInt(e.target.value) || 1)} disabled={!!inlineData.max_workers} className={`${fieldClass} ${inlineData.max_workers ? 'opacity-50 cursor-not-allowed' : ''}`} />
            </div>
            <div>
              <label className={labelClass}>Execution loop</label>
              <Select
                ariaLabel="Execution loop" value={mode} onChange={v => setMode(v as ExecutionMode)}
                options={[
                  { value: "file_loop", label: "Standard", hint: "edit + verify" },
                  { value: "session", label: "Session", hint: "plan, work, review" },
                ]}
              />
            </div>
            {mode === "session" && (
              <div>
                <label className={labelClass}>Architect model <span className="text-zinc-600 normal-case font-normal">(plans &amp; reviews)</span></label>
                <Select
                  ariaLabel="Architect model" searchable value={architectModel} onChange={setArchitectModel}
                  options={[{ value: "", label: `Same as worker (${workerModel})` }, ...workerOptions.map(m => ({ value: m, label: m }))]}
                />
              </div>
            )}
            <div>
              <label className={labelClass}>Shared context {inlineData.reference_mode && <span className="text-green-500 normal-case font-normal ml-1">(set inline ✓)</span>}</label>
              <button
                type="button" role="switch" aria-checked={(inlineData.reference_mode || referenceMode) !== "off"}
                aria-label="Use context from past jobs"
                onClick={() => !inlineData.reference_mode && setReferenceMode(referenceMode === "off" ? "auto" : "off")}
                disabled={!!inlineData.reference_mode}
                className={`flex items-center gap-3 w-full h-[42px] px-3 rounded-lg border transition-colors ${(inlineData.reference_mode || referenceMode) !== "off" ? "border-[#93C645]/40 bg-[#93C645]/10" : "border-white/10 bg-black/20"} ${inlineData.reference_mode ? 'opacity-50 cursor-not-allowed' : ''}`}
              >
                <span className={`relative inline-flex h-5 w-9 shrink-0 rounded-full transition-colors ${referenceMode !== "off" ? "bg-[#93C645]" : "bg-white/15"}`}>
                  <span className={`absolute top-0.5 h-4 w-4 rounded-full bg-white transition-all ${referenceMode !== "off" ? "left-4" : "left-0.5"}`} />
                </span>
                <span className="text-sm text-zinc-300">{referenceMode !== "off" ? "Using past jobs" : "Off"}</span>
              </button>
            </div>
            {(inlineData.reference_mode || referenceMode) !== "off" && (
              <div>
                <label className={labelClass}>Context source</label>
                <Select
                  ariaLabel="Context source" value={inlineData.reference_mode || referenceMode} onChange={setReferenceMode}
                  options={[{ value: "auto", label: "Auto — related past jobs" }, { value: "manual", label: "Manual — pick jobs" }]}
                  className={inlineData.reference_mode ? 'opacity-50 cursor-not-allowed pointer-events-none' : ''}
                />
                {(inlineData.reference_mode || referenceMode) === "auto" && (
                  <p className="text-xs text-amber-400/80 mt-1.5">Auto-selects related past jobs — may use extra tokens.</p>
                )}
              </div>
            )}
            {referenceMode === "manual" && !inlineData.reference_mode && (
              <div className="md:col-span-2 lg:col-span-3">
                <label className={labelClass}>Reference Jobs</label>
                <div className="flex flex-col gap-2 max-h-48 overflow-y-auto p-2 border border-white/5 rounded-lg bg-black/20">
                  {jobs.map(j => (
                    <label key={j.job_id} className="flex items-start gap-3 cursor-pointer p-2 hover:bg-white/5 rounded-md">
                      <input 
                        type="checkbox" 
                        checked={referenceJobIds.includes(j.job_id)}
                        onChange={(e) => {
                          if (e.target.checked) setReferenceJobIds([...referenceJobIds, j.job_id]);
                          else setReferenceJobIds(referenceJobIds.filter(id => id !== j.job_id));
                        }}
                        className="mt-1 accent-[#93C645]"
                      />
                      <div className="flex flex-col min-w-0">
                        <span className="font-mono text-xs text-zinc-400">{shortId(j.job_id)}</span>
                        <span className="text-sm text-zinc-200 line-clamp-1 truncate">{j.task}</span>
                      </div>
                    </label>
                  ))}
                  {jobs.length === 0 && <div className="text-zinc-500 text-sm p-2">No past jobs available.</div>}
                </div>
              </div>
            )}
          </div>
        )}

        {/* Why Launch will not do what you expect, stated before you press it. */}
        {(outOfMinutes || atConcurrencyLimit) && (
          <div className="pt-3 mt-1">
            {outOfMinutes ? (
              <div className="flex items-center gap-2 text-sm text-red-400">
                <AlertCircle className="w-4 h-4 shrink-0" />
                <span>
                  Out of agent-minutes for this month, so new tasks will not start.
                  <Link href="/settings#plan" className="underline ml-1.5 font-semibold text-red-300 hover:text-white transition-colors">
                    Review plan and usage →
                  </Link>
                </span>
              </div>
            ) : (
              <div className="flex items-center gap-2 text-sm text-amber-400/90">
                <Clock className="w-4 h-4 shrink-0" />
                <span>
                  {u?.concurrent_jobs_running} of {u?.max_concurrent_jobs} concurrent
                  {" "}job{u?.max_concurrent_jobs === 1 ? "" : "s"} running — this task will queue until one finishes.
                </span>
              </div>
            )}
          </div>
        )}

        {/* Status line */}
        {(submitError || submitSuccess) && (
          <div className="pt-3 mt-1">
            {submitError && (() => {
              // Plan matters: a Free org never goes through paid activation, so
              // it must never be told to activate.
              const err = parseActionableError(submitError, { plan: u?.plan });
              return (
                <div className="flex items-center gap-2 text-red-400 text-sm">
                  <AlertCircle className="w-4 h-4 shrink-0" />
                  <span>
                    {err.message}
                    {err.actionHref && err.actionLabel && (
                      <Link href={err.actionHref} className="underline ml-1.5 font-semibold text-red-300 hover:text-white transition-colors">
                        {err.actionLabel} →
                      </Link>
                    )}
                  </span>
                </div>
              );
            })()}
            {submitSuccess && (
              <div className="flex items-center gap-2 text-green-400 text-sm">
                <CheckCircle2 className="w-4 h-4 shrink-0" />
                Launched — <button className="underline" onClick={() => setActiveDrawerTaskId(submitSuccess)}>{shortId(submitSuccess)}</button>
              </div>
            )}
          </div>
        )}
      </div>

      {/* Filter Bar */}
      {jobs.length > 0 && (
        <div className="mb-6 flex flex-wrap items-center justify-between gap-3 p-3 rounded-2xl bg-white/[0.03] border border-white/5 relative z-20">
          {/* Status filter chips */}
          <div className="flex flex-wrap items-center gap-1.5">
            {[
              { key: "all", label: "All", count: counts.all },
              ...FILTERABLE_STATUSES.map(s => ({
                key: s,
                // "QUEUED" -> "Queued": the chips read as words, not enum values.
                label: s.charAt(0) + s.slice(1).toLowerCase(),
                count: counts[s],
              })),
            ].map(chip => {
              const isActive = statusFilter.toLowerCase() === chip.key.toLowerCase();
              return (
                <button
                  key={chip.key}
                  type="button"
                  onClick={() => setStatusFilter(chip.key)}
                  className={`px-3 py-1.5 rounded-xl text-xs font-semibold transition-colors flex items-center gap-1.5 ${
                    isActive
                      ? "bg-white/15 text-white border border-white/20 shadow-sm"
                      : "text-zinc-400 hover:text-white hover:bg-white/5 border border-transparent"
                  }`}
                >
                  <span>{chip.label}</span>
                  <span className={`px-1.5 py-0.2 rounded-full text-[10px] font-mono ${isActive ? "bg-white/20 text-white" : "bg-white/5 text-zinc-500"}`}>
                    {chip.count}
                  </span>
                </button>
              );
            })}
          </div>

          {/* Search, Repo, and Sort controls */}
          <div className="flex flex-wrap items-center gap-2">
            {/* Search Input */}
            <div className="relative">
              <Search className="w-3.5 h-3.5 text-zinc-500 absolute left-3 top-1/2 -translate-y-1/2" />
              <input
                type="text"
                value={searchQuery}
                onChange={e => setSearchQuery(e.target.value)}
                placeholder="Filter jobs…"
                className="field text-xs pl-8 pr-7 py-1.5 h-8 w-44 md:w-56"
              />
              {searchQuery && (
                <button
                  type="button"
                  onClick={() => setSearchQuery("")}
                  className="absolute right-2 top-1/2 -translate-y-1/2 text-zinc-500 hover:text-zinc-300"
                >
                  <X className="w-3.5 h-3.5" />
                </button>
              )}
            </div>

            {/* Repo Select */}
            {availableRepos.length > 0 && (
              <Select
                variant="chip"
                searchable
                label="Repo"
                ariaLabel="Filter by repo"
                value={repoFilter}
                onChange={setRepoFilter}
                options={repoOptions}
              />
            )}

            {/* Sort Select */}
            <Select
              variant="chip"
              label="Sort"
              ariaLabel="Sort jobs"
              value={sortBy}
              onChange={(v) => setSortBy(v as JobSortOption)}
              options={[
                { value: "newest", label: "Newest first" },
                { value: "oldest", label: "Oldest first" },
                { value: "status", label: "By status" },
              ]}
            />
          </div>
        </div>
      )}

      {/* Grid of Jobs */}
      {jobs.length === 0 ? (
        <div className="flex flex-col items-center justify-center text-center py-20 text-zinc-500">
          <Server className="w-10 h-10 mb-3 text-zinc-700" />
          No jobs yet — describe a goal above and launch your first one.
        </div>
      ) : sortedJobs.length === 0 ? (
        <div className="flex flex-col items-center justify-center text-center py-16 text-zinc-400 bg-white/[0.02] border border-white/5 rounded-2xl">
          <Filter className="w-8 h-8 mb-3 text-zinc-600" />
          <p className="text-sm font-medium text-zinc-300">No jobs match these filters</p>
          <p className="text-xs text-zinc-500 mt-1 max-w-sm">Try searching for a different term or clearing active status and repository filters.</p>
          <button
            type="button"
            onClick={() => {
              setStatusFilter("all");
              setRepoFilter("all");
              setSearchQuery("");
              setSortBy("newest");
            }}
            className="btn-ghost mt-4"
          >
            Clear filters
          </button>
        </div>
      ) : (
        <div className="flex flex-col gap-8 pb-32 relative z-10">
          {dateGroups.map(group => {
            if (group.items.length === 0) return null;
            return (
              <div key={group.label} className="flex flex-col">
                <div className="sticky top-0 z-20 py-2 bg-[var(--background)]/90 backdrop-blur-md mb-3 flex items-center gap-2 border-b border-white/5">
                  <p className="eyebrow"><span className="dot"></span> {group.label}</p>
                  <span className="text-[10px] font-mono text-zinc-500 bg-white/5 rounded-full px-2 py-0.5">{group.items.length}</span>
                </div>
                <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4 gap-4">
                  {group.items.map(job => {
                    const m = statusOf(job.status);
                    const Icon = m.Icon;
                    return (
                      <div
                        key={job.job_id}
                        style={{
                          background: `linear-gradient(0deg, ${m.wash}, ${m.wash}), ${CARD_BASE}`,
                          borderColor: m.border,
                          boxShadow: `0 4px 30px rgba(0,0,0,0.5), 0 0 15px -2px ${m.glow}`,
                        }}
                        className="group relative text-left rounded-2xl p-4 border flex flex-col h-full card-hover"
                      >
                        {/* Stretched click target overlay (valid HTML5 semantics, no button nesting) */}
                        <button
                          type="button"
                          aria-label={`View details for job ${shortId(job.job_id)}`}
                          onClick={() => openJobDrawer(job.job_id)}
                          className="absolute inset-0 z-0 rounded-2xl cursor-pointer focus:outline-none focus-visible:ring-2 focus-visible:ring-[color:var(--green)]/60"
                        />

                        {/* Card inner content sitting above click overlay */}
                        <div className="relative z-10 pointer-events-none flex flex-col h-full">
                          <div className="flex items-center justify-between gap-2 mb-3">
                            <span title={job.job_id} className="font-mono text-xs text-zinc-500 truncate min-w-0 group-hover:text-zinc-300 transition-colors">
                              {shortId(job.job_id)}
                            </span>
                            <div className="flex items-center gap-1.5 shrink-0">
                              <div
                                className="group/status inline-flex items-center gap-1.5 rounded-full border px-1.5 py-0.5 hover:px-2 text-[10px] font-bold uppercase tracking-wider shrink-0 transition-all"
                                style={{ color: m.color, borderColor: m.border, background: m.wash }}
                              >
                                <Icon className={`w-3 h-3 shrink-0 ${m.spin ? "animate-spin" : ""}`} />
                                <span className="hidden group-hover/status:inline">{m.label}</span>
                              </div>
                              <div className="flex items-center gap-1 pointer-events-auto" onClick={e => e.stopPropagation()}>
                                {cardBusyJob === job.job_id ? (
                                  <Loader2 className="w-3.5 h-3.5 animate-spin text-zinc-400" />
                                ) : (
                                  <>
                                    {(job.status === "QUEUED" || job.status === "RUNNING") && (
                                      <button
                                        type="button"
                                        onClick={e => handleCardCancel(e, job.job_id)}
                                        data-confirm-action
                                        title="Cancel job"
                                        className={`group/btn flex items-center gap-1 px-1.5 py-0.5 hover:px-2 rounded text-[10px] font-medium border transition-all ${
                                          confirmCancelJob === job.job_id
                                            ? "border-amber-500/50 bg-amber-500/20 text-amber-300"
                                            : "border-white/10 text-zinc-400 hover:text-amber-300 hover:border-amber-500/30 hover:bg-amber-500/10"
                                        }`}
                                      >
                                        <Ban className="w-3 h-3 shrink-0" />
                                        <span className="hidden group-hover/btn:inline whitespace-nowrap">{confirmCancelJob === job.job_id ? "Confirm cancel?" : "Cancel"}</span>
                                      </button>
                                    )}
                                    {/* Retry requeues failed AND cancelled tasks (see store.RetryJob),
                                        so a job you called off is resumable straight from the card
                                        rather than only from the drawer. */}
                                    {(job.status === "FAILED" || job.status === "CANCELLED") && (
                                      <>
                                        <button
                                          type="button"
                                          onClick={e => handleCardRetry(e, job.job_id)}
                                          title="Retry job"
                                          className="group/btn flex items-center gap-1 px-1.5 py-0.5 hover:px-2 rounded text-[10px] font-medium border border-white/10 text-zinc-400 hover:text-blue-300 hover:border-blue-500/30 hover:bg-blue-500/10 transition-all"
                                        >
                                          <RotateCcw className="w-3 h-3 shrink-0" />
                                          <span className="hidden group-hover/btn:inline whitespace-nowrap">Retry</span>
                                        </button>
                                        <button
                                          type="button"
                                          onClick={e => {
                                            e.stopPropagation();
                                            handleRerunWithEdits(job);
                                          }}
                                          title="Re-run with edits"
                                          className="group/btn flex items-center gap-1 px-1.5 py-0.5 hover:px-2 rounded text-[10px] font-medium border border-white/10 text-zinc-400 hover:text-[#93C645] hover:border-[#93C645]/30 hover:bg-[#93C645]/10 transition-all"
                                        >
                                          <Copy className="w-3 h-3 shrink-0" />
                                          <span className="hidden group-hover/btn:inline whitespace-nowrap">Re-run</span>
                                        </button>
                                      </>
                                    )}
                                    {(job.status === "SUCCEEDED" || job.status === "FAILED" || job.status === "CANCELLED") && (
                                      <button
                                        type="button"
                                        onClick={e => handleCardDelete(e, job.job_id)}
                                        data-confirm-action
                                        title="Delete job"
                                        className={`group/btn flex items-center gap-1 px-1.5 py-0.5 hover:px-2 rounded text-[10px] font-medium border transition-all ${
                                          confirmDeleteJob === job.job_id
                                            ? "border-red-500/50 bg-red-500/20 text-red-300"
                                            : "border-white/10 text-zinc-400 hover:text-red-300 hover:border-red-500/30 hover:bg-red-500/10"
                                        }`}
                                      >
                                        <Trash2 className="w-3 h-3 shrink-0" />
                                        <span className="hidden group-hover/btn:inline whitespace-nowrap">{confirmDeleteJob === job.job_id ? "Confirm delete?" : "Delete"}</span>
                                      </button>
                                    )}
                                  </>
                                )}
                              </div>
                            </div>
                          </div>

                          <h3 className="text-sm font-medium text-white mb-2 line-clamp-2 leading-snug">
                            {job.task?.trim() || `Job ${shortId(job.job_id)}`}
                          </h3>

                          {cardNotice?.jobId === job.job_id && (
                            <div
                              role="status"
                              aria-live="polite"
                              className={`mb-3 p-2 rounded-lg text-xs flex items-start gap-1.5 border pointer-events-auto ${
                                cardNotice.tasksAffected === 0
                                  ? "bg-amber-500/10 border-amber-500/20 text-amber-300"
                                  : "bg-green-500/10 border-green-500/20 text-green-300"
                              }`}
                              onClick={e => e.stopPropagation()}
                            >
                              {cardNotice.tasksAffected === 0 ? (
                                <Info className="w-3.5 h-3.5 shrink-0 mt-0.5" />
                              ) : (
                                <CheckCircle2 className="w-3.5 h-3.5 shrink-0 mt-0.5" />
                              )}
                              <span className="leading-tight">{cardNotice.message}</span>
                            </div>
                          )}

                          <div className="pt-3 border-t border-white/5 mt-auto flex items-center justify-between gap-2 text-xs text-zinc-400">
                            {/* Left: compact PR count popover or repo or time */}
                            <div className="min-w-0 relative pointer-events-auto">
                              {job.pr_urls && job.pr_urls.length > 0 ? (
                                <>
                                  <button
                                    data-pr-trigger
                                    onClick={(e) => { e.stopPropagation(); setOpenPrJob(openPrJob === job.job_id ? null : job.job_id); }}
                                    className="flex items-center gap-1.5 rounded-full border border-green-500/25 bg-green-500/10 pl-2 pr-2.5 py-1 text-green-300 hover:text-green-200 hover:border-green-500/40 hover:bg-green-500/15 transition-colors"
                                    title={`${job.pr_urls.length} pull request${job.pr_urls.length > 1 ? "s" : ""}`}
                                    aria-expanded={openPrJob === job.job_id}
                                  >
                                    <GitPullRequest className="w-3.5 h-3.5 shrink-0" />
                                    <span className="font-mono text-[11px] font-semibold">{job.pr_urls.length}</span>
                                    <span className="text-[11px]">PR{job.pr_urls.length > 1 ? "s" : ""}</span>
                                  </button>
                                  {openPrJob === job.job_id && (
                                    <div
                                      onClick={(e) => e.stopPropagation()}
                                      className="pr-popover absolute bottom-full left-0 mb-2 z-50 w-72 rounded-xl border border-white/10 bg-[#0E1A24]/95 backdrop-blur-xl shadow-[0_24px_60px_-16px_rgba(0,0,0,0.85)] p-1.5"
                                    >
                                      <div className="flex items-center gap-2 px-2 py-1.5 mb-1 border-b border-white/5">
                                        <GitPullRequest className="w-3.5 h-3.5 text-green-400 shrink-0" />
                                        <span className="text-[11px] font-semibold uppercase tracking-wider text-zinc-300">Pull requests</span>
                                        <span className="ml-auto font-mono text-[10px] text-zinc-400 bg-white/5 rounded-full px-1.5 py-0.5">{job.pr_urls.length}</span>
                                      </div>
                                      <div className="flex flex-col gap-0.5 max-h-56 overflow-y-auto">
                                        {job.pr_urls.map((url) => (
                                          <a key={url} href={url} target="_blank" rel="noreferrer"
                                            onClick={() => setOpenPrJob(null)}
                                            className="group/pr flex items-center gap-2.5 px-2 py-1.5 rounded-lg text-xs text-zinc-200 hover:bg-white/[0.06] transition-colors">
                                            <span className="w-6 h-6 rounded-md bg-green-500/10 border border-green-500/20 flex items-center justify-center shrink-0">
                                              <GitPullRequest className="w-3.5 h-3.5 text-green-400" />
                                            </span>
                                            <span className="font-mono truncate flex-1">{prLabel(url)}</span>
                                            <ExternalLink className="w-3.5 h-3.5 text-zinc-500 group-hover/pr:text-zinc-300 transition-colors shrink-0" />
                                          </a>
                                        ))}
                                      </div>
                                    </div>
                                  )}
                                </>
                              ) : job.repo ? (
                                <span className="flex items-center gap-1.5 font-mono text-zinc-400 truncate" title={job.repo}>
                                  <FolderGit2 className="w-3.5 h-3.5 text-zinc-500 shrink-0" />{formatRepoName(job.repo)}
                                </span>
                              ) : null}
                              {/* Always rendered, not just as a repo-less fallback. It used
                                  to be the `else` branch, so a job with a repo — the normal
                                  case — showed no time at all, and one without showed a bare
                                  clock that said nothing about which day. */}
                              <span
                                className="flex items-center gap-1.5 text-zinc-500 shrink-0"
                                title={exactTime(job.created_at)}
                              >
                                <Clock className="w-3 h-3 shrink-0" />{shortTime(job.created_at)}
                              </span>
                            </div>
                            {/* Right: task count */}
                            <div className="flex items-center gap-1.5 shrink-0" title={`${job.task_count} task${job.task_count !== 1 ? "s" : ""}`}>
                              <Bot className="w-3.5 h-3.5 text-zinc-500" />
                              <span>{job.task_count} task{job.task_count !== 1 ? "s" : ""}</span>
                            </div>
                          </div>
                        </div>
                      </div>
                    );
                  })}
                </div>
              </div>
            );
          })}

          {sortedJobs.length > displayLimit && (
            <div className="flex justify-center pt-4">
              <button
                type="button"
                onClick={() => setDisplayLimit(l => l + 60)}
                className="px-6 py-2.5 rounded-xl border border-white/10 bg-white/5 hover:bg-white/10 text-xs font-semibold text-zinc-300 transition-colors"
              >
                Show more ({sortedJobs.length - displayLimit} remaining)
              </button>
            </div>
          )}
        </div>
      )}

      <TaskDrawer taskId={activeDrawerTaskId} onClose={closeJobDrawer} onRerunWithEdits={handleRerunWithEdits} />
    </div>
  );
}

export default function CommandCenter() {
  return (
    <Suspense fallback={<LoadingState label="Loading command center…" className="min-h-[70vh]" />}>
      <CommandCenterContent />
    </Suspense>
  );
}
