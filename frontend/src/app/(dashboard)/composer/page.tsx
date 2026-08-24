"use client";

import React, { useState, useEffect, Suspense } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import {
  ShieldCheck,
  Play,
  Sliders,
  FolderGit2,
  Zap,
  Sparkles,
  Bug,
  TestTube2,
  Cpu,
  GitPullRequest,
  CheckCircle2,
  Lock,
  Compass,
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

const PRESET_GOALS = [
  {
    id: "bug",
    icon: <Bug className="w-3.5 h-3.5 text-rose-600" />,
    label: "Fix Bug / Error",
    template: "Fix the bug in [FILE_PATH] where [SYMPTOM_OR_ERROR_MESSAGE]. Ensure edge cases are handled and add a regression test.",
  },
  {
    id: "tests",
    icon: <TestTube2 className="w-3.5 h-3.5 text-emerald-600" />,
    label: "Add Unit Tests",
    template: "Write comprehensive unit tests for [PACKAGE_OR_FILE] covering happy path, invalid inputs, and error handling with full test assertions.",
  },
  {
    id: "refactor",
    icon: <Cpu className="w-3.5 h-3.5 text-sky-600" />,
    label: "Refactor Code",
    template: "Refactor [FUNCTION_OR_MODULE] in [FILE_PATH] to improve readability and performance without changing existing external API behavior.",
  },
  {
    id: "feature",
    icon: <Sparkles className="w-3.5 h-3.5 text-amber-600" />,
    label: "Add Feature",
    template: "Implement [FEATURE_NAME] in [FILE_PATH]. Include input validation, error handling, and unit tests verifying the feature.",
  },
  {
    id: "security",
    icon: <ShieldCheck className="w-3.5 h-3.5 text-indigo-600" />,
    label: "Security Hardening",
    template: "Audit and harden [ENDPOINT_OR_HANDLER] against unauthorized access, sanitize inputs, and prevent potential injection vulnerabilities.",
  },
];

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
    <div className="space-y-4 max-w-4xl mx-auto font-sans text-stone-900 select-none">
      
      {/* Header Banner with Modern Swiss Aesthetics */}
      <div className="p-4 sm:p-5 rounded-2xl border border-sand-200/90 bg-white shadow-2xs flex flex-col sm:flex-row items-start sm:items-center justify-between gap-4">
        <div className="flex items-center gap-3.5">
          <div className="w-12 h-12 rounded-2xl bg-sand-50 border border-sand-200/90 shadow-2xs flex items-center justify-center shrink-0">
            <Logo variant="full-color" pose="hacking" animated={true} className="w-7 h-7" />
          </div>
          <div>
            <div className="flex items-center gap-2">
              <span className="text-[9px] font-mono font-bold uppercase tracking-wider text-stone-700 bg-sand-100 px-2 py-0.5 rounded border border-sand-200">
                TASK COMPOSER
              </span>
              <span className="text-[11px] font-mono text-stone-400 font-semibold">
                Autonomous Task Dispatch
              </span>
            </div>
            <h1 className="text-lg font-bold text-stone-900 tracking-tight mt-0.5">
              Task Composer
            </h1>
            <p className="text-xs text-stone-500 mt-0.5">
              Build features, fix bugs, or refactor code using paired Architect and Implementer models.
            </p>
          </div>
        </div>

        <button
          onClick={() => setPrompt("")}
          className="text-xs font-semibold text-stone-400 hover:text-stone-700 transition-colors self-end sm:self-center cursor-pointer"
        >
          Clear Form
        </button>
      </div>

      {/* Platform Quota Telemetry Strip */}
      {spend?.allowance && spend.allowance.length > 0 && (
        <div className="p-3 rounded-xl bg-sand-50/80 border border-sand-200/90 flex flex-wrap items-center justify-between gap-2.5 text-xs shadow-2xs">
          <div className="flex items-center gap-2">
            <Zap className="w-3.5 h-3.5 text-kiwi-600 fill-current" />
            <span className="font-bold text-stone-900 text-xs">Platform Allowances:</span>
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
                  className={`px-2 py-0.5 rounded-lg border text-[10px] font-mono flex items-center gap-1.5 ${
                    exhausted
                      ? "bg-rose-50 border-rose-200 text-rose-800 font-bold"
                      : "bg-white border-sand-200 text-stone-700 shadow-2xs"
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

          <a href="/models" className="text-[11px] text-stone-500 hover:text-stone-900 font-mono font-medium">
            Models &rarr;
          </a>
        </div>
      )}

      {/* Main Task Composer Card */}
      <div className="p-4 sm:p-5 rounded-2xl border border-sand-200/90 bg-white shadow-2xs space-y-4">
        
        {/* Preset Objective Starters */}
        <div>
          <div className="flex items-center justify-between mb-2">
            <label className="text-xs font-bold text-stone-800 flex items-center gap-1.5">
              <span>Goal Starters</span>
              <span className="text-[10px] font-mono text-stone-400 font-normal">(Click to prefill)</span>
            </label>
          </div>

          <div className="flex flex-wrap gap-1.5">
            {PRESET_GOALS.map((p) => (
              <button
                key={p.id}
                type="button"
                onClick={() => setPrompt(p.template)}
                className="px-2.5 py-1.5 rounded-xl border border-sand-200 bg-sand-50/70 hover:bg-sand-100 hover:border-sand-300 text-stone-700 text-xs font-medium transition-all flex items-center gap-1.5 cursor-pointer shadow-2xs"
              >
                {p.icon}
                <span>{p.label}</span>
              </button>
            ))}
          </div>
        </div>

        {/* Task Objective Textarea */}
        <div>
          <div className="flex items-center justify-between mb-1.5">
            <label className="text-xs font-bold text-stone-900">Task Objective &amp; Requirements</label>
            <span className="text-[11px] font-mono text-stone-400">Markdown supported</span>
          </div>

          <div className="rounded-xl border border-sand-200 focus-within:border-stone-900 bg-sand-50/40 focus-within:bg-white transition-all shadow-2xs">
            <textarea
              rows={4}
              value={prompt}
              onChange={(e) => setPrompt(e.target.value)}
              placeholder="e.g. Add an endpoint in pkg/api/health.go that returns 200 OK with server uptime, and add unit tests in health_test.go"
              className="w-full p-3.5 text-xs text-stone-900 placeholder-stone-400 font-sans resize-none outline-none bg-transparent leading-relaxed"
            />
            <div className="p-2.5 bg-sand-50/90 border-t border-sand-200/80 flex flex-wrap items-center justify-between gap-2 text-[11px]">
              <span className="text-stone-500">
                💡 Tip: Reference specific files like <code className="font-mono text-stone-800 bg-sand-200/70 px-1 py-0.5 rounded">pkg/auth/jwt.go</code> for pinpoint edits
              </span>
              <span className="font-mono text-emerald-700 font-semibold flex items-center gap-1">
                <ShieldCheck className="w-3.5 h-3.5 text-emerald-600" />
                <span>Isolated Container Sandbox</span>
              </span>
            </div>
          </div>
        </div>

        {/* Configuration Row 1: Target Repository & Automated Verification */}
        {reposLoaded && repos.length === 0 ? (
          <div className="p-3.5 rounded-xl border border-amber-200 bg-amber-50/70 text-xs text-amber-900 flex items-center justify-between gap-3">
            <span>No repositories connected yet — connect GitHub to assign tasks.</span>
            <a href="/integrations" className="px-3 py-1.5 rounded-xl bg-charcoal-900 hover:bg-charcoal-800 text-white font-semibold shrink-0 cursor-pointer">
              Connect GitHub
            </a>
          </div>
        ) : (
          <div className="grid grid-cols-1 sm:grid-cols-2 gap-3.5 text-xs">
            <div>
              <label className="block font-bold text-stone-800 mb-1">Target Repository &amp; Branch</label>
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
                  title="Branch (defaults to repo default branch)"
                  className="w-24 px-3 py-2 rounded-xl bg-sand-50/80 hover:bg-white focus:bg-white border border-sand-200 text-stone-900 font-mono text-xs outline-none focus:border-stone-900 transition-all font-medium shrink-0 shadow-2xs"
                />
              </div>
            </div>

            <div>
              <label className="block font-bold text-stone-800 mb-1 flex items-center justify-between">
                <span>Automated Test Guard</span>
                <span className="text-[10px] font-mono text-stone-400 font-normal">Must pass 100%</span>
              </label>
              <input
                type="text"
                value={testCmd}
                onChange={(e) => setTestCmd(e.target.value)}
                placeholder="e.g. npm test, pytest, go test ./... (auto-detected if blank)"
                className="w-full px-3 py-2 rounded-xl bg-sand-50/80 hover:bg-white focus:bg-white border border-sand-200 text-stone-900 font-mono text-xs outline-none focus:border-stone-900 transition-all font-medium shadow-2xs"
              />
            </div>
          </div>
        )}

        {/* Configuration Row 2: DUAL MODEL PAIRING SELECTORS */}
        <ModelSelector
          architectModel={architectModel}
          workerModel={workerModel}
          onArchitectChange={setArchitectModel}
          onWorkerChange={setWorkerModel}
        />

        {/* Configuration Row 3: Execution Strategy & Guardrails */}
        <div className="p-3.5 sm:p-4 rounded-xl bg-sand-50/60 border border-sand-200/90 shadow-2xs space-y-3">
          <div className="flex items-center justify-between border-b border-sand-200/80 pb-2">
            <div className="flex items-center gap-1.5">
              <Sliders className="w-3.5 h-3.5 text-stone-700" />
              <span className="text-xs font-bold text-stone-900">Execution Strategy &amp; Safety Guardrails</span>
            </div>
            <span className="text-[10px] font-mono text-stone-400 uppercase tracking-wider">GUARDRAILS</span>
          </div>

          <div className="grid grid-cols-1 sm:grid-cols-3 gap-3 text-xs">
            {/* Strategy */}
            <div>
              <label className="block font-bold text-stone-800 mb-1.5">Execution Flow</label>
              <div className="space-y-1.5">
                <label
                  onClick={() => setStrategy("plan")}
                  className={`flex items-center justify-between p-2 rounded-xl border cursor-pointer transition-all ${
                    strategy === "plan"
                      ? "bg-white border-stone-900 shadow-2xs font-bold text-stone-900"
                      : "bg-white/60 border-sand-200 text-stone-600 hover:bg-white"
                  }`}
                >
                  <div className="flex items-center gap-1.5 text-xs">
                    <Compass className="w-3.5 h-3.5 text-indigo-600" />
                    <span>Plan Approval (Recommended)</span>
                  </div>
                  {strategy === "plan" && <CheckCircle2 className="w-3.5 h-3.5 text-stone-900" />}
                </label>

                <label
                  onClick={() => setStrategy("direct")}
                  className={`flex items-center justify-between p-2 rounded-xl border cursor-pointer transition-all ${
                    strategy === "direct"
                      ? "bg-white border-stone-900 shadow-2xs font-bold text-stone-900"
                      : "bg-white/60 border-sand-200 text-stone-600 hover:bg-white"
                  }`}
                >
                  <div className="flex items-center gap-1.5 text-xs">
                    <Zap className="w-3.5 h-3.5 text-kiwi-600 fill-current" />
                    <span>Autonomous Execution</span>
                  </div>
                  {strategy === "direct" && <CheckCircle2 className="w-3.5 h-3.5 text-stone-900" />}
                </label>
              </div>
            </div>

            {/* Spend Cap */}
            <div>
              <label className="block font-bold text-stone-800 mb-1.5 flex items-center justify-between">
                <span>Max Spend Hard-Cap</span>
                <span className="text-[10px] font-mono text-stone-400 font-normal">USD</span>
              </label>
              <div className="flex items-center gap-1">
                {[0.25, 0.50, 1.00, 2.50].map((cap) => (
                  <button
                    key={cap}
                    type="button"
                    onClick={() => setSpendCap(cap)}
                    className={`flex-1 py-1.5 rounded-lg font-mono font-bold text-[11px] transition-all cursor-pointer ${
                      spendCap === cap
                        ? "bg-stone-900 text-white shadow-2xs"
                        : "bg-white border border-sand-200 hover:bg-sand-100 text-stone-700"
                    }`}
                  >
                    ${cap.toFixed(2)}
                  </button>
                ))}
              </div>
              <p className="text-[10px] text-stone-400 mt-1 font-mono">Pauses safely if limit is reached</p>
            </div>

            {/* Target Action */}
            <div>
              <label className="block font-bold text-stone-800 mb-1.5">Action on Pass</label>
              <div className="space-y-1.5">
                <label
                  onClick={() => setMode("pr")}
                  className={`flex items-center justify-between p-2 rounded-xl border cursor-pointer transition-all ${
                    mode === "pr"
                      ? "bg-white border-stone-900 shadow-2xs font-bold text-stone-900"
                      : "bg-white/60 border-sand-200 text-stone-600 hover:bg-white"
                  }`}
                >
                  <div className="flex items-center gap-1.5 text-xs">
                    <GitPullRequest className="w-3.5 h-3.5 text-emerald-600" />
                    <span>Open Pull Request</span>
                  </div>
                  {mode === "pr" && <CheckCircle2 className="w-3.5 h-3.5 text-stone-900" />}
                </label>

                <label
                  onClick={() => setMode("dryrun")}
                  className={`flex items-center justify-between p-2 rounded-xl border cursor-pointer transition-all ${
                    mode === "dryrun"
                      ? "bg-white border-stone-900 shadow-2xs font-bold text-stone-900"
                      : "bg-white/60 border-sand-200 text-stone-600 hover:bg-white"
                  }`}
                >
                  <div className="flex items-center gap-1.5 text-xs">
                    <Lock className="w-3.5 h-3.5 text-stone-500" />
                    <span>Dry-Run (No Push)</span>
                  </div>
                  {mode === "dryrun" && <CheckCircle2 className="w-3.5 h-3.5 text-stone-900" />}
                </label>
              </div>
            </div>
          </div>
        </div>

        {/* Pre-Flight Status Bar & Launch Button */}
        <div className="pt-3 border-t border-sand-200/80 space-y-3">
          {submitError && (
            <div className="p-3 rounded-xl bg-rose-50 border border-rose-200 text-rose-800 text-xs flex items-center gap-2">
              <span>{submitError}</span>
            </div>
          )}

          <div className="flex flex-wrap items-center justify-between gap-3">
            <div className="flex flex-wrap items-center gap-2.5 text-[11px] text-stone-500 font-mono">
              <span>Mode: <strong className="text-stone-900 font-bold">{strategy === "plan" ? "Plan Mode" : "Autonomous Mode"}</strong></span>
              <span>•</span>
              <span>Cap: <strong className="text-stone-900 font-bold">${spendCap.toFixed(2)} USD</strong></span>
              <span>•</span>
              <span>Target: <strong className="text-stone-900 font-bold">{mode === "pr" ? "GitHub Pull Request" : "Dry-Run Local"}</strong></span>
            </div>

            <button
              onClick={handleStart}
              disabled={isSubmitting || !prompt.trim() || !repo}
              className="px-6 py-2.5 rounded-xl bg-charcoal-900 hover:bg-charcoal-800 text-white font-bold text-xs shadow-sm flex items-center gap-2 transition-all active:scale-[0.98] disabled:opacity-40 cursor-pointer"
            >
              {isSubmitting ? (
                <KiwiMicroButtonLoader />
              ) : (
                <Play className="w-3.5 h-3.5 text-kiwi-400 fill-current" />
              )}
              <span>Launch Task &rarr;</span>
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
