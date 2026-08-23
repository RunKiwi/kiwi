"use client";

import React, { useEffect, useState, useMemo, Suspense } from "react";
import { useSearchParams } from "next/navigation";
import {
  FolderGit2,
  GitPullRequest,
  CheckCircle2,
  Server,
  Plus,
  Play,
  Folder,
  Compass,
  Hammer,
  Ban,
  ChevronRight,
} from "lucide-react";
import { api, DEFAULT_WORKER_MODEL, type UsageResponse, type GithubRepo, type SpendResponse, type SandboxCacheStats } from "@/lib/api";
import { shortTime, formatCost, formatTokens } from "@/lib/datetime";
import { TaskDrawer } from "@/components/TaskDrawer";
import { ModelSelector } from "@/components/TaskComposer/ModelSelector";
import { ThinkingOrb } from "@/components/ThinkingOrb";
import { UpgradeButton } from "@/components/UpgradeButton";
import { useFleetStore } from "@/store/useFleetStore";
import { Logo } from "@/components/Logo";

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
  const [testCmd, setTestCmd] = useState("go test -race ./pkg/auth/...");
  const [architectModel, setArchitectModel] = useState("claude-sonnet-5");
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

  useEffect(() => {
    loadJobs().catch(() => {}).finally(() => setLoading(false));
    loadDaemons().catch(() => {});

    api.getUsage().then(setUsage).catch(() => {});
    api.listGithubRepos()
      .then((r) => {
        const list = r.repos || [];
        setRepos(list);
        setRepoUrl((prev) => prev || list[0]?.full_name || list[0]?.name || "");
      })
      .catch(() => {});
    api.getSpend().then(setSpend).catch(() => {});
    api.getSandboxCacheStats().then(setCacheStats).catch(() => {});
  }, [loadJobs, loadDaemons]);

  const handleLaunch = async (e?: React.FormEvent) => {
    if (e) e.preventDefault();
    if (!taskPrompt.trim()) return;

    setIsSubmitting(true);
    setSubmitError(null);
    try {
      await api.submitPlan({
        task: taskPrompt.trim(),
        repo_url: repoUrl,
        test_cmd: testCmd,
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

  const filteredJobs = useMemo(() => {
    const list = jobs || [];
    return list.filter((job) => {
      const isPlanReview = job.status === "PLAN_REVIEW" || job.status === "AWAITING_PLAN_APPROVAL" || job.requires_plan_approval;
      const isWaitingUser = job.status === "WAITING_USER";
      const isRunning = job.status === "LEASED" || job.status === "RUNNING";
      const isPrReady = job.status === "SUCCEEDED" || (job.pr_urls && job.pr_urls.length > 0);

      if (statusFilter === "plan") return isPlanReview;
      if (statusFilter === "waiting") return isWaitingUser;
      if (statusFilter === "running") return isRunning;
      if (statusFilter === "pr_created") return isPrReady;
      return true;
    });
  }, [jobs, statusFilter]);

  const usedMinutes = usage?.agent_minutes_used ?? 0;
  const limitMinutes = usage?.agent_minutes_limit ?? 500;
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
    <div className="p-6 space-y-7 max-w-6xl mx-auto font-sans text-stone-900">
      
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
              <p className="font-bold text-stone-900 capitalize text-sm">{usage?.plan || "Free"} Tier Active ({limitMinutes} Mins Cap)</p>
              <span className="text-[9px] font-mono font-bold bg-amber-100 text-amber-800 px-1.5 py-0.2 rounded border border-amber-200 uppercase">
                {limitMinutes} MINS CAP
              </span>
            </div>
            <p className="text-stone-600 text-[11px] mt-0.5">
              {usedMinutes.toFixed(1)} / {limitMinutes} agent minutes used ({percentUsed}%) • {maxWorkers} concurrent workers • {usage?.plan === "enterprise" ? "BYOC Private Fleet" : "Standard Fleet"}
            </p>
          </div>
        </div>

        <div className="relative z-10 flex items-center gap-2">
          <UpgradeButton variant="full" />
          <button
            onClick={() => setShowComposer(true)}
            className="px-3 py-1.5 rounded-xl bg-white hover:bg-sand-100 border border-sand-300 text-stone-800 font-semibold text-xs shadow-2xs transition-all cursor-pointer"
          >
            + Assign Task
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
            <UpgradeButton variant="compact" />
            <span className="px-2.5 py-1 rounded-lg bg-sand-150 text-stone-800 text-[11px] font-mono font-bold uppercase">
              PLAN: {usage?.plan || "FREE"} ({maxWorkers} WORKERS CAP)
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
      <div className="space-y-3.5">
        <div className="flex flex-wrap items-center justify-between gap-2">
          <div>
            <h2 className="text-sm font-bold text-stone-900">Task Execution Queue</h2>
            <p className="text-xs text-stone-500 mt-0.5">Click to inspect real-time progress, live logs, code diffs, and verification receipts.</p>
          </div>
          <div className="flex flex-wrap items-center gap-1 text-xs">
            <button
              onClick={() => setStatusFilter("all")}
              className={`px-2.5 py-1 rounded-lg font-semibold text-[11px] transition-all ${
                statusFilter === "all" ? "bg-stone-900 text-white shadow-2xs" : "text-stone-600 hover:bg-sand-150"
              }`}
            >
              All ({(jobs || []).length})
            </button>
            <button
              onClick={() => setStatusFilter("running")}
              className={`px-2.5 py-1 rounded-lg font-medium text-[11px] transition-all ${
                statusFilter === "running" ? "bg-stone-900 text-white shadow-2xs" : "text-stone-600 hover:bg-sand-150"
              }`}
            >
              Running ({(jobs || []).filter((j) => j.status === "LEASED" || j.status === "RUNNING").length})
            </button>
            <button
              onClick={() => setStatusFilter("plan")}
              className={`px-2.5 py-1 rounded-lg font-semibold text-[11px] transition-all flex items-center gap-1 border border-indigo-200/80 ${
                statusFilter === "plan" ? "bg-indigo-600 text-white shadow-2xs" : "text-indigo-900 bg-indigo-50 hover:bg-indigo-100"
              }`}
            >
              <span className="w-1.5 h-1.5 rounded-full bg-indigo-500 animate-pulse" />
              <span>📋 Plan Review ({(jobs || []).filter((j) => j.status === "PLAN_REVIEW" || j.status === "AWAITING_PLAN_APPROVAL" || j.requires_plan_approval).length})</span>
            </button>
            <button
              onClick={() => setStatusFilter("pr_created")}
              className={`px-2.5 py-1 rounded-lg font-medium text-[11px] transition-all ${
                statusFilter === "pr_created" ? "bg-stone-900 text-white shadow-2xs" : "text-stone-600 hover:bg-sand-150"
              }`}
            >
              PR Ready ({(jobs || []).filter((j) => j.status === "SUCCEEDED" || (j.pr_urls && j.pr_urls.length > 0)).length})
            </button>
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
              <p className="text-xs font-bold text-stone-800">Queue is quiet and resting</p>
              <p className="text-[11px] text-stone-500 max-w-sm mx-auto">
                No active tasks match this filter. Assign a new programming objective or run an automated refactor.
              </p>
            </div>
            <div className="relative z-10 pt-1">
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
              const isPlanReview = job.status === "PLAN_REVIEW" || job.status === "AWAITING_PLAN_APPROVAL" || job.requires_plan_approval || job.plan_status === "pending_review";
              const isWaitingInput = job.status === "WAITING_USER";
              const isRunning = job.status === "LEASED" || job.status === "RUNNING";
              const isSucceeded = job.status === "SUCCEEDED" || (job.pr_urls && job.pr_urls.length > 0);
              const isCancelled = job.status === "CANCELLED";
              const isFailed = job.status === "FAILED" || job.plan_status === "rejected";

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
              else if (isPlanReview || isWaitingInput) stage = 2;

              return (
                <div
                  key={job.job_id}
                  onClick={() => setActiveDrawerTaskId(job.job_id)}
                  className={`p-4 sm:p-5 rounded-2xl border transition-all cursor-pointer shadow-2xs group bg-white ${
                    isPlanReview
                      ? "border-indigo-200/80 bg-gradient-to-r from-indigo-50/20 via-white to-white hover:border-indigo-300 hover:shadow-xs"
                      : isWaitingInput
                      ? "border-amber-200/80 bg-gradient-to-r from-amber-50/20 via-white to-white hover:border-amber-300 hover:shadow-xs"
                      : isRunning
                      ? "border-sand-200 hover:border-emerald-300 hover:shadow-xs"
                      : "border-sand-200 hover:border-sand-300 hover:shadow-xs"
                  }`}
                >
                  {/* Header Row */}
                  <div className="flex flex-wrap items-center justify-between gap-2 mb-2.5">
                    <div className="flex items-center gap-2 flex-wrap">
                      <span
                        className={`font-mono text-xs font-bold px-2 py-0.5 rounded-md border shadow-2xs transition-colors ${
                          isPlanReview
                            ? "bg-indigo-50 text-indigo-900 border-indigo-200"
                            : isWaitingInput
                            ? "bg-amber-50 text-amber-900 border-amber-200"
                            : isRunning
                            ? "bg-emerald-50 text-emerald-900 border-emerald-200"
                            : isSucceeded
                            ? "bg-purple-50 text-purple-900 border-purple-200"
                            : isFailed
                            ? "bg-rose-50/80 text-rose-900 border-rose-200"
                            : isCancelled
                            ? "bg-sand-100 text-stone-500 border-sand-200"
                            : "bg-sand-100 text-stone-800 border-sand-200"
                        }`}
                      >
                        #{job.job_id.slice(0, 8)}
                      </span>
                      <span className="text-xs font-mono text-stone-600 flex items-center gap-1">
                        <Folder className="w-3.5 h-3.5 text-stone-400" />
                        <span>{job.repo || "RunKiwi/website"}</span>
                      </span>
                      {timeAgo && (
                        <span className="text-xs text-stone-400 font-mono">
                          • {timeAgo}
                        </span>
                      )}
                      {job.requires_plan_approval && (
                        <span className="px-1.5 py-0.2 rounded font-mono text-[9px] font-bold bg-indigo-50 text-indigo-800 border border-indigo-200">
                          PLAN MODE
                        </span>
                      )}
                      {job.is_dry_run && (
                        <span className="px-1.5 py-0.2 rounded font-mono text-[9px] font-bold bg-sky-50 text-sky-800 border border-sky-200">
                          DRY-RUN
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
                          className="px-2.5 py-0.5 rounded-full text-[10px] font-mono font-bold bg-purple-50 hover:bg-purple-100 text-purple-700 border border-purple-200 flex items-center gap-1 shadow-2xs transition-all"
                        >
                          <GitPullRequest className="w-3 h-3 text-purple-600" />
                          <span>#{job.pr_number || "PR"}</span>
                        </a>
                      )}

                      <span
                        className={`px-2.5 py-0.5 rounded-full text-[10px] font-mono font-bold border flex items-center gap-1.5 ${
                          isPlanReview
                            ? "bg-indigo-50 text-indigo-800 border-indigo-200"
                            : isWaitingInput
                            ? "bg-amber-50 text-amber-800 border-amber-200"
                            : isRunning
                            ? "bg-emerald-50 text-emerald-800 border-emerald-200"
                            : isSucceeded
                            ? "bg-purple-50 text-purple-800 border-purple-200"
                            : isCancelled
                            ? "bg-sand-100 text-stone-600 border-sand-200"
                            : isFailed
                            ? "bg-sand-100 text-stone-700 border-sand-200"
                            : "bg-amber-50 text-amber-800 border-amber-200"
                        }`}
                      >
                        {isRunning && (
                          <span className="relative flex h-2 w-2">
                            <span className="animate-ping absolute inline-flex h-full w-full rounded-full bg-emerald-400 opacity-75" />
                            <span className="relative inline-flex rounded-full h-2 w-2 bg-emerald-500" />
                          </span>
                        )}
                        {isPlanReview && (
                          <span className="relative flex h-2 w-2">
                            <span className="animate-ping absolute inline-flex h-full w-full rounded-full bg-indigo-400 opacity-75" />
                            <span className="relative inline-flex rounded-full h-2 w-2 bg-indigo-600" />
                          </span>
                        )}
                        {isWaitingInput && (
                          <span className="relative flex h-2 w-2">
                            <span className="animate-ping absolute inline-flex h-full w-full rounded-full bg-amber-400 opacity-75" />
                            <span className="relative inline-flex rounded-full h-2 w-2 bg-amber-600" />
                          </span>
                        )}
                        {isCancelled && <Ban className="w-3 h-3 text-stone-400" />}
                        {isFailed && <span className="w-1.5 h-1.5 rounded-full bg-rose-500 shrink-0" />}
                        {isSucceeded && <CheckCircle2 className="w-3 h-3 text-purple-600" />}
                        <span>
                          {isPlanReview
                            ? "ACTION: PLAN READY"
                            : isWaitingInput
                            ? "ACTION: INPUT NEEDED"
                            : isRunning
                            ? "RUNNING"
                            : isSucceeded
                            ? "PR READY"
                            : isCancelled
                            ? "CANCELLED"
                            : isFailed
                            ? "FAILED"
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
                  {isPlanReview && (
                    <div className="mb-3 p-2.5 rounded-xl bg-indigo-50/90 border border-indigo-200 flex items-center justify-between text-xs text-indigo-950 shadow-2xs">
                      <span className="flex items-center gap-2">
                        <span className="w-2 h-2 rounded-full bg-indigo-600 shrink-0" />
                        <span className="text-[11px]">
                          <strong>Architect execution plan ready:</strong> Requires your review & sign-off before code execution.
                        </span>
                      </span>
                      <span className="text-[10px] font-mono font-bold text-indigo-800 bg-white px-2 py-0.5 rounded-md border border-indigo-200 shrink-0">
                        Awaiting Sign-off
                      </span>
                    </div>
                  )}

                  {isWaitingInput && (
                    <div className="mb-3 p-2.5 rounded-xl bg-amber-50/90 border border-amber-200 flex items-center justify-between text-xs text-amber-950 shadow-2xs">
                      <span className="flex items-center gap-2">
                        <span className="w-2 h-2 rounded-full bg-amber-600 shrink-0" />
                        <span className="text-[11px]">
                          <strong>Worker paused for clarification:</strong> Human confirmation needed to proceed.
                        </span>
                      </span>
                      <span className="text-[10px] font-mono font-bold text-amber-800 bg-white px-2 py-0.5 rounded-md border border-amber-200 shrink-0">
                        Input Required
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
                            ? "bg-indigo-50 border-indigo-200 text-indigo-900 font-bold"
                            : "bg-white border-sand-200 text-stone-800 font-semibold"
                          : "text-stone-400 border-transparent"
                      }`}
                    >
                      <div className="flex items-center gap-1.5 min-w-0">
                        <span
                          className={`w-1.5 h-1.5 rounded-full shrink-0 ${
                            stage >= 1 ? "bg-emerald-500" : "bg-stone-300"
                          }`}
                        />
                        <span className="truncate">1. Plan</span>
                      </div>
                      <span className="text-[10px] shrink-0 font-bold">
                        {stage > 1 || isSucceeded || isFailed ? (
                          <span className="text-emerald-600">✓</span>
                        ) : isRunning && stage === 1 ? (
                          <span className="text-indigo-600">⟳</span>
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
                          : stage >= 2
                          ? isRunning && stage === 2
                            ? "bg-indigo-50 border-indigo-200 text-indigo-900 font-bold"
                            : "bg-white border-sand-200 text-stone-800 font-semibold"
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
                              : stage > 2 || isSucceeded
                              ? "bg-emerald-500"
                              : stage === 2
                              ? "bg-indigo-500"
                              : "bg-stone-300"
                          }`}
                        />
                        <span className="truncate">
                          {job.requires_plan_approval ? "2. Review" : "2. Env Prep"}
                        </span>
                      </div>
                      <span className="text-[10px] shrink-0 font-bold">
                        {isFailed && job.plan_status === "rejected" ? (
                          <span className="text-rose-600">✕</span>
                        ) : isPlanReview ? (
                          <span className="text-indigo-600 text-[9px] font-bold">Action</span>
                        ) : stage > 2 || isSucceeded || (isFailed && job.plan_status !== "rejected") ? (
                          <span className="text-emerald-600">✓</span>
                        ) : isRunning && stage === 2 ? (
                          <span className="text-indigo-600">⟳</span>
                        ) : (
                          <span className="text-stone-300 font-normal">—</span>
                        )}
                      </span>
                    </div>

                    {/* 3. Code & Test */}
                    <div
                      className={`px-2 py-1.5 rounded-lg border flex items-center justify-between transition-colors ${
                        isWaitingInput
                          ? "bg-amber-100 border-amber-300 text-amber-950 font-bold shadow-2xs"
                          : isFailed && job.plan_status !== "rejected"
                          ? "bg-rose-50 border-rose-200 text-rose-800 font-bold"
                          : isRunning && stage === 3
                          ? "bg-emerald-50 border-emerald-300 text-emerald-950 font-bold animate-pulse"
                          : stage >= 3
                          ? isSucceeded
                            ? "bg-white border-sand-200 text-stone-800 font-semibold"
                            : "bg-white border-sand-200 text-stone-800 font-semibold"
                          : isCancelled
                          ? "bg-sand-100/60 border-sand-200 text-stone-500"
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
                              ? "bg-emerald-600"
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
                          <span className="text-rose-600">✕</span>
                        ) : isWaitingInput ? (
                          <span className="text-amber-700 text-[9px]">Input</span>
                        ) : isSucceeded ? (
                          <span className="text-emerald-600">✓</span>
                        ) : isRunning && stage === 3 ? (
                          <span className="text-emerald-600">⟳</span>
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
                          ? "bg-purple-50 border-purple-200 text-purple-900 font-bold"
                          : "text-stone-400 border-transparent"
                      }`}
                    >
                      <div className="flex items-center gap-1.5 min-w-0">
                        <span
                          className={`w-1.5 h-1.5 rounded-full shrink-0 ${
                            isSucceeded ? "bg-purple-600" : "bg-stone-300"
                          }`}
                        />
                        <span className="truncate">{job.is_dry_run ? "4. Dry-Run" : "4. PR Ready"}</span>
                      </div>
                      <span className="text-[10px] shrink-0 font-bold">
                        {isSucceeded ? (
                          <span className="text-purple-700">✓</span>
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
                      {isPlanReview ? (
                        <button
                          onClick={(e) => {
                            e.stopPropagation();
                            setActiveDrawerTaskId(job.job_id);
                          }}
                          className="px-3 py-1 rounded-xl bg-indigo-600 hover:bg-indigo-700 text-white font-sans font-bold text-xs flex items-center gap-1.5 shadow-2xs transition-all"
                        >
                          <span>Review & Approve Plan &rarr;</span>
                        </button>
                      ) : isWaitingInput ? (
                        <button
                          onClick={(e) => {
                            e.stopPropagation();
                            setActiveDrawerTaskId(job.job_id);
                          }}
                          className="px-3 py-1 rounded-xl bg-amber-600 hover:bg-amber-700 text-white font-sans font-bold text-xs flex items-center gap-1.5 shadow-2xs transition-all"
                        >
                          <span>Provide Input &rarr;</span>
                        </button>
                      ) : isSucceeded && prLink ? (
                        <a
                          href={prLink}
                          target="_blank"
                          rel="noreferrer"
                          onClick={(e) => e.stopPropagation()}
                          className="px-3 py-1 rounded-xl bg-purple-600 hover:bg-purple-700 text-white font-sans font-bold text-xs flex items-center gap-1.5 shadow-2xs transition-all"
                        >
                          <GitPullRequest className="w-3 h-3 text-purple-200" />
                          <span>View PR &rarr;</span>
                        </a>
                      ) : (
                        <span className="text-stone-400 group-hover:text-stone-900 font-sans font-semibold text-xs flex items-center gap-1 transition-colors">
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
                        return (
                          <option key={name} value={name}>
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
