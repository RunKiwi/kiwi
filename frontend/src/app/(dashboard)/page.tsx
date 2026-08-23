"use client";

import React, { useEffect, useState, useMemo, Suspense } from "react";
import Link from "next/link";
import { useRouter, useSearchParams } from "next/navigation";
import {
  FolderGit2,
  GitPullRequest,
  CheckCircle2,
  Server,
  Zap,
  Plus,
  Compass,
  Hammer,
  Check,
  Search,
  Sliders,
  Play,
  RotateCcw,
  Sparkles,
  ArrowRight,
  Activity,
  Layers,
  ShieldCheck,
  AlertCircle,
  Eye,
} from "lucide-react";
import { api, type JobSummary, type UsageResponse, type GithubRepo, type SpendResponse, type SandboxCacheStats } from "@/lib/api";
import { TaskDrawer } from "@/components/TaskDrawer";
import { ModelSelector } from "@/components/TaskComposer/ModelSelector";
import { ThinkingOrb } from "@/components/ThinkingOrb";
import { useFleetStore } from "@/store/useFleetStore";

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
  const router = useRouter();
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
  const [repoUrl, setRepoUrl] = useState("acme-corp/core-api");
  const [testCmd, setTestCmd] = useState("go test -race ./pkg/auth/...");
  const [architectModel, setArchitectModel] = useState("anthropic/claude-sonnet-5");
  const [workerModel, setWorkerModel] = useState("anthropic/claude-haiku-4.5");
  const [spendCap, setSpendCap] = useState(0.50);
  const [planMode, setPlanMode] = useState(false);
  const [dryRun, setDryRun] = useState(false);
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [submitError, setSubmitError] = useState<string | null>(null);

  // Status Filter
  const [statusFilter, setStatusFilter] = useState(searchParams.get("filter") || "all");

  useEffect(() => {
    loadJobs().catch(() => {}).finally(() => setLoading(false));
    loadDaemons().catch(() => {});

    api.getUsage().then(setUsage).catch(() => {});
    api.listGithubRepos().then((r) => setRepos(r.repos || [])).catch(() => {});
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
    } catch (err: any) {
      setSubmitError(err?.message || "Failed to submit task");
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
      
      {/* 1. ONBOARDING / PLAN BANNER WITH DYNAMIC UPGRADE CTA */}
      <div className="p-4 rounded-2xl border border-sand-200 bg-sand-50/80 flex flex-wrap items-center justify-between gap-3 text-xs shadow-2xs">
        <div className="flex items-center gap-3">
          <div className="w-8 h-8 rounded-xl bg-stone-900 text-white flex items-center justify-center font-bold shadow-2xs">
            ⚡
          </div>
          <div>
            <div className="flex items-center gap-2">
              <p className="font-bold text-stone-900 capitalize">{usage?.plan || "Free"} Tier Active ({limitMinutes} Mins Cap)</p>
              <span className="text-[9px] font-mono font-bold bg-amber-100 text-amber-800 px-1.5 py-0.2 rounded border border-amber-200 uppercase">
                {limitMinutes} MINS CAP
              </span>
            </div>
            <p className="text-stone-600 text-[11px]">
              {usedMinutes} / {limitMinutes} agent minutes used ({percentUsed}%) • {maxWorkers} concurrent workers • {usage?.plan === "enterprise" ? "BYOC Private Fleet" : "Standard Fleet"}
            </p>
          </div>
        </div>

        <div className="flex items-center gap-2">
          <Link
            href="/spend"
            className="px-3.5 py-1.5 rounded-xl bg-stone-900 hover:bg-stone-800 text-white font-semibold text-xs shadow-2xs flex items-center gap-1.5 transition-all"
          >
            <Zap className="w-3.5 h-3.5 text-kiwi-400 fill-current" />
            <span>Upgrade to Pro</span>
          </Link>
          <button
            onClick={() => setShowComposer(true)}
            className="px-3 py-1.5 rounded-xl bg-white hover:bg-sand-100 border border-sand-300 text-stone-800 font-semibold text-xs shadow-2xs transition-all"
          >
            + Assign Task
          </button>
        </div>
      </div>

      {/* 2. ACTIVE WORK & CAPACITY TILES */}
      <div className="space-y-4">
        <div className="flex items-center justify-between">
          <div>
            <h1 className="text-base font-bold text-stone-900 tracking-tight">Active Work & Capacity</h1>
            <p className="text-xs text-stone-500 mt-0.5">Real-time status of autonomous agents building code and passing automated tests.</p>
          </div>
          <button
            onClick={() => setShowComposer(true)}
            className="px-3.5 py-1.5 rounded-xl bg-stone-900 hover:bg-stone-800 text-white font-semibold text-xs shadow-2xs flex items-center gap-1.5 transition-all"
          >
            <Plus className="w-3.5 h-3.5 text-kiwi-400" />
            <span>New Task</span>
          </button>
        </div>

        <div className="grid grid-cols-2 sm:grid-cols-4 gap-3">
          <div className="p-4 rounded-2xl border border-sand-200 bg-sand-50/50 hover:bg-white transition-all shadow-2xs">
            <div className="flex items-center gap-1.5 text-stone-500 text-xs font-medium mb-1">
              <FolderGit2 className="w-3.5 h-3.5 text-stone-400" />
              <span>Connected Repos</span>
            </div>
            <p className="text-2xl font-bold text-stone-900 tracking-tight font-mono">{repos.length}</p>
          </div>

          <div className="p-4 rounded-2xl border border-sand-200 bg-sand-50/50 hover:bg-white transition-all shadow-2xs">
            <div className="flex items-center gap-1.5 text-stone-500 text-xs font-medium mb-1">
              <GitPullRequest className="w-3.5 h-3.5 text-stone-400" />
              <span>PRs Delivered</span>
            </div>
            <p className="text-2xl font-bold text-stone-900 tracking-tight font-mono">{prsDeliveredCount}</p>
          </div>

          <div className="p-4 rounded-2xl border border-sand-200 bg-sand-50/50 hover:bg-white transition-all shadow-2xs">
            <div className="flex items-center gap-1.5 text-stone-500 text-xs font-medium mb-1">
              <CheckCircle2 className="w-3.5 h-3.5 text-emerald-600" />
              <span>Verified Passed</span>
            </div>
            <p className="text-2xl font-bold text-stone-900 tracking-tight font-mono">{verifiedPassedCount}</p>
          </div>

          <div className="p-4 rounded-2xl border border-sand-200 bg-sand-50/50 hover:bg-white transition-all shadow-2xs">
            <div className="flex items-center gap-1.5 text-stone-500 text-xs font-medium mb-1">
              <Server className="w-3.5 h-3.5 text-stone-400" />
              <span>Private Runners</span>
            </div>
            <p className="text-2xl font-bold text-stone-900 tracking-tight font-mono">{privateRunnersCount}</p>
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
            <Link
              href="/spend"
              className="px-2.5 py-1 rounded-lg bg-amber-500 hover:bg-amber-600 text-white text-[11px] font-bold shadow-2xs transition-all flex items-center gap-1"
            >
              <Zap className="w-3 h-3 fill-current" />
              <span>Upgrade to Pro</span>
            </Link>
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
              onClick={() => setStatusFilter("waiting")}
              className={`px-2.5 py-1 rounded-lg font-semibold text-[11px] transition-all flex items-center gap-1 ${
                statusFilter === "waiting" ? "bg-amber-600 text-white shadow-2xs" : "text-amber-800 bg-amber-50 hover:bg-amber-100"
              }`}
            >
              <span className="w-1.5 h-1.5 rounded-full bg-amber-500 animate-pulse" />
              <span>Needs Input ({(jobs || []).filter((j) => j.status === "WAITING_USER").length})</span>
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
        {filteredJobs.length === 0 ? (
          <div className="p-8 rounded-2xl border border-sand-200 bg-sand-50/50 text-center space-y-3">
            <p className="text-xs text-stone-500">No tasks in execution queue for this filter.</p>
            <button
              onClick={() => setShowComposer(true)}
              className="px-4 py-2 rounded-xl bg-stone-900 hover:bg-stone-800 text-white font-semibold text-xs shadow-2xs"
            >
              + Create New Task
            </button>
          </div>
        ) : (
          <div className="space-y-3">
            {filteredJobs.map((job) => {
              const isPlanReview = job.status === "PLAN_REVIEW" || job.status === "AWAITING_PLAN_APPROVAL" || job.requires_plan_approval;
              const isWaitingInput = job.status === "WAITING_USER";
              const isRunning = job.status === "LEASED" || job.status === "RUNNING";
              const isSucceeded = job.status === "SUCCEEDED";

              return (
                <div
                  key={job.job_id}
                  onClick={() => setActiveDrawerTaskId(job.job_id)}
                  className={`p-4 rounded-2xl border transition-all cursor-pointer shadow-2xs group bg-white ${
                    isPlanReview
                      ? "border-sand-200 border-l-4 border-l-indigo-600 bg-gradient-to-r from-indigo-50/35 via-white to-white hover:border-indigo-300"
                      : isWaitingInput
                      ? "border-sand-200 border-l-4 border-l-amber-500 bg-gradient-to-r from-amber-50/35 via-white to-white hover:border-amber-300"
                      : isRunning
                      ? "border-sand-200 border-l-4 border-l-emerald-500 hover:border-emerald-300"
                      : "border-sand-200 hover:border-sand-300"
                  }`}
                >
                  {/* Header Row */}
                  <div className="flex items-center justify-between gap-2 mb-2">
                    <div className="flex items-center gap-2">
                      <span className="font-mono text-xs font-bold text-stone-900 bg-sand-100 px-2 py-0.5 rounded-md border border-sand-200">
                        #{job.job_id.slice(0, 8)}
                      </span>
                      <span className="text-xs font-mono text-stone-600">
                        {job.repo || "acme-corp/core-api"}
                      </span>
                    </div>

                    <div className="flex items-center gap-2">
                      <span
                        className={`px-2 py-0.5 rounded-md text-[10px] font-mono font-bold border ${
                          isPlanReview
                            ? "bg-indigo-50 text-indigo-800 border-indigo-200"
                            : isWaitingInput
                            ? "bg-amber-50 text-amber-800 border-amber-200"
                            : isRunning
                            ? "bg-emerald-50 text-emerald-800 border-emerald-200"
                            : isSucceeded
                            ? "bg-kiwi-50 text-kiwi-800 border-kiwi-200"
                            : "bg-rose-50 text-rose-700 border-rose-200"
                        }`}
                      >
                        {job.status}
                      </span>
                    </div>
                  </div>

                  {/* Title */}
                  <h3 className="text-xs font-bold text-stone-900 mb-2.5 leading-snug">
                    {job.task || "Autonomous execution task"}
                  </h3>

                  {/* 4-Stage Balanced Pipeline */}
                  <div className="grid grid-cols-4 gap-1.5 p-1 bg-sand-50 rounded-xl border border-sand-200 text-[10px] font-mono mb-3">
                    <div className={`px-2 py-1 rounded-lg border flex items-center gap-1.5 ${
                      isPlanReview || isWaitingInput || isRunning || isSucceeded
                        ? "bg-white border-sand-200 text-emerald-800 font-semibold"
                        : "text-stone-400 border-transparent"
                    }`}>
                      <span className={`w-1.5 h-1.5 rounded-full shrink-0 ${
                        isPlanReview || isWaitingInput || isRunning || isSucceeded ? "bg-emerald-500" : "bg-stone-300"
                      }`} />
                      <span>1. Plan ✓</span>
                    </div>

                    <div className={`px-2 py-1 rounded-lg border flex items-center gap-1.5 ${
                      isPlanReview
                        ? "bg-indigo-50 border-indigo-200 text-indigo-900 font-bold"
                        : isWaitingInput || isRunning || isSucceeded
                        ? "bg-white border-sand-200 text-emerald-800 font-semibold"
                        : "text-stone-400 border-transparent"
                    }`}>
                      <span className={`w-1.5 h-1.5 rounded-full shrink-0 ${
                        isPlanReview ? "bg-indigo-600 animate-ping" : isWaitingInput || isRunning || isSucceeded ? "bg-emerald-500" : "bg-stone-300"
                      }`} />
                      <span>2. Review</span>
                    </div>

                    <div className={`px-2 py-1 rounded-lg border flex items-center gap-1.5 ${
                      isRunning || isWaitingInput
                        ? "bg-emerald-50 border-emerald-200 text-emerald-900 font-bold"
                        : isSucceeded
                        ? "bg-white border-sand-200 text-emerald-800"
                        : "text-stone-400 border-transparent"
                    }`}>
                      <span className={`w-1.5 h-1.5 rounded-full shrink-0 ${
                        isRunning ? "bg-emerald-500 animate-pulse" : isSucceeded ? "bg-emerald-500" : "bg-stone-300"
                      }`} />
                      <span>3. Code & Test</span>
                    </div>

                    <div className={`px-2 py-1 rounded-lg border flex items-center gap-1.5 ${
                      isSucceeded
                        ? "bg-emerald-50 border-emerald-200 text-emerald-900 font-bold"
                        : "text-stone-400 border-transparent"
                    }`}>
                      <span className={`w-1.5 h-1.5 rounded-full shrink-0 ${
                        isSucceeded ? "bg-emerald-500" : "bg-stone-300"
                      }`} />
                      <span>4. PR Ready</span>
                    </div>
                  </div>

                  {/* Footer */}
                  <div className="flex items-center justify-between text-[11px] text-stone-500 font-mono pt-1">
                    <div className="flex items-center gap-2">
                      <span>{job.architect_model ? job.architect_model.split("/").pop() : "Claude Sonnet"} + {job.worker_model ? job.worker_model.split("/").pop() : "Claude Haiku"}</span>
                      <span>•</span>
                      <span className="text-kiwi-700 font-bold">{job.cost_usd ? `$${job.cost_usd.toFixed(2)}` : "$0.00"}</span>
                      {job.spend_cap_usd && (
                        <>
                          <span>•</span>
                          <span className="text-stone-400">${job.spend_cap_usd.toFixed(2)} cap</span>
                        </>
                      )}
                    </div>

                    <span className="text-stone-400 group-hover:text-stone-800 font-sans font-semibold text-xs flex items-center gap-1">
                      <span>{isPlanReview ? "Review Plan" : isWaitingInput ? "Provide Input" : "Inspect"}</span>
                      <span>&rarr;</span>
                    </span>
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
                  <input
                    type="text"
                    value={repoUrl}
                    onChange={(e) => setRepoUrl(e.target.value)}
                    className="w-full p-2.5 rounded-xl border border-sand-200 bg-sand-50 font-mono text-xs"
                  />
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

              <div className="flex items-center justify-between pt-2 border-t border-sand-150">
                <div className="flex items-center gap-2">
                  <span className="text-xs font-mono text-stone-500">Est: <strong className="text-kiwi-700">$0.18</strong></span>
                </div>

                <div className="flex items-center gap-2">
                  <button
                    type="button"
                    onClick={() => setShowComposer(false)}
                    className="px-4 py-2 rounded-xl border border-sand-200 bg-white hover:bg-sand-100 text-xs font-semibold text-stone-700"
                  >
                    Cancel
                  </button>
                  <button
                    type="submit"
                    disabled={isSubmitting || !taskPrompt.trim()}
                    className="px-5 py-2 rounded-xl bg-stone-900 hover:bg-stone-800 disabled:opacity-50 text-white text-xs font-bold flex items-center gap-1.5 shadow-2xs"
                  >
                    <Play className="w-3.5 h-3.5 fill-current text-kiwi-400" />
                    <span>Launch Task</span>
                  </button>
                </div>
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
