"use client";

import React, { useState, useEffect, Suspense } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import {
  ShieldCheck,
  Play,
  Sliders,
  FolderGit2,
  Zap,
} from "lucide-react";
import {
  api,
  DEFAULT_WORKER_MODEL,
  formatTokens,
  modelClassLabel,
  CLASS_ORDER,
  type GithubRepo,
  type SpendResponse,
} from "@/lib/api";
import { KiwiMicroButtonLoader } from "@/components/KiwiLoaders";
import { ModelSelector } from "@/components/TaskComposer/ModelSelector";
import { Logo } from "@/components/Logo";
import { Select } from "@/components/Select";

function ComposerContent() {
  const router = useRouter();
  const searchParams = useSearchParams();

  const targetRepoParam = searchParams.get("repo") || searchParams.get("repo_url");
  const initialTask = searchParams.get("task") || searchParams.get("prompt") || "";
  const initialBranch = searchParams.get("branch") || searchParams.get("ref") || "";
  const initialTestCmd = searchParams.get("test_cmd") || "";
  const initialStrategy =
    searchParams.get("strategy") === "plan" || searchParams.get("plan_mode") === "true"
      ? "plan"
      : "direct";
  const initialSpendCap =
    searchParams.get("spend_cap") || searchParams.get("spend_cap_usd")
      ? parseFloat(searchParams.get("spend_cap") || searchParams.get("spend_cap_usd") || "0.5")
      : 0.50;
  const initialMode =
    searchParams.get("mode") === "dryrun" || searchParams.get("dry_run") === "true"
      ? "dryrun"
      : "pr";
  const initialArchitect = searchParams.get("architect_model") || "claude-sonnet-5";
  const initialWorker = searchParams.get("worker_model") || searchParams.get("model") || DEFAULT_WORKER_MODEL;
  const sourceJobId = searchParams.get("job_id");

  const [prompt, setPrompt] = useState(initialTask);
  const [repos, setRepos] = useState<GithubRepo[]>([]);
  const [reposLoaded, setReposLoaded] = useState(false);
  const [repo, setRepo] = useState(targetRepoParam || "");
  const [branch, setBranch] = useState(initialBranch);
  const [testCmd, setTestCmd] = useState(initialTestCmd);
  const [strategy, setStrategy] = useState<"direct" | "plan">(initialStrategy);
  const [spendCap, setSpendCap] = useState(initialSpendCap);
  const [mode, setMode] = useState<"pr" | "dryrun">(initialMode);
  const [architectModel, setArchitectModel] = useState(initialArchitect);
  const [workerModel, setWorkerModel] = useState(initialWorker);
  const [spend, setSpend] = useState<SpendResponse | null>(null);
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [submitError, setSubmitError] = useState<string | null>(null);

  // Helper to match repository URL from repo name, short name or full name
  const matchRepo = (target: string, list: GithubRepo[]): string => {
    if (!target) return "";
    const clean = target.trim();
    const normClean = clean.toLowerCase().replace(/\.git$/, "").replace(/^https?:\/\/github\.com\//, "").replace(/^git@github\.com:/, "");

    const match = list.find((item) => {
      const full = (item.full_name || "").toLowerCase().replace(/\.git$/, "");
      const name = (item.name || "").toLowerCase();
      const normUrl = (item.url || "").toLowerCase().replace(/\.git$/, "").replace(/^https?:\/\/github\.com\//, "").replace(/^git@github\.com:/, "");

      return (
        normUrl === normClean ||
        full === normClean ||
        name === normClean ||
        normClean.endsWith("/" + name) ||
        full.endsWith("/" + normClean) ||
        normUrl.endsWith("/" + normClean) ||
        (name.length > 0 && (normClean.includes(name) || full.includes(normClean)))
      );
    });
    if (match) {
      return match.url || `https://github.com/${match.full_name || match.name}`;
    }
    return clean.includes("://") || clean.includes("@") ? clean : `https://github.com/${clean}`;
  };

  // If a source job_id is passed, fetch its full details to prefill any missing fields
  useEffect(() => {
    if (!sourceJobId) return;
    Promise.all([
      api.getJob(sourceJobId).catch(() => null),
      api.getJobPlan(sourceJobId).catch(() => null),
    ]).then(([job, plan]) => {
      if (job) {
        if (job.task) setPrompt((p) => p || job.task || "");
        const arch = job.architect_model || plan?.architect_model || job.tasks?.[0]?.architect_model;
        if (arch) setArchitectModel(arch);
        const worker = job.worker_model || job.tasks?.[0]?.model;
        if (worker) setWorkerModel(worker);
        if (job.spend_cap_usd != null) setSpendCap(job.spend_cap_usd);
        if (job.is_dry_run) setMode("dryrun");
        if (job.requires_plan_approval || job.plan_status) setStrategy("plan");
        if (job.repo) {
          setRepo(matchRepo(job.repo, repos));
        }
      }
    });
  }, [sourceJobId, repos]);

  useEffect(() => {
    api.getSpend().then(setSpend).catch(() => {});
    api.listGithubRepos()
      .then((r) => {
        const list = r.repos || [];
        setRepos(list);
        const target = targetRepoParam;
        if (target) {
          setRepo(matchRepo(target, list));
        } else if (list.length > 0 && !sourceJobId) {
          const first = list[0];
          setRepo((prev) => prev || first.url || `https://github.com/${first.full_name || first.name}`);
        }
      })
      .catch(() => {})
      .finally(() => setReposLoaded(true));
  }, [targetRepoParam, sourceJobId]);

  const handleStart = async () => {
    if (!prompt.trim() || !repo) return;
    setIsSubmitting(true);
    setSubmitError(null);

    const selectedRepo = repos.find(
      (r) =>
        r.url === repo ||
        (r.full_name && r.full_name === repo) ||
        (r.name && r.name === repo)
    );
    const repoUrl =
      selectedRepo?.url ||
      (repo.includes("://") || repo.includes("@")
        ? repo
        : `https://github.com/${repo}`);

    try {
      await api.submitPlan({
        task: prompt.trim(),
        repo_url: repoUrl,
        ref: branch.trim() || undefined,
        test_cmd: testCmd.trim() || undefined,
        architect_model: architectModel,
        model: workerModel,
        plan_mode: strategy === "plan",
        spend_cap_usd: spendCap,
        dry_run: mode === "dryrun",
      });
      router.push("/");
    } catch (err) {
      setSubmitError(err instanceof Error ? err.message : "Failed to submit task");
    } finally {
      setIsSubmitting(false);
    }
  };

  return (
    <div className="space-y-6 max-w-4xl mx-auto font-sans text-stone-900">
      {/* Header with Hybrid Light Aura & Animated Hacking Mascot */}
      <div className="relative overflow-hidden p-6 rounded-3xl border border-sand-200 bg-gradient-to-r from-sand-100/90 via-white to-kiwi-50/70 backdrop-blur-xl flex flex-wrap items-center justify-between gap-4 shadow-2xs group">
        <div
          className="absolute inset-0 opacity-[0.035] pointer-events-none"
          style={{
            backgroundImage: `url("data:image/svg+xml,%3Csvg viewBox='0 0 200 200' xmlns='http://www.w3.org/2000/svg'%3E%3Cfilter id='n'%3E%3CfeTurbulence type='fractalNoise' baseFrequency='0.8' numOctaves='3'/%3E%3C/filter%3E%3Crect width='100%25' height='100%25' filter='url(%23n)'/%3E%3C/svg%3E")`,
          }}
        />
        <div className="absolute -top-12 -right-12 w-36 h-36 bg-kiwi-400/20 rounded-full blur-3xl group-hover:scale-110 transition-transform" />

        <div className="relative z-10 flex items-center gap-3.5">
          <div className="w-12 h-12 rounded-2xl bg-white border border-sand-200/90 shadow-2xs flex items-center justify-center shrink-0">
            <Logo variant="full-color" pose="hacking" animated={true} className="w-8 h-8" />
          </div>
          <div>
            <div className="flex items-center gap-2">
              <span className="text-[10px] font-mono font-bold bg-kiwi-100 text-kiwi-900 px-2 py-0.5 rounded-full border border-kiwi-200">TASK COMPOSER</span>
              <h1 className="text-lg font-bold text-stone-900 tracking-tight">Assign Work to AI Agent</h1>
            </div>
            <p className="text-xs text-stone-600 mt-0.5">
              Kiwi writes the code, verifies tests in an isolated container, and opens a ready-to-merge Pull Request.
            </p>
          </div>
        </div>

        <button onClick={() => setPrompt("")} className="relative z-10 text-xs text-stone-400 hover:text-stone-700 font-medium cursor-pointer">
          Reset Form
        </button>
      </div>

      {/* Platform Token Quota Health Strip */}
      {spend?.allowance && spend.allowance.length > 0 && (
        <div className="p-3.5 rounded-2xl bg-white/90 backdrop-blur-xl border border-sand-200 shadow-2xs flex flex-wrap items-center justify-between gap-3 text-xs">
          <div className="flex items-center gap-2">
            <Zap className="w-4 h-4 text-kiwi-600" />
            <span className="font-bold text-stone-900">Kiwi Platform Quota:</span>
          </div>

          <div className="flex flex-wrap items-center gap-2">
            {CLASS_ORDER.map((tierKey) => {
              const a = spend.allowance?.find((x) => x.tier === tierKey);
              if (!a) return null;
              const unlimited = a.granted < 0;
              const exhausted = !unlimited && (a.remaining <= 0 || a.used >= a.granted);

              return (
                <div
                  key={a.tier}
                  className={`px-2.5 py-1 rounded-xl border text-[11px] font-mono flex items-center gap-1.5 ${
                    exhausted
                      ? "bg-rose-50 border-rose-200 text-rose-800 font-bold"
                      : "bg-sand-50/80 border-sand-200 text-stone-700"
                  }`}
                >
                  <span
                    className={`w-1.5 h-1.5 rounded-full ${
                      exhausted
                        ? "bg-rose-500"
                        : a.tier === "frontier"
                        ? "bg-amber-500"
                        : a.tier === "economy"
                        ? "bg-emerald-500"
                        : "bg-sky-500"
                    }`}
                  />
                  <span className="font-sans font-semibold text-stone-900">{modelClassLabel(a.tier)}:</span>
                  <span>{unlimited ? "Unlimited" : exhausted ? "⛔ Exhausted" : `${formatTokens(a.remaining)} left`}</span>
                </div>
              );
            })}
          </div>

          <a href="/models" className="text-[11px] text-kiwi-700 font-semibold hover:underline">
            View All Models &rarr;
          </a>
        </div>
      )}

      {/* Task Prompt Box */}
      <div className="relative p-6 rounded-3xl border border-sand-200 bg-white/90 backdrop-blur-xl shadow-2xs space-y-4 group">
        <div className="absolute inset-0 rounded-3xl overflow-hidden pointer-events-none">
          <div
            className="absolute inset-0 opacity-[0.03]"
            style={{
              backgroundImage: `url("data:image/svg+xml,%3Csvg viewBox='0 0 200 200' xmlns='http://www.w3.org/2000/svg'%3E%3Cfilter id='n'%3E%3CfeTurbulence type='fractalNoise' baseFrequency='0.8' numOctaves='3'/%3E%3C/filter%3E%3Crect width='100%25' height='100%25' filter='url(%23n)'/%3E%3C/svg%3E")`,
            }}
          />
        </div>
        <div className="relative z-10">
          <div className="flex items-center justify-between mb-1.5">
            <label className="text-xs font-bold text-stone-800">Task Objective</label>
            <span className="text-xs text-kiwi-700 hover:underline font-medium cursor-pointer">
              Import from GitHub Issue
            </span>
          </div>
          <div className="rounded-xl border border-sand-300 focus-within:border-kiwi-500 focus-within:ring-2 focus-within:ring-kiwi-100 bg-white transition-all shadow-sm">
            <textarea
              rows={4}
              value={prompt}
              onChange={(e) => setPrompt(e.target.value)}
              placeholder="e.g. Refactor the JWT authentication middleware in pkg/auth to use Ed25519 asymmetric verification and add race-condition unit tests in jwt_test.go"
              className="w-full p-4 text-sm text-stone-900 placeholder-stone-400 font-sans resize-none outline-none bg-transparent leading-relaxed"
            />
            <div className="p-2.5 bg-sand-50/80 border-t border-sand-150 flex flex-wrap items-center justify-between gap-2 text-xs">
              <span className="text-[11px] text-stone-500">
                ✨ Tip: Mention files like <code className="font-mono text-stone-800 bg-sand-200/60 px-1 py-0.5 rounded">pkg/auth/jwt.go</code> for faster pinpoint changes
              </span>
              <span className="text-[11px] font-mono text-emerald-700 font-semibold flex items-center gap-1">
                <ShieldCheck className="w-3.5 h-3.5" />
                Test command runs offline, with no network access
              </span>
            </div>
          </div>
        </div>

        {/* Configuration Grid 1: Target Repository & Test Guard */}
        {reposLoaded && repos.length === 0 ? (
          <div className="p-4 rounded-2xl border border-amber-200 bg-amber-50/60 text-xs text-amber-900 flex items-center justify-between gap-3">
            <span>No repositories connected yet — connect GitHub to assign a task.</span>
            <a href="/integrations" className="px-3 py-1.5 rounded-xl bg-stone-900 hover:bg-stone-800 text-white font-semibold shrink-0">
              Connect GitHub
            </a>
          </div>
        ) : (
          <div className="grid grid-cols-1 sm:grid-cols-2 gap-4 text-xs">
            <div>
              <label className="block font-semibold text-stone-700 mb-1">Target Repository &amp; Branch</label>
              <div className="flex items-center gap-2">
                <div className="flex-1 min-w-0">
                  <Select
                    value={repo}
                    onChange={setRepo}
                    options={repos.map((r) => {
                      const repoName = r.full_name || r.name || "repo";
                      const repoUrl = r.url || `https://github.com/${repoName}`;
                      return {
                        value: repoUrl,
                        label: repoName,
                        hint: r.private ? "private" : "public",
                      };
                    })}
                    searchable
                    placeholder="Select repository…"
                    icon={<FolderGit2 className="w-4 h-4 text-stone-500 shrink-0" />}
                  />
                </div>
                <input
                  type="text"
                  value={branch}
                  onChange={(e) => setBranch(e.target.value)}
                  placeholder="main"
                  title="Branch (defaults to the repo's default branch)"
                  className="w-24 px-3 py-2.5 rounded-xl bg-sand-50/90 hover:bg-white focus:bg-white border border-sand-200 text-stone-900 font-mono text-xs outline-none focus:border-kiwi-500 transition-all font-medium shrink-0 shadow-xs"
                />
              </div>
            </div>

            <div>
              <label className="block font-semibold text-stone-700 mb-1 flex items-center justify-between">
                <span>Automated Verification Guard</span>
                <span className="text-stone-400 font-normal">Must pass 100%</span>
              </label>
              <input
                type="text"
                value={testCmd}
                onChange={(e) => setTestCmd(e.target.value)}
                placeholder="e.g. npm test, pytest, go test ./... (auto-detected if blank)"
                className="w-full px-3.5 py-2.5 rounded-xl bg-sand-50/90 hover:bg-white focus:bg-white border border-sand-200 text-stone-900 font-mono text-xs outline-none focus:border-kiwi-500 transition-all font-medium shadow-xs"
              />
            </div>
          </div>
        )}

        {/* Configuration Grid 2: TWO DEDICATED MODEL SELECTORS */}
        <ModelSelector
          architectModel={architectModel}
          workerModel={workerModel}
          onArchitectChange={setArchitectModel}
          onWorkerChange={setWorkerModel}
        />

        {/* Configuration Grid 3: Execution Strategy, Spend Cap, Timeout & Mode */}
        <div className="relative z-10 p-4 rounded-2xl bg-white border border-sand-200 shadow-2xs space-y-3">
          <div className="flex items-center justify-between border-b border-sand-150 pb-2">
            <div className="flex items-center gap-2">
              <Sliders className="w-4 h-4 text-kiwi-700" />
              <span className="text-xs font-bold text-stone-900">Execution Strategy & Safety Guardrails</span>
            </div>
            <span className="text-[11px] text-stone-500 font-mono">Plan First vs Direct Loop</span>
          </div>

          <div className="grid grid-cols-1 sm:grid-cols-3 gap-4 text-xs">
            {/* Strategy */}
            <div>
              <label className="block font-semibold text-stone-700 mb-1 flex items-center justify-between">
                <span>Execution Strategy</span>
                <span className="text-[9px] font-mono text-indigo-700 font-bold bg-indigo-50 px-1 rounded">Interactive</span>
              </label>
              <div className="space-y-1">
                <label className="flex items-center gap-1.5 cursor-pointer text-stone-800 text-[11px] font-medium p-1.5 rounded-lg hover:bg-sand-50">
                  <input type="radio" name="strategy" checked={strategy === "direct"} onChange={() => setStrategy("direct")} className="accent-kiwi-600" />
                  <span>⚡ Direct Execution</span>
                </label>
                <label className="flex items-center gap-1.5 cursor-pointer text-indigo-900 text-[11px] font-bold p-1.5 rounded-lg bg-indigo-50/70 border border-indigo-200/80">
                  <input type="radio" name="strategy" checked={strategy === "plan"} onChange={() => setStrategy("plan")} className="accent-indigo-600" />
                  <span>📋 Plan Mode (Approve)</span>
                </label>
              </div>
            </div>

            {/* Spend Cap */}
            <div>
              <label className="block font-semibold text-stone-700 mb-1">Max Spend Hard-Cap</label>
              <div className="flex items-center gap-1">
                {[0.50, 1.00, 2.50].map((cap) => (
                  <button
                    key={cap}
                    type="button"
                    onClick={() => setSpendCap(cap)}
                    className={`px-2.5 py-1.5 rounded-lg font-mono font-bold text-[11px] transition-all ${
                      spendCap === cap ? "bg-stone-900 text-white" : "bg-sand-100 hover:bg-sand-200 text-stone-700"
                    }`}
                  >
                    ${cap.toFixed(2)}
                  </button>
                ))}
              </div>
              <p className="text-[10px] text-stone-400 mt-1 font-mono">Pauses safely if reached</p>
            </div>

            {/* Target Action */}
            <div>
              <label className="block font-semibold text-stone-700 mb-1">Target Action</label>
              <div className="space-y-1">
                <label className="flex items-center gap-1.5 cursor-pointer text-stone-800 text-[11px] font-medium p-1 rounded hover:bg-sand-50">
                  <input type="radio" name="mode" checked={mode === "pr"} onChange={() => setMode("pr")} className="accent-kiwi-600" />
                  <span>Open GitHub PR</span>
                </label>
                <label className="flex items-center gap-1.5 cursor-pointer text-stone-600 text-[11px] font-medium p-1 rounded hover:bg-sand-50">
                  <input type="radio" name="mode" checked={mode === "dryrun"} onChange={() => setMode("dryrun")} className="accent-kiwi-600" />
                  <span>🧪 Dry-Run (No Push)</span>
                </label>
              </div>
            </div>
          </div>
        </div>

        {/* Pre-Flight Bar & Start Button */}
        <div className="relative z-10 pt-3 border-t border-sand-200 space-y-3">
          {submitError && (
            <div className="p-2.5 rounded-xl bg-rose-50 border border-rose-200 text-rose-800 text-xs flex items-center gap-2">
              <span>{submitError}</span>
            </div>
          )}
          <div className="flex flex-wrap items-center justify-between gap-4">
            <div className="flex items-center gap-3 text-xs text-stone-500 font-mono">
              <span>Mode: <strong className="text-indigo-700 font-bold">{strategy === "plan" ? "Plan Mode" : "Direct Autonomous"}</strong></span>
              <span>•</span>
              <span>Cap: <strong className="text-stone-900 font-bold">${spendCap.toFixed(2)} USD</strong></span>
              <span>•</span>
              <span>Target: <strong className="text-stone-900">{mode === "pr" ? "GitHub Pull Request" : "Dry-Run Local"}</strong></span>
            </div>

            <button
              onClick={handleStart}
              disabled={isSubmitting || !prompt.trim() || !repo}
              className="px-6 py-2.5 rounded-xl bg-charcoal-900 hover:bg-charcoal-800 text-white font-bold text-xs shadow-sm flex items-center gap-2 transition-all active:scale-[0.98] disabled:opacity-50"
            >
              {isSubmitting ? <KiwiMicroButtonLoader /> : <Play className="w-4 h-4 text-kiwi-400 fill-current" />}
              <span>Start Task</span>
            </button>
          </div>
        </div>
      </div>
    </div>
  );
}

export default function ComposerPage() {
  return (
    <Suspense fallback={<div className="p-6 text-center text-xs text-stone-400 font-mono">Loading composer...</div>}>
      <ComposerContent />
    </Suspense>
  );
}

