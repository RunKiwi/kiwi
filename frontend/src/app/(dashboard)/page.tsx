"use client";

import React, { useEffect, useState, useMemo, Suspense } from "react";
import { useSearchParams } from "next/navigation";
import { FaSlack } from "react-icons/fa6";
import {
  FolderGit2,
  GitPullRequest,
  CheckCircle2,
  Server,
  Plus,
  Play,
  Compass,
  Hammer,
  Ban,
  ChevronRight,
  AlertCircle,
  AlertTriangle,
  RotateCcw,
  Loader2,
} from "lucide-react";
import { api, DEFAULT_ARCHITECT_MODEL, DEFAULT_WORKER_MODEL, type UsageResponse, type GithubRepo, type SpendResponse, type SandboxCacheStats, type JobSummary } from "@/lib/api";
import { shortTime, formatCost, formatTokens } from "@/lib/datetime";
import { TaskDrawer } from "@/components/TaskDrawer";
import { ModelSelector } from "@/components/TaskComposer/ModelSelector";
import { ThinkingOrb } from "@/components/ThinkingOrb";
import { UpgradeButton } from "@/components/UpgradeButton";
import { useFleetStore } from "@/store/useFleetStore";
import { Logo } from "@/components/Logo";

import { usePolling } from "@/hooks/usePolling";

function SegmentedMeter({
  totalTicks = 36,
  activeTicks = 18,
  activeColorClass = "meter-tick-active-emerald",
}: {
  totalTicks?: number;
  activeTicks?: number;
  activeColorClass?: string;
}) {
  return (
    <div className="flex items-center gap-1 py-1 overflow-x-auto">
      {Array.from({ length: totalTicks }).map((_, i) => {
        const isActive = i < activeTicks;
        return (
          <div
            key={i}
            className={`meter-tick ${
              isActive ? activeColorClass : "meter-tick-inactive"
            }`}
          />
        );
      })}
    </div>
  );
}

function CommandCenterContent() {
  const searchParams = useSearchParams();
  const { jobs, daemons, loadJobs, loadDaemons } = useFleetStore();

  const [activeDrawerTaskId, setActiveDrawerTaskId] = useState<string | null>(searchParams.get("job") || null);
  const [usage, setUsage] = useState<UsageResponse | null>(null);
  const [repos, setRepos] = useState<GithubRepo[]>([]);
  const [spend, setSpend] = useState<SpendResponse | null>(null);
  const [cacheStats, setCacheStats] = useState<SandboxCacheStats | null>(null);
  const [loading, setLoading] = useState(true);

  // Composer drawer/modal state
  const [showComposer, setShowComposer] = useState(searchParams.get("compose") === "true");
  const [taskPrompt, setTaskPrompt] = useState("");
  const [repoUrl, setRepoUrl] = useState("");
  const [testCmd, setTestCmd] = useState("");
  const [architectModel, setArchitectModel] = useState(DEFAULT_ARCHITECT_MODEL);
  const [workerModel, setWorkerModel] = useState(DEFAULT_WORKER_MODEL);
  // Quick-compose keeps the safe defaults; the full controls (plan mode, spend
  // cap, dry-run) live on the /composer page for anyone who wants to change them.
  const spendCap = 0.5;
  const planMode = false;
  const dryRun = false;
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [submitError, setSubmitError] = useState<string | null>(null);

  // Status Filter
  const [statusFilter, setStatusFilter] = useState(searchParams.get("filter") || "all");

  const effectiveJobs = useMemo(() => jobs || [], [jobs]);

  // Initial load for static metadata like GitHub repos
  useEffect(() => {
    loadJobs().catch(() => {}).finally(() => setLoading(false));
    loadDaemons().catch(() => {});

    api.getUsage().then(setUsage).catch(() => {});
    api.listGithubRepos()
      .then((r) => {
        const list = r.repos || [];
        setRepos(list);
        if (list.length > 0) {
          setRepoUrl((prev) => prev || list[0].url || `https://github.com/${list[0].full_name || list[0].name}`);
        }
      })
      .catch(() => {});
    api.getSpend().then(setSpend).catch(() => {});
    api.getSandboxCacheStats().then(setCacheStats).catch(() => {});
  }, [loadJobs, loadDaemons]);

  // Check if any job is currently queued or running
  const hasActiveJobs = useMemo(() => {
    return (jobs || []).some(
      (j) => j.status === "QUEUED" || j.status === "LEASED" || j.status === "RUNNING"
    );
  }, [jobs]);

  // Dynamic continuous polling for live dashboard updates
  usePolling(
    async () => {
      await Promise.all([
        loadJobs().catch(() => {}),
        loadDaemons().catch(() => {}),
        api.getUsage().then(setUsage).catch(() => {}),
        api.getSpend().then(setSpend).catch(() => {}),
        api.getSandboxCacheStats().then(setCacheStats).catch(() => {}),
      ]);
    },
    {
      activeIntervalMs: 2500,
      idleIntervalMs: 10000,
      isIdle: !hasActiveJobs,
    }
  );

  const handleLaunch = async (e?: React.FormEvent) => {
    if (e) e.preventDefault();
    if (!taskPrompt.trim()) return;

    setIsSubmitting(true);
    setSubmitError(null);

    const selectedRepo = repos.find(
      (r) =>
        r.url === repoUrl ||
        (r.full_name && r.full_name === repoUrl) ||
        (r.name && r.name === repoUrl)
    );
    const targetUrl =
      selectedRepo?.url ||
      (repoUrl.includes("://") || repoUrl.includes("@")
        ? repoUrl
        : `https://github.com/${repoUrl}`);

    try {
      await api.submitPlan({
        task: taskPrompt.trim(),
        repo_url: targetUrl,
        test_cmd: testCmd.trim() || undefined,
        architect_model: architectModel,
        model: workerModel,
        plan_mode: planMode,
        spend_cap_usd: spendCap,
        dry_run: dryRun,
      });
      setTaskPrompt("");
      setShowComposer(false);
      await loadJobs();
    } catch (err) {
      setSubmitError(err instanceof Error ? err.message : "Failed to submit task");
    } finally {
      setIsSubmitting(false);
    }
  };

  const statusCounts = useMemo(() => {
    let running = 0;
    let planReview = 0;
    let waiting = 0;
    let prReady = 0;
    let failed = 0;
    // Slack is an origin, not a status — a job can be Running and from Slack
    // at once — so it's counted independently rather than joining the
    // mutually-exclusive else-if chain below.
    let slack = 0;

    for (const job of effectiveJobs) {
      const isFailed = job.status === "FAILED" || job.plan_status === "rejected";
      const isCancelled = job.status === "CANCELLED";
      const isPrReady = job.status === "SUCCEEDED" || (job.pr_urls && job.pr_urls.length > 0);
      const isPlanReview = !isFailed && !isCancelled && !isPrReady && (job.status === "PLAN_REVIEW" || job.status === "AWAITING_PLAN_APPROVAL" || job.plan_status === "pending_review");
      const isWaitingUser = job.status === "WAITING_USER" && !isFailed && !isCancelled && !isPrReady;
      const isRunning = (job.status === "LEASED" || job.status === "RUNNING") && !isPlanReview && !isFailed && !isCancelled && !isPrReady;

      if (isFailed) failed++;
      else if (isPlanReview) planReview++;
      else if (isWaitingUser) waiting++;
      else if (isRunning) running++;
      else if (isPrReady) prReady++;

      if (job.latest_origin === "slack") slack++;
    }

    return {
      all: effectiveJobs.length,
      running,
      planReview,
      waiting,
      prReady,
      failed,
      slack,
    };
  }, [effectiveJobs]);

  const filteredJobs = useMemo(() => {
    const list = effectiveJobs;
    return list.filter((job) => {
      const isFailed = job.status === "FAILED" || job.plan_status === "rejected";
      const isCancelled = job.status === "CANCELLED";
      const isPrReady = job.status === "SUCCEEDED" || (job.pr_urls && job.pr_urls.length > 0);
      const isPlanReview = !isFailed && !isCancelled && !isPrReady && (job.status === "PLAN_REVIEW" || job.status === "AWAITING_PLAN_APPROVAL" || job.plan_status === "pending_review");
      const isWaitingUser = job.status === "WAITING_USER" && !isFailed && !isCancelled && !isPrReady;
      const isRunning = (job.status === "LEASED" || job.status === "RUNNING") && !isPlanReview && !isFailed && !isCancelled && !isPrReady;

      if (statusFilter === "running") return isRunning;
      if (statusFilter === "plan") return isPlanReview;
      if (statusFilter === "waiting") return isWaitingUser;
      if (statusFilter === "pr_created" || statusFilter === "succeeded") return isPrReady;
      if (statusFilter === "failed") return isFailed;
      if (statusFilter === "slack") return job.latest_origin === "slack";
      return true;
    });
  }, [effectiveJobs, statusFilter]);

  const plan = usage?.plan || "free";
  const usedMinutes = usage?.agent_minutes_used ?? 0;
  const limitMinutes = usage?.agent_minutes_limit && usage.agent_minutes_limit > 0
    ? usage.agent_minutes_limit
    : (plan === "pro" || plan === "individual" ? 2000 : plan === "team" ? 5000 : 500);
  const percentUsed = limitMinutes > 0 ? Math.min(100, Math.round((usedMinutes / limitMinutes) * 100)) : 0;

  const activeWorkers = usage?.concurrent_jobs_running ?? (jobs || []).filter((j) => j.status === "LEASED" || j.status === "RUNNING").length;
  const maxWorkers = usage?.max_concurrent_jobs || usage?.concurrent_jobs_limit || (usage?.plan === "enterprise" ? 16 : usage?.plan === "pro" ? 8 : 2);
  const workerPercent = Math.min(100, Math.round((activeWorkers / maxWorkers) * 100));
  const workerTicks = Math.max(0, Math.min(36, Math.round((activeWorkers / maxWorkers) * 36)));

  const tokensUsed = (usage?.tokens_in ?? 0) + (usage?.tokens_out ?? 0) || (spend?.tokens_in ?? 0) + (spend?.tokens_out ?? 0);
  const tokenLimit = 2500000;
  const tokenPercent = Math.min(100, Math.round((tokensUsed / tokenLimit) * 100));
  const tokenTicks = Math.max(0, Math.min(36, Math.round((tokensUsed / tokenLimit) * 36)));

  const storageMB = cacheStats?.storage_footprint_mb ?? 0;
  const storageLimitMB = 16384;
  const storageGB = (storageMB / 1024).toFixed(2);
  const storageLimitGB = (storageLimitMB / 1024).toFixed(2);
  const storagePercent = Math.min(100, Math.round((storageMB / storageLimitMB) * 100));
  const storageTicks = Math.max(0, Math.min(36, Math.round((storageMB / storageLimitMB) * 36)));

  const prsDeliveredCount = (jobs || []).filter((j) => (j.pr_urls && j.pr_urls.length > 0) || j.status === "SUCCEEDED").length;
  const verifiedPassedCount = (jobs || []).filter((j) => j.status === "SUCCEEDED").length;
  const privateRunnersCount = (daemons || []).length;

  return (
    <div className="p-0 sm:p-2 md:p-4 space-y-5 sm:space-y-7 max-w-6xl mx-auto font-sans text-stone-900">
      
      {/* 1. ONBOARDING / PLAN BANNER WITH DYNAMIC UPGRADE CTA & ANIMATED MASCOT */}
      <div className="relative overflow-hidden p-5 rounded-3xl border border-sand-200/90 bg-gradient-to-r from-sand-100/90 via-white to-kiwi-50/70 backdrop-blur-xl flex flex-wrap items-center justify-between gap-4 text-xs shadow-2xs group">
        {/* Grain texture */}
        <div
          className="absolute inset-0 opacity-[0.035] pointer-events-none"
          style={{
            backgroundImage: `url("data:image/svg+xml,%3Csvg viewBox='0 0 200 200' xmlns='http://www.w3.org/2000/svg'%3E%3Cfilter id='n'%3E%3CfeTurbulence type='fractalNoise' baseFrequency='0.8' numOctaves='3'/%3E%3C/filter%3E%3Crect width='100%25' height='100%25' filter='url(%23n)'/%3E%3C/svg%3E")`,
          }}
        />
        {/* Corner light aura */}
        <div className="absolute -top-12 -right-12 w-36 h-36 bg-kiwi-400/20 rounded-full blur-3xl group-hover:scale-110 transition-transform" />

        <div className="relative z-10 flex items-center gap-3.5">
          <div className="w-11 h-11 rounded-2xl bg-white border border-sand-200/90 shadow-2xs flex items-center justify-center shrink-0">
            <Logo variant="full-color" pose="vibing" animated={true} className="w-7 h-7" />
          </div>
          <div>
            <div className="flex items-center gap-2">
              <p className="font-bold text-stone-900 capitalize text-sm">{plan} Tier Active ({limitMinutes} Agent-Minutes)</p>
              <span className="text-[9px] font-mono font-bold bg-amber-100 text-amber-800 px-1.5 py-0.2 rounded border border-amber-200 uppercase">
                {limitMinutes} MINS CAP
              </span>
            </div>
            <p className="text-stone-600 text-[11px] mt-0.5">
              {usedMinutes.toFixed(1)} / {limitMinutes} agent-minutes used ({percentUsed}%) • {maxWorkers} concurrent tasks • {plan === "enterprise" ? "BYOC Private Runners" : "Managed Cloud Fleet"}
            </p>
          </div>
        </div>

        <div className="relative z-10 flex items-center gap-2">
          <UpgradeButton plan={plan} variant="full" />
          <button
            onClick={() => setShowComposer(true)}
            className="px-3 py-1.5 rounded-xl bg-white hover:bg-sand-100 border border-sand-300 text-stone-800 font-semibold text-xs shadow-2xs transition-all cursor-pointer"
          >
            + New Task
          </button>
        </div>
      </div>

      {/* 2. ACTIVE WORK & CAPACITY TILES (HYBRID FROSTED + LIGHT AURA + SPARKLINES) */}
      <div className="space-y-4">
        <div className="flex items-center justify-between">
          <div>
            <h1 className="text-base font-bold text-stone-900 tracking-tight">Active Work & Capacity</h1>
            <p className="text-xs text-stone-500 mt-0.5">Real-time status of autonomous agents building code and passing automated tests.</p>
          </div>
          <button
            onClick={() => setShowComposer(true)}
            className="px-3.5 py-1.5 rounded-xl bg-stone-900 hover:bg-stone-800 text-white font-semibold text-xs shadow-2xs flex items-center gap-1.5 transition-all cursor-pointer"
          >
            <Plus className="w-3.5 h-3.5 text-kiwi-400" />
            <span>New Task</span>
          </button>
        </div>

        <div className="grid grid-cols-2 sm:grid-cols-4 gap-3">
          {/* Tile 1: Connected Repos */}
          <div className="relative overflow-hidden p-4 rounded-2xl border border-sand-200 bg-white/85 backdrop-blur-xl hover:border-kiwi-300 hover:shadow-island transition-all shadow-2xs group">
            <div
              className="absolute inset-0 opacity-[0.03] pointer-events-none"
              style={{
                backgroundImage: `url("data:image/svg+xml,%3Csvg viewBox='0 0 200 200' xmlns='http://www.w3.org/2000/svg'%3E%3Cfilter id='n'%3E%3CfeTurbulence type='fractalNoise' baseFrequency='0.8' numOctaves='3'/%3E%3C/filter%3E%3Crect width='100%25' height='100%25' filter='url(%23n)'/%3E%3C/svg%3E")`,
              }}
            />
            <div className="absolute -top-8 -right-8 w-20 h-20 bg-kiwi-400/20 rounded-full blur-2xl group-hover:scale-125 transition-transform" />

            <div className="relative z-10 flex items-center justify-between text-stone-600 text-xs font-medium mb-1">
              <span className="flex items-center gap-1.5">
                <FolderGit2 className="w-3.5 h-3.5 text-stone-500" />
                Connected Repos
              </span>
            </div>
            <p className="relative z-10 text-2xl font-bold text-stone-900 tracking-tight font-mono">{repos.length}</p>
            {/* Sparkline track */}
            <div className="relative z-10 mt-2.5 flex items-end gap-1 h-3.5">
              {[40, 55, 45, 70, 60, 85, 80].map((h, i) => (
                <div key={i} className="flex-1 bg-kiwi-200 rounded-2xs" style={{ height: `${h}%` }} />
              ))}
            </div>
          </div>

          {/* Tile 2: PRs Delivered */}
          <div className="relative overflow-hidden p-4 rounded-2xl border border-sand-200 bg-white/85 backdrop-blur-xl hover:border-amber-300 hover:shadow-island transition-all shadow-2xs group">
            <div
              className="absolute inset-0 opacity-[0.03] pointer-events-none"
              style={{
                backgroundImage: `url("data:image/svg+xml,%3Csvg viewBox='0 0 200 200' xmlns='http://www.w3.org/2000/svg'%3E%3Cfilter id='n'%3E%3CfeTurbulence type='fractalNoise' baseFrequency='0.8' numOctaves='3'/%3E%3C/filter%3E%3Crect width='100%25' height='100%25' filter='url(%23n)'/%3E%3C/svg%3E")`,
              }}
            />
            <div className="absolute -top-8 -right-8 w-20 h-20 bg-amber-400/20 rounded-full blur-2xl group-hover:scale-125 transition-transform" />

            <div className="relative z-10 flex items-center justify-between text-stone-600 text-xs font-medium mb-1">
              <span className="flex items-center gap-1.5">
                <GitPullRequest className="w-3.5 h-3.5 text-amber-600" />
                PRs Delivered
              </span>
            </div>
            <p className="relative z-10 text-2xl font-bold text-stone-900 tracking-tight font-mono">{prsDeliveredCount}</p>
            <div className="relative z-10 mt-2.5 flex items-end gap-1 h-3.5">
              {[30, 45, 60, 40, 75, 90, 85].map((h, i) => (
                <div key={i} className="flex-1 bg-amber-200 rounded-2xs" style={{ height: `${h}%` }} />
              ))}
            </div>
          </div>

          {/* Tile 3: Verified Passed */}
          <div className="relative overflow-hidden p-4 rounded-2xl border border-sand-200 bg-white/85 backdrop-blur-xl hover:border-emerald-300 hover:shadow-island transition-all shadow-2xs group">
            <div
              className="absolute inset-0 opacity-[0.03] pointer-events-none"
              style={{
                backgroundImage: `url("data:image/svg+xml,%3Csvg viewBox='0 0 200 200' xmlns='http://www.w3.org/2000/svg'%3E%3Cfilter id='n'%3E%3CfeTurbulence type='fractalNoise' baseFrequency='0.8' numOctaves='3'/%3E%3C/filter%3E%3Crect width='100%25' height='100%25' filter='url(%23n)'/%3E%3C/svg%3E")`,
              }}
            />
            <div className="absolute -top-8 -right-8 w-20 h-20 bg-emerald-400/20 rounded-full blur-2xl group-hover:scale-125 transition-transform" />

            <div className="relative z-10 flex items-center justify-between text-stone-600 text-xs font-medium mb-1">
              <span className="flex items-center gap-1.5">
                <CheckCircle2 className="w-3.5 h-3.5 text-emerald-600" />
                Verified Passed
              </span>
            </div>
            <p className="relative z-10 text-2xl font-bold text-stone-900 tracking-tight font-mono">{verifiedPassedCount}</p>
            <div className="relative z-10 mt-2.5 flex items-end gap-1 h-3.5">
              {[70, 80, 85, 90, 95, 98, 100].map((h, i) => (
                <div key={i} className="flex-1 bg-emerald-200 rounded-2xs" style={{ height: `${h}%` }} />
              ))}
            </div>
          </div>

          {/* Tile 4: Private Runners */}
          <div className="relative overflow-hidden p-4 rounded-2xl border border-sand-200 bg-white/85 backdrop-blur-xl hover:border-purple-300 hover:shadow-island transition-all shadow-2xs group">
            <div
              className="absolute inset-0 opacity-[0.03] pointer-events-none"
              style={{
                backgroundImage: `url("data:image/svg+xml,%3Csvg viewBox='0 0 200 200' xmlns='http://www.w3.org/2000/svg'%3E%3Cfilter id='n'%3E%3CfeTurbulence type='fractalNoise' baseFrequency='0.8' numOctaves='3'/%3E%3C/filter%3E%3Crect width='100%25' height='100%25' filter='url(%23n)'/%3E%3C/svg%3E")`,
              }}
            />
            <div className="absolute -top-8 -right-8 w-20 h-20 bg-purple-400/20 rounded-full blur-2xl group-hover:scale-125 transition-transform" />

            <div className="relative z-10 flex items-center justify-between text-stone-600 text-xs font-medium mb-1">
              <span className="flex items-center gap-1.5">
                <Server className="w-3.5 h-3.5 text-purple-600" />
                Private Runners
              </span>
            </div>
            <p className="relative z-10 text-2xl font-bold text-stone-900 tracking-tight font-mono">{privateRunnersCount}</p>
            <div className="relative z-10 mt-2.5 flex items-end gap-1 h-3.5">
              {[50, 50, 60, 60, 80, 80, 100].map((h, i) => (
                <div key={i} className="flex-1 bg-purple-200 rounded-2xs" style={{ height: `${h}%` }} />
              ))}
            </div>
          </div>
        </div>
      </div>

      <hr className="border-sand-200/80" />

      {/* 3. MONTHLY CAPACITY & LIMITS (HARDWARE SEGMENTED METERS) */}
      <div className="space-y-5">
        <div className="flex flex-wrap items-center justify-between gap-2">
          <div>
            <h2 className="text-sm font-bold text-stone-900">Monthly Capacity & Limits</h2>
            <p className="text-xs text-stone-500 mt-0.5">Current usage across parallel agent workers, monthly AI tokens, and workspace cache.</p>
          </div>
          <div className="flex items-center gap-2">
            <UpgradeButton plan={plan} variant="compact" />
            <span className="px-2.5 py-1 rounded-lg bg-sand-150 text-stone-800 text-[11px] font-mono font-bold uppercase">
              PLAN: {plan.toUpperCase()} ({maxWorkers} WORKERS CAP)
            </span>
          </div>
        </div>

        {/* Meter 1: Active Workers */}
        <div className="space-y-1.5 p-3.5 rounded-2xl bg-sand-50/60 border border-sand-200">
          <div className="flex items-center justify-between text-xs">
            <span className="font-medium text-stone-700">Active Agent Workers</span>
          </div>
          <SegmentedMeter totalTicks={36} activeTicks={workerTicks} activeColorClass="meter-tick-active-emerald" />
          <div className="flex items-center justify-between text-xs text-stone-500 font-mono">
            <span>Active: <strong className="text-stone-800">{activeWorkers} of {maxWorkers} Workers ({workerPercent}%)</strong></span>
            <span>Concurrent Limit: {maxWorkers} Workers</span>
          </div>
        </div>

        {/* Meter 2: Monthly Token Allowance */}
        <div className="space-y-1.5 p-3.5 rounded-2xl bg-sand-50/60 border border-sand-200">
          <div className="flex items-center justify-between text-xs">
            <span className="font-medium text-stone-700">Monthly AI Token Allowance (Anthropic, Gemini & OpenAI)</span>
          </div>
          <SegmentedMeter totalTicks={36} activeTicks={tokenTicks} activeColorClass="meter-tick-active-orange" />
          <div className="flex items-center justify-between text-xs text-stone-500 font-mono">
            <span>Used: <strong className="text-stone-800">{(tokensUsed / 1000000).toFixed(2)}M of {(tokenLimit / 1000000).toFixed(2)}M Tokens ({tokenPercent}%)</strong></span>
            <span>Monthly Budget: {(tokenLimit / 1000000).toFixed(2)}M Tokens</span>
          </div>
        </div>

        {/* Meter 3: Sandbox Memory & Cache */}
        <div className="space-y-1.5 p-3.5 rounded-2xl bg-sand-50/60 border border-sand-200">
          <div className="flex items-center justify-between text-xs">
            <span className="font-medium text-stone-700">Sandbox Memory & Workspace Cache</span>
          </div>
          <SegmentedMeter totalTicks={36} activeTicks={storageTicks} activeColorClass="meter-tick-active-kiwi" />
          <div className="flex items-center justify-between text-xs text-stone-500 font-mono">
            <span>Allocated: <strong className="text-stone-800">{storageGB} GB / {storageLimitGB} GB ({storagePercent}%)</strong></span>
            <span>Cache Hit Rate: {cacheStats?.cache_hit_rate_pct ? `${cacheStats.cache_hit_rate_pct.toFixed(1)}%` : "N/A"}</span>
          </div>
        </div>
      </div>

      <hr className="border-sand-200/80" />

      {/* 4. TASK EXECUTION QUEUE */}
      <div className="space-y-3.5" data-tour="tasks-queue">
        <div className="flex flex-wrap items-center justify-between gap-2">
          <div>
            <h2 className="text-sm font-bold text-stone-900">Task Execution Queue</h2>
            <p className="text-xs text-stone-500 mt-0.5">Click to inspect real-time progress, live logs, code diffs, and verification receipts.</p>
          </div>
          <div className="flex flex-wrap items-center gap-1 text-xs">
            <button
              onClick={() => setStatusFilter("all")}
              className={`px-2.5 py-1 rounded-lg font-semibold text-[11px] transition-all cursor-pointer ${
                statusFilter === "all"
                  ? "bg-stone-900 text-white shadow-2xs"
                  : "text-stone-600 hover:text-stone-900 hover:bg-sand-150"
              }`}
            >
              All ({statusCounts.all})
            </button>
            <button
              onClick={() => setStatusFilter("running")}
              className={`px-2.5 py-1 rounded-lg font-semibold text-[11px] transition-all flex items-center gap-1.5 cursor-pointer ${
                statusFilter === "running"
                  ? "bg-stone-900 text-white shadow-2xs"
                  : "text-stone-600 hover:text-stone-900 hover:bg-sand-150"
              }`}
            >
              <span className={`w-1.5 h-1.5 rounded-full ${statusFilter === "running" ? "bg-sky-400" : "bg-sky-500"} ${statusCounts.running > 0 ? "animate-pulse" : ""}`} />
              <span>Running ({statusCounts.running})</span>
            </button>
            <button
              onClick={() => setStatusFilter("plan")}
              className={`px-2.5 py-1 rounded-lg font-semibold text-[11px] transition-all flex items-center gap-1.5 cursor-pointer ${
                statusFilter === "plan"
                  ? "bg-stone-900 text-white shadow-2xs"
                  : "text-stone-600 hover:text-stone-900 hover:bg-sand-150"
              }`}
            >
              <span className={`w-1.5 h-1.5 rounded-full ${statusFilter === "plan" ? "bg-indigo-400" : "bg-indigo-500"}`} />
              <span>📋 Plan Review ({statusCounts.planReview})</span>
            </button>
            {statusCounts.waiting > 0 && (
              <button
                onClick={() => setStatusFilter("waiting")}
                className={`px-2.5 py-1 rounded-lg font-semibold text-[11px] transition-all flex items-center gap-1.5 cursor-pointer ${
                  statusFilter === "waiting"
                    ? "bg-stone-900 text-white shadow-2xs"
                    : "text-stone-600 hover:text-stone-900 hover:bg-sand-150"
                }`}
              >
                <span className={`w-1.5 h-1.5 rounded-full ${statusFilter === "waiting" ? "bg-amber-400" : "bg-amber-500"} animate-ping`} />
                <span>Input Needed ({statusCounts.waiting})</span>
              </button>
            )}
            <button
              onClick={() => setStatusFilter("pr_created")}
              className={`px-2.5 py-1 rounded-lg font-semibold text-[11px] transition-all flex items-center gap-1.5 cursor-pointer ${
                statusFilter === "pr_created"
                  ? "bg-stone-900 text-white shadow-2xs"
                  : "text-stone-600 hover:text-stone-900 hover:bg-sand-150"
              }`}
            >
              <CheckCircle2 className={`w-3 h-3 ${statusFilter === "pr_created" ? "text-emerald-400" : "text-emerald-600"}`} />
              <span>PR Ready ({statusCounts.prReady})</span>
            </button>
            <button
              onClick={() => setStatusFilter("failed")}
              className={`px-2.5 py-1 rounded-lg font-semibold text-[11px] transition-all flex items-center gap-1.5 cursor-pointer ${
                statusFilter === "failed"
                  ? "bg-stone-900 text-white shadow-2xs"
                  : "text-stone-600 hover:text-stone-900 hover:bg-sand-150"
              }`}
            >
              <AlertCircle className={`w-3 h-3 ${statusFilter === "failed" ? "text-rose-400" : "text-rose-600"}`} />
              <span>Failed ({statusCounts.failed})</span>
            </button>
            {statusCounts.slack > 0 && (
              <button
                onClick={() => setStatusFilter("slack")}
                className={`px-2.5 py-1 rounded-lg font-semibold text-[11px] transition-all flex items-center gap-1.5 cursor-pointer ${
                  statusFilter === "slack"
                    ? "bg-[#4A154B] text-white shadow-2xs"
                    : "text-stone-600 hover:text-stone-900 hover:bg-sand-150"
                }`}
              >
                <FaSlack className={`w-3 h-3 ${statusFilter === "slack" ? "text-white" : "text-[#4A154B]"}`} aria-hidden="true" />
                <span>Slack ({statusCounts.slack})</span>
              </button>
            )}
          </div>
        </div>

        {/* Task Cards List */}
        {loading ? (
          <div className="p-8 rounded-2xl border border-sand-200 bg-sand-50/50 text-center">
            <ThinkingOrb state="working" size={32} />
          </div>
        ) : filteredJobs.length === 0 ? (
          <div className="relative overflow-hidden p-10 rounded-3xl border border-sand-200 bg-white/80 backdrop-blur-xl text-center space-y-3.5 shadow-2xs group">
            <div
              className="absolute inset-0 opacity-[0.035] pointer-events-none"
              style={{
                backgroundImage: `url("data:image/svg+xml,%3Csvg viewBox='0 0 200 200' xmlns='http://www.w3.org/2000/svg'%3E%3Cfilter id='n'%3E%3CfeTurbulence type='fractalNoise' baseFrequency='0.8' numOctaves='3'/%3E%3C/filter%3E%3Crect width='100%25' height='100%25' filter='url(%23n)'/%3E%3C/svg%3E")`,
              }}
            />
            <div className="absolute -top-10 -right-10 w-28 h-28 bg-kiwi-400/15 rounded-full blur-2xl pointer-events-none" />

            <div className="relative z-10 w-14 h-14 mx-auto rounded-2xl bg-sand-50 border border-sand-200/80 shadow-2xs flex items-center justify-center">
              <Logo variant="full-color" pose="sleeping" animated={true} className="w-8 h-8" />
            </div>
            <div className="relative z-10 space-y-1">
              <p className="text-xs font-bold text-stone-800">
                {statusFilter === "all"
                  ? "Queue is quiet and resting"
                  : `No ${statusFilter === "pr_created" ? "PR ready" : statusFilter === "plan" ? "plan review" : statusFilter} tasks`}
              </p>
              <p className="text-[11px] text-stone-500 max-w-sm mx-auto">
                {statusFilter === "all"
                  ? "No active tasks match this filter. Assign a new programming objective or run an automated refactor."
                  : `There are currently no tasks matching the "${statusFilter === "pr_created" ? "PR ready" : statusFilter}" filter.`}
              </p>
            </div>
            <div className="relative z-10 pt-1 flex items-center justify-center gap-2 flex-wrap">
              {statusFilter !== "all" && (
                <button
                  onClick={() => setStatusFilter("all")}
                  className="px-3.5 py-2 rounded-xl bg-sand-100 hover:bg-sand-200 text-stone-800 font-semibold text-xs border border-sand-200 shadow-2xs transition-all cursor-pointer inline-flex items-center gap-1.5"
                >
                  <span>Clear Filter (Show All)</span>
                </button>
              )}
              <button
                onClick={() => setShowComposer(true)}
                className="px-4 py-2 rounded-xl bg-stone-900 hover:bg-stone-800 text-white font-semibold text-xs shadow-2xs transition-all cursor-pointer inline-flex items-center gap-1.5"
              >
                <Plus className="w-3.5 h-3.5 text-kiwi-400" />
                <span>Assign New Task</span>
              </button>
            </div>
          </div>
        ) : (
          <div className="space-y-3.5">
            {filteredJobs.map((job) => {
              const isFailed = job.status === "FAILED" || job.plan_status === "rejected";
              const isCancelled = job.status === "CANCELLED";
              const isSucceeded = job.status === "SUCCEEDED" || (job.pr_urls && job.pr_urls.length > 0);
              const isPlanReview = !isFailed && !isCancelled && !isSucceeded && (job.status === "PLAN_REVIEW" || job.status === "AWAITING_PLAN_APPROVAL" || job.plan_status === "pending_review");
              const isWaitingInput = job.status === "WAITING_USER" && !isFailed && !isCancelled && !isSucceeded;
              const isRunning = (job.status === "LEASED" || job.status === "RUNNING") && !isPlanReview && !isFailed && !isCancelled && !isSucceeded;

              const prLink = (job.pr_urls && job.pr_urls.length > 0 ? job.pr_urls[0] : null) ||
                (job.repo && job.pr_number ? `https://github.com/${job.repo}/pull/${job.pr_number}` : null);

              const architectModel = job.architect_model ? job.architect_model.split("/").pop() : null;
              const workerModel = job.worker_model ? job.worker_model.split("/").pop() : null;
              const totalTokens = (job.tokens_in ?? 0) + (job.tokens_out ?? 0);
              const timeAgo = job.created_at ? shortTime(job.created_at) : "";

              // Compute stage
              let stage = 1;
              if (isSucceeded) stage = 4;
              else if (isRunning) stage = 3;
              else if (isFailed && job.plan_status !== "rejected") stage = 3;
              else if (isPlanReview || isWaitingInput || (isFailed && job.plan_status === "rejected")) stage = 2;

              return (
                <div
                  key={job.job_id}
                  onClick={() => setActiveDrawerTaskId(job.job_id)}
                  className={`relative overflow-hidden p-4 sm:p-5 rounded-xl border transition-all cursor-pointer shadow-2xs group bg-white ${
                    isFailed
                      ? "border-rose-200/90 bg-gradient-to-r from-rose-50/25 via-white to-white hover:border-rose-300 hover:shadow-xs"
                      : isPlanReview
                      ? "border-indigo-300/80 bg-gradient-to-r from-indigo-50/25 via-white to-white hover:border-indigo-400 hover:shadow-xs"
                      : isWaitingInput
                      ? "border-amber-300/80 bg-gradient-to-r from-amber-50/25 via-white to-white hover:border-amber-400 hover:shadow-xs"
                      : isSucceeded
                      ? "border-emerald-200/90 bg-gradient-to-r from-emerald-50/20 via-white to-white hover:border-emerald-300 hover:shadow-xs"
                      : isRunning
                      ? "border-sky-200/90 bg-gradient-to-r from-sky-50/20 via-white to-white hover:border-sky-300 hover:shadow-xs"
                      : isCancelled
                      ? "border-sand-200/90 bg-sand-50/40 hover:border-stone-400 hover:shadow-xs"
                      : "border-sand-200/90 hover:border-stone-400/90 hover:shadow-xs"
                  }`}
                >
                  {/* Top Edge Gradient Beam Shimmer (Option 1) */}
                  {isRunning && (
                    <div className="absolute top-0 left-0 right-0 h-[2px] overflow-hidden rounded-t-xl bg-sky-100/50 pointer-events-none">
                      <div className="w-1/2 h-full bg-gradient-to-r from-transparent via-sky-500 to-transparent animate-beam" />
                    </div>
                  )}

                  {/* Header Row */}
                  <div className="flex flex-wrap items-center justify-between gap-2 mb-2.5">
                    <div className="flex items-center gap-2 flex-wrap">
                      <span className="text-xs font-mono font-bold text-stone-900 flex items-center gap-1">
                        <FolderGit2 className="w-3.5 h-3.5 text-stone-500" />
                        <span>{job.repo || "RunKiwi/kiwi"}</span>
                      </span>
                      <span className="font-mono text-[11px] text-stone-400">
                        #job_{job.job_id.slice(0, 8)}
                      </span>
                      {timeAgo && (
                        <span className="text-xs text-stone-400 font-mono">
                          • {timeAgo}
                        </span>
                      )}
                      {job.requires_plan_approval && (
                        <span className="px-1.5 py-0.2 rounded font-mono text-[9px] font-bold bg-indigo-50 text-indigo-800 border border-indigo-200 uppercase">
                          PLAN MODE
                        </span>
                      )}
                      {job.is_dry_run && (
                        <span className="px-1.5 py-0.2 rounded font-mono text-[9px] font-bold bg-sky-50 text-sky-800 border border-sky-200 uppercase">
                          DRY-RUN
                        </span>
                      )}
                      {job.latest_origin === "slack" && (
                        <span className="flex items-center gap-1 px-1.5 py-0.2 rounded font-mono text-[9px] font-bold bg-[#4A154B]/10 text-[#4A154B] border border-[#4A154B]/25 uppercase">
                          <FaSlack className="w-2.5 h-2.5" aria-hidden="true" />
                          Slack
                        </span>
                      )}
                    </div>

                    <div className="flex items-center gap-2">
                      {prLink && (
                        <a
                          href={prLink}
                          target="_blank"
                          rel="noreferrer"
                          onClick={(e) => e.stopPropagation()}
                          className="px-2 py-0.5 rounded-md text-[10px] font-mono font-bold bg-emerald-50 hover:bg-emerald-100 text-emerald-800 border border-emerald-200 flex items-center gap-1 shadow-2xs transition-all"
                        >
                          <GitPullRequest className="w-3 h-3 text-emerald-600" />
                          <span>#{job.pr_number || "PR"}</span>
                        </a>
                      )}

                      <span
                        className={`px-2 py-0.5 rounded-md text-[10px] font-mono font-bold border flex items-center gap-1.5 ${
                          isFailed
                            ? "text-rose-900 bg-rose-50 border-rose-200"
                            : isPlanReview
                            ? "text-indigo-800 bg-indigo-50 border-indigo-200"
                            : isWaitingInput
                            ? "text-amber-800 bg-amber-50 border-amber-200"
                            : isRunning
                            ? "text-sky-900 bg-sky-50 border-sky-200"
                            : isSucceeded
                            ? "text-emerald-900 bg-emerald-50 border-emerald-200"
                            : isCancelled
                            ? "text-stone-600 bg-sand-100 border-sand-200"
                            : "text-amber-800 bg-amber-50 border-amber-200"
                        }`}
                      >
                        {isRunning && (
                          <span className="relative flex h-1.5 w-1.5">
                            <span className="animate-ping absolute inline-flex h-full w-full rounded-full bg-sky-400 opacity-75" />
                            <span className="relative inline-flex rounded-full h-1.5 w-1.5 bg-sky-600" />
                          </span>
                        )}
                        {isPlanReview && (
                          <span className="relative flex h-1.5 w-1.5">
                            <span className="animate-ping absolute inline-flex h-full w-full rounded-full bg-indigo-400 opacity-75" />
                            <span className="relative inline-flex rounded-full h-1.5 w-1.5 bg-indigo-600" />
                          </span>
                        )}
                        {isWaitingInput && (
                          <span className="relative flex h-1.5 w-1.5">
                            <span className="animate-ping absolute inline-flex h-full w-full rounded-full bg-amber-400 opacity-75" />
                            <span className="relative inline-flex rounded-full h-1.5 w-1.5 bg-amber-600" />
                          </span>
                        )}
                        {isFailed && <AlertCircle className="w-3 h-3 text-rose-600 shrink-0" />}
                        {isCancelled && <Ban className="w-3 h-3 text-stone-400 shrink-0" />}
                        {isSucceeded && <CheckCircle2 className="w-3 h-3 text-emerald-600 shrink-0" />}
                        <span>
                          {isFailed
                            ? "FAILED"
                            : isPlanReview
                            ? "ACTION: PLAN READY"
                            : isWaitingInput
                            ? "ACTION: INPUT NEEDED"
                            : isRunning
                            ? "RUNNING"
                            : isSucceeded
                            ? "PR READY"
                            : isCancelled
                            ? "CANCELLED"
                            : job.status || "QUEUED"}
                        </span>
                      </span>
                    </div>
                  </div>

                  {/* Task Prompt / Title */}
                  <h3 className="text-sm font-bold text-stone-900 group-hover:text-kiwi-800 transition-colors mb-3 leading-snug">
                    {job.task || "Autonomous execution task"}
                  </h3>

                  {/* Actionable Callout Banners */}
                  {isFailed && (
                    <div className="mb-3 p-2.5 rounded-xl bg-rose-50/90 border border-rose-200 flex items-center justify-between text-xs text-rose-950 shadow-2xs">
                      <span className="flex items-center gap-2">
                        <AlertTriangle className="w-4 h-4 text-rose-600 shrink-0" />
                        <span className="text-[11px]">
                          <strong>Execution halted:</strong> {job.plan_status === "rejected" ? "Plan was rejected during review." : "Automated tests or agent execution encountered an error."}
                        </span>
                      </span>
                      <span className="text-[10px] font-mono font-bold text-rose-800 bg-white px-2 py-0.5 rounded-md border border-rose-200 shrink-0 flex items-center gap-1">
                        <RotateCcw className="w-2.5 h-2.5" />
                        <span>Inspect Error</span>
                      </span>
                    </div>
                  )}

                  {isPlanReview && (
                    <div className="mb-3 p-2.5 rounded-xl bg-indigo-50/90 border border-indigo-200 flex items-center justify-between text-xs text-indigo-950 shadow-2xs">
                      <span className="flex items-center gap-2">
                        <span className="w-2 h-2 rounded-full bg-indigo-600 shrink-0" />
                        <span className="text-[11px]">
                          <strong>Execution plan ready:</strong> Review and approve before agents write code.
                        </span>
                      </span>
                      <span className="text-[10px] font-mono font-bold text-indigo-800 bg-white px-2 py-0.5 rounded-md border border-indigo-200 shrink-0">
                        Review Plan
                      </span>
                    </div>
                  )}

                  {isWaitingInput && (
                    <div className="mb-3 p-2.5 rounded-xl bg-amber-50/90 border border-amber-200 flex items-center justify-between text-xs text-amber-950 shadow-2xs">
                      <span className="flex items-center gap-2">
                        <span className="w-2 h-2 rounded-full bg-amber-600 shrink-0" />
                        <span className="text-[11px]">
                          <strong>Agent paused for input:</strong> Reply with clarification to continue.
                        </span>
                      </span>
                      <span className="text-[10px] font-mono font-bold text-amber-800 bg-white px-2 py-0.5 rounded-md border border-amber-200 shrink-0">
                        Input Needed
                      </span>
                    </div>
                  )}

                  {/* Universal 4-Stage Lifecycle & Pass/Fail Pipeline Strip */}
                  <div className="grid grid-cols-2 sm:grid-cols-4 gap-1.5 p-1 bg-sand-50/80 rounded-xl border border-sand-200 text-[10px] font-mono mb-3 shadow-2xs">
                    {/* 1. Plan */}
                    <div
                      className={`px-2 py-1.5 rounded-lg border flex items-center justify-between transition-colors ${
                        stage >= 1
                          ? isRunning && stage === 1
                            ? "bg-sky-50/90 border-sky-300 text-sky-950 font-bold shadow-[0_0_12px_rgba(14,165,233,0.22)] ring-1 ring-sky-300/70"
                            : "bg-white border-sand-200 text-stone-800 font-semibold"
                          : "text-stone-400 border-transparent"
                      }`}
                    >
                      <div className="flex items-center gap-1.5 min-w-0">
                        <span
                          className={`w-1.5 h-1.5 rounded-full shrink-0 ${
                            stage >= 1 ? (isRunning && stage === 1 ? "bg-sky-500" : "bg-emerald-500") : "bg-stone-300"
                          }`}
                        />
                        <span className="truncate">1. Plan</span>
                      </div>
                      <span className="text-[10px] shrink-0 font-bold">
                        {stage > 1 || isSucceeded || isFailed ? (
                          <span className="text-emerald-600">✓</span>
                        ) : isRunning && stage === 1 ? (
                          <Loader2 className="w-3 h-3 animate-spin text-sky-600" />
                        ) : (
                          <span className="text-stone-300 font-normal">—</span>
                        )}
                      </span>
                    </div>

                    {/* 2. Review / Env Prep */}
                    <div
                      className={`px-2 py-1.5 rounded-lg border flex items-center justify-between transition-colors ${
                        isPlanReview
                          ? "bg-indigo-100 border-indigo-300 text-indigo-950 font-bold shadow-2xs"
                          : isFailed && job.plan_status === "rejected"
                          ? "bg-rose-50 border-rose-200 text-rose-800 font-bold"
                          : isRunning && stage === 2
                          ? "bg-sky-50/90 border-sky-300 text-sky-950 font-bold shadow-[0_0_12px_rgba(14,165,233,0.22)] ring-1 ring-sky-300/70"
                          : stage >= 2
                          ? "bg-white border-sand-200 text-stone-800 font-semibold"
                          : isFailed && job.plan_status !== "rejected"
                          ? "bg-white border-sand-200 text-stone-800 font-semibold"
                          : "text-stone-400 border-transparent"
                      }`}
                    >
                      <div className="flex items-center gap-1.5 min-w-0">
                        <span
                          className={`w-1.5 h-1.5 rounded-full shrink-0 ${
                            isPlanReview
                              ? "bg-indigo-600"
                              : isFailed && job.plan_status === "rejected"
                              ? "bg-rose-500"
                              : stage > 2 || isSucceeded || (isFailed && job.plan_status !== "rejected")
                              ? "bg-emerald-500"
                              : stage === 2 && isRunning
                              ? "bg-sky-500"
                              : "bg-stone-300"
                          }`}
                        />
                        <span className="truncate">
                          {job.requires_plan_approval ? "2. Review" : "2. Env Prep"}
                        </span>
                      </div>
                      <span className="text-[10px] shrink-0 font-bold">
                        {isFailed && job.plan_status === "rejected" ? (
                          <span className="text-rose-600">✕ Rejected</span>
                        ) : isPlanReview ? (
                          <span className="text-indigo-600 text-[9px] font-bold">Action</span>
                        ) : stage > 2 || isSucceeded || (isFailed && job.plan_status !== "rejected") ? (
                          <span className="text-emerald-600">✓</span>
                        ) : isRunning && stage === 2 ? (
                          <Loader2 className="w-3 h-3 animate-spin text-sky-600" />
                        ) : (
                          <span className="text-stone-300 font-normal">—</span>
                        )}
                      </span>
                    </div>

                    {/* 3. Code & Test (Option 2: Stage Glow & Spinner) */}
                    <div
                      className={`px-2 py-1.5 rounded-lg border flex items-center justify-between transition-all ${
                        isWaitingInput
                          ? "bg-amber-100 border-amber-300 text-amber-950 font-bold shadow-2xs"
                          : isFailed && job.plan_status !== "rejected"
                          ? "bg-rose-50 border-rose-200 text-rose-800 font-bold"
                          : isRunning && stage === 3
                          ? "bg-sky-50/90 border-sky-300 text-sky-950 font-bold shadow-[0_0_12px_rgba(14,165,233,0.22)] ring-1 ring-sky-300/70"
                          : stage >= 3
                          ? "bg-white border-sand-200 text-stone-800 font-semibold"
                          : isCancelled
                          ? "bg-sand-100/60 border-sand-200 text-stone-500"
                          : isFailed && job.plan_status === "rejected"
                          ? "text-stone-400 border-transparent opacity-40"
                          : "text-stone-400 border-transparent"
                      }`}
                    >
                      <div className="flex items-center gap-1.5 min-w-0">
                        <span
                          className={`w-1.5 h-1.5 rounded-full shrink-0 ${
                            isWaitingInput
                              ? "bg-amber-600"
                              : isFailed && job.plan_status !== "rejected"
                              ? "bg-rose-500"
                              : isRunning && stage === 3
                              ? "bg-sky-600"
                              : isSucceeded || stage > 3
                              ? "bg-emerald-500"
                              : isCancelled
                              ? "bg-stone-400"
                              : "bg-stone-300"
                          }`}
                        />
                        <span className="truncate">3. Code & Test</span>
                      </div>
                      <span className="text-[10px] shrink-0 font-bold">
                        {isFailed && job.plan_status !== "rejected" ? (
                          <span className="text-rose-600">✕ Failed</span>
                        ) : isWaitingInput ? (
                          <span className="text-amber-700 text-[9px]">Input</span>
                        ) : isSucceeded ? (
                          <span className="text-emerald-600">✓</span>
                        ) : isRunning && stage === 3 ? (
                          <Loader2 className="w-3 h-3 animate-spin text-sky-600" />
                        ) : isCancelled ? (
                          <span className="text-stone-400 font-normal">⊘</span>
                        ) : (
                          <span className="text-stone-300 font-normal">—</span>
                        )}
                      </span>
                    </div>

                    {/* 4. Delivery */}
                    <div
                      className={`px-2 py-1.5 rounded-lg border flex items-center justify-between transition-colors ${
                        isSucceeded
                          ? "bg-emerald-50 border-emerald-200 text-emerald-900 font-bold"
                          : isFailed
                          ? "text-stone-400 border-transparent opacity-40"
                          : "text-stone-400 border-transparent"
                      }`}
                    >
                      <div className="flex items-center gap-1.5 min-w-0">
                        <span
                          className={`w-1.5 h-1.5 rounded-full shrink-0 ${
                            isSucceeded ? "bg-emerald-600" : "bg-stone-300"
                          }`}
                        />
                        <span className="truncate">{job.is_dry_run ? "4. Dry-Run" : "4. PR Ready"}</span>
                      </div>
                      <span className="text-[10px] shrink-0 font-bold">
                        {isSucceeded ? (
                          <span className="text-emerald-700">✓</span>
                        ) : isCancelled ? (
                          <span className="text-stone-300 font-normal">⊘</span>
                        ) : (
                          <span className="text-stone-300 font-normal">—</span>
                        )}
                      </span>
                    </div>
                  </div>

                  {/* Telemetry & Action Footer Row */}
                  <div className="flex flex-wrap items-center justify-between text-[11px] text-stone-500 font-mono pt-1">
                    <div className="flex items-center gap-2.5 flex-wrap">
                      {architectModel && workerModel && architectModel !== workerModel ? (
                        <>
                          <span className="text-stone-700 font-medium flex items-center gap-1">
                            <Compass className="w-3 h-3 text-indigo-500" />
                            <span>{architectModel}</span>
                          </span>
                          <span>+</span>
                          <span className="text-stone-700 font-medium flex items-center gap-1">
                            <Hammer className="w-3 h-3 text-emerald-500" />
                            <span>{workerModel}</span>
                          </span>
                        </>
                      ) : workerModel ? (
                        <span className="text-stone-700 font-medium flex items-center gap-1">
                          <Hammer className="w-3 h-3 text-emerald-500" />
                          <span>{workerModel}</span>
                        </span>
                      ) : architectModel ? (
                        <span className="text-stone-700 font-medium flex items-center gap-1">
                          <Compass className="w-3 h-3 text-indigo-500" />
                          <span>{architectModel}</span>
                        </span>
                      ) : (
                        <span className="text-stone-600 font-mono">
                          {job.task_count ?? 1} {job.task_count === 1 ? "task" : "tasks"}
                        </span>
                      )}
                      <span>•</span>
                      <span className="text-kiwi-700 font-bold">{formatCost(job.cost_usd ?? 0)}</span>
                      {job.spend_cap_usd && (
                        <>
                          <span className="text-stone-400">(Cap: ${job.spend_cap_usd.toFixed(2)})</span>
                        </>
                      )}
                      {totalTokens > 0 && (
                        <>
                          <span>•</span>
                          <span className="text-stone-500">{formatTokens(totalTokens)} tok</span>
                        </>
                      )}
                    </div>

                    <div>
                      {isFailed ? (
                        <button
                          onClick={(e) => {
                            e.stopPropagation();
                            setActiveDrawerTaskId(job.job_id);
                          }}
                          className="px-3 py-1 rounded-lg bg-rose-600 hover:bg-rose-700 text-white font-semibold text-xs flex items-center gap-1.5 shadow-2xs transition-all cursor-pointer"
                        >
                          <RotateCcw className="w-3 h-3" />
                          <span>Inspect Error &rarr;</span>
                        </button>
                      ) : isPlanReview ? (
                        <button
                          onClick={(e) => {
                            e.stopPropagation();
                            setActiveDrawerTaskId(job.job_id);
                          }}
                          className="px-3 py-1 rounded-lg bg-indigo-600 hover:bg-indigo-700 text-white font-semibold text-xs flex items-center gap-1.5 shadow-2xs transition-all cursor-pointer"
                        >
                          <span>Review & Approve Plan &rarr;</span>
                        </button>
                      ) : isWaitingInput ? (
                        <button
                          onClick={(e) => {
                            e.stopPropagation();
                            setActiveDrawerTaskId(job.job_id);
                          }}
                          className="px-3 py-1 rounded-lg bg-amber-600 hover:bg-amber-700 text-white font-semibold text-xs flex items-center gap-1.5 shadow-2xs transition-all cursor-pointer"
                        >
                          <span>Provide Input &rarr;</span>
                        </button>
                      ) : isSucceeded && prLink ? (
                        <a
                          href={prLink}
                          target="_blank"
                          rel="noreferrer"
                          onClick={(e) => e.stopPropagation()}
                          className="px-3 py-1 rounded-lg bg-emerald-600 hover:bg-emerald-700 text-white font-semibold text-xs flex items-center gap-1.5 shadow-2xs transition-all cursor-pointer"
                        >
                          <GitPullRequest className="w-3 h-3 text-emerald-200" />
                          <span>View PR &rarr;</span>
                        </a>
                      ) : (
                        <span className="px-2.5 py-1 rounded-lg bg-sand-100 group-hover:bg-sand-200 text-stone-800 font-semibold text-xs transition-all flex items-center gap-1">
                          <span>Inspect</span>
                          <ChevronRight className="w-3.5 h-3.5" />
                        </span>
                      )}
                    </div>
                  </div>
                </div>
              );
            })}
          </div>
        )}
      </div>

      {/* 5. TASK COMPOSER MODAL / SHEET */}
      {showComposer && (
        <div className="fixed inset-0 bg-black/40 backdrop-blur-xs z-50 flex items-center justify-center p-4">
          <div className="bg-white border border-sand-200 rounded-3xl p-6 max-w-2xl w-full shadow-2xl space-y-4 max-h-[90vh] overflow-y-auto">
            <div className="flex items-center justify-between border-b border-sand-150 pb-3">
              <div className="flex items-center gap-2">
                <span className="text-xs font-mono font-bold bg-kiwi-100 text-kiwi-800 px-2 py-0.5 rounded">NEW TASK</span>
                <h2 className="text-base font-bold text-stone-900">Assign Work to AI Agent</h2>
              </div>
              <button onClick={() => setShowComposer(false)} className="text-stone-400 hover:text-stone-800 text-xs font-semibold">
                ✕ Close
              </button>
            </div>

            <form onSubmit={handleLaunch} className="space-y-4">
              <div>
                <label className="block text-xs font-bold text-stone-800 mb-1">Task Objective</label>
                <textarea
                  value={taskPrompt}
                  onChange={(e) => setTaskPrompt(e.target.value)}
                  placeholder="e.g. Refactor the JWT authentication middleware in pkg/auth to use Ed25519 asymmetric verification and add race-condition unit tests in jwt_test.go"
                  rows={4}
                  className="w-full p-3 rounded-xl border border-sand-300 focus:border-stone-900 text-xs font-sans text-stone-900 outline-none leading-relaxed"
                />
              </div>

              <div className="grid grid-cols-1 sm:grid-cols-2 gap-3 text-xs">
                <div>
                  <label className="block font-semibold text-stone-700 mb-1">Target Repository</label>
                  {repos.length > 0 ? (
                    <select
                      value={repoUrl}
                      onChange={(e) => setRepoUrl(e.target.value)}
                      className="w-full p-2.5 rounded-xl border border-sand-200 bg-sand-50 font-mono text-xs"
                    >
                      {repos.map((r) => {
                        const name = r.full_name || r.name || "repo";
                        const url = r.url || `https://github.com/${name}`;
                        return (
                          <option key={name} value={url}>
                            {name}
                          </option>
                        );
                      })}
                    </select>
                  ) : (
                    <a
                      href="/integrations"
                      className="block w-full p-2.5 rounded-xl border border-amber-200 bg-amber-50 text-amber-900 font-medium text-xs hover:bg-amber-100"
                    >
                      Connect GitHub to select a repository →
                    </a>
                  )}
                </div>
                <div>
                  <label className="block font-semibold text-stone-700 mb-1">Verification Guard</label>
                  <input
                    type="text"
                    value={testCmd}
                    onChange={(e) => setTestCmd(e.target.value)}
                    placeholder="e.g. npm test, pytest, go test ./... (auto-detected if blank)"
                    className="w-full p-2.5 rounded-xl border border-sand-200 bg-sand-50 font-mono text-xs"
                  />
                </div>
              </div>

              <ModelSelector
                architectModel={architectModel}
                workerModel={workerModel}
                onArchitectChange={setArchitectModel}
                onWorkerChange={setWorkerModel}
              />

              {submitError && (
                <div className="p-2.5 rounded-xl bg-rose-50 border border-rose-200 text-rose-800 text-xs">{submitError}</div>
              )}

              <div className="flex items-center justify-end gap-2 pt-2 border-t border-sand-150">
                <button
                  type="button"
                  onClick={() => setShowComposer(false)}
                  className="px-4 py-2 rounded-xl border border-sand-200 bg-white hover:bg-sand-100 text-xs font-semibold text-stone-700"
                >
                  Cancel
                </button>
                <button
                  type="submit"
                  disabled={isSubmitting || !taskPrompt.trim() || !repoUrl}
                  className="px-5 py-2 rounded-xl bg-stone-900 hover:bg-stone-800 disabled:opacity-50 text-white text-xs font-bold flex items-center gap-1.5 shadow-2xs"
                >
                  <Play className="w-3.5 h-3.5 fill-current text-kiwi-400" />
                  <span>Launch Task</span>
                </button>
              </div>
            </form>
          </div>
        </div>
      )}

      {/* TASK DRAWER */}
      <TaskDrawer
        taskId={activeDrawerTaskId}
        onClose={() => setActiveDrawerTaskId(null)}
      />
    </div>
  );
}

export default function DashboardPage() {
  return (
    <Suspense fallback={<div className="p-12 text-center flex flex-col items-center gap-3"><ThinkingOrb state="working" size={64} /><span className="text-xs text-stone-400 font-mono">Loading dashboard...</span></div>}>
      <CommandCenterContent />
    </Suspense>
  );
}
