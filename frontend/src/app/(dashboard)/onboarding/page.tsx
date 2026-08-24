"use client";

import React, { useState, useEffect } from "react";
import {
  CheckCircle2,
  ChevronRight,
  Loader2,
  AlertCircle,
  Key,
  FolderGit2,
  Rocket,
  Sparkles,
  Zap,
  ArrowRight,
} from "lucide-react";
import { useRouter } from "next/navigation";
import { client, type Integration, type GithubRepo } from "@/lib/api";
import { capture } from "@/lib/analytics";
import { Logo } from "@/components/Logo";

const STARTER_TASKS = [
  {
    id: "healthz",
    title: "Add /healthz API endpoint",
    description: "Add a /healthz endpoint returning 200 OK and server uptime to the HTTP server router",
    task: "Add a /healthz endpoint returning 200 OK and server uptime JSON to the HTTP server router",
    tag: "BACKEND",
  },
  {
    id: "sidebar",
    title: "Fix responsive drawer layout",
    description: "Fix responsive drawer z-index and flex layout wrapping on mobile viewports",
    task: "Fix responsive drawer z-index and flex layout wrapping on mobile viewports",
    tag: "FRONTEND",
  },
  {
    id: "tests",
    title: "Add unit tests for utilities",
    description: "Add unit tests for string formatting, date parsing, and error handling helpers",
    task: "Add unit tests for string formatting, date parsing, and error handling helpers",
    tag: "TESTING",
  },
];

const MODEL_PROVIDERS = [
  {
    key: "anthropic",
    label: "Anthropic Claude",
    credName: "ANTHROPIC_API_KEY",
    placeholder: "sk-ant-api03-…",
    models: "Claude 3.7 Sonnet, Claude 3.5 Haiku",
  },
  {
    key: "openai",
    label: "OpenAI GPT-4",
    credName: "OPENAI_API_KEY",
    placeholder: "sk-proj-…",
    models: "GPT-4.5, GPT-4o, o3-mini",
  },
  {
    key: "gemini",
    label: "Google Gemini",
    credName: "GEMINI_API_KEY",
    placeholder: "AIzaSy…",
    models: "Gemini 2.0 Flash, Gemini 1.5 Pro",
  },
];

export default function OnboardingPage() {
  const router = useRouter();

  // Load initial step from localStorage, default to 1
  const [step, setStep] = useState(() => {
    if (typeof window !== "undefined") {
      const saved = localStorage.getItem("kiwi_onboarding_step");
      return saved ? Math.min(Math.max(parseInt(saved, 10) || 1, 1), 3) : 1;
    }
    return 1;
  });

  const [ghConnected, setGhConnected] = useState(false);
  const [ghToken, setGhToken] = useState("");
  const [useByok, setUseByok] = useState(false);
  const [modelProvider, setModelProvider] = useState("anthropic");
  const [modelKey, setModelKey] = useState("");
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState("");
  const [selectedStarter, setSelectedStarter] = useState(STARTER_TASKS[0].id);
  const [repos, setRepos] = useState<GithubRepo[]>([]);
  const [starterRepo, setStarterRepo] = useState("");

  // Persist step to localStorage
  useEffect(() => {
    if (typeof window !== "undefined") {
      localStorage.setItem("kiwi_onboarding_step", String(step));
    }
  }, [step]);

  // Step 1: Check and poll for GitHub connection
  useEffect(() => {
    if (ghConnected) return;
    const checkGH = async () => {
      try {
        const res = await client.listIntegrations();
        const gh = res.integrations.find((i: Integration) => i.key === "github");
        if (gh?.connected) {
          setGhConnected(true);
          const r = await client.listGithubRepos();
          if (r.repos && r.repos.length > 0) {
            setRepos(r.repos);
            setStarterRepo((prev) => prev || r.repos[0]?.url || "");
          }
        }
      } catch {
        /* best-effort */
      }
    };
    checkGH();
    const interval = setInterval(checkGH, 3000);
    return () => clearInterval(interval);
  }, [ghConnected]);

  // Load repos when entering step 3
  useEffect(() => {
    if (step === 3 && repos.length === 0) {
      client
        .listGithubRepos()
        .then((r) => {
          setRepos(r.repos || []);
          setStarterRepo((prev) => prev || r.repos[0]?.url || "");
        })
        .catch(() => {});
    }
  }, [step, repos.length]);

  const handleInstallApp = async () => {
    setBusy(true);
    setErr("");
    try {
      const { install_url } = await client.githubInstallUrl();
      window.location.href = install_url;
    } catch (e) {
      setErr(e instanceof Error ? e.message : "Could not start the GitHub App install");
      setBusy(false);
    }
  };

  const handleConnectPAT = async () => {
    const val = ghToken.trim();
    if (!val) {
      setErr("Please paste a valid GitHub Personal Access Token.");
      return;
    }
    setBusy(true);
    setErr("");
    try {
      await client.setCredential("GITHUB_TOKEN", "github", val);
      capture("repo_connected", { surface: "onboarding" });
      setGhConnected(true);
      setStep(2);
    } catch (e) {
      setErr(e instanceof Error ? e.message : "Failed to connect GitHub token");
    } finally {
      setBusy(false);
    }
  };

  const handleSaveModelKey = async () => {
    if (!useByok) {
      capture("onboarding_step_skipped", { step: 2 });
      setStep(3);
      return;
    }

    const val = modelKey.trim();
    if (!val) {
      setErr("Please enter a valid API key, or choose Kiwi Platform Allowance.");
      return;
    }

    setBusy(true);
    setErr("");
    try {
      const secretName =
        MODEL_PROVIDERS.find((p) => p.key === modelProvider)?.credName ?? "ANTHROPIC_API_KEY";
      await client.setCredential(secretName, "llm", val);
      capture("model_key_added", { provider: modelProvider, surface: "onboarding" });
      setStep(3);
    } catch (e) {
      setErr(e instanceof Error ? e.message : "Failed to save model credential");
    } finally {
      setBusy(false);
    }
  };

  const handleCompleteAndLaunch = (taskText: string, repoUrl = "") => {
    if (typeof window !== "undefined") {
      localStorage.setItem("kiwi_starter_task", taskText);
      localStorage.setItem("kiwi_starter_repo", repoUrl);
      localStorage.setItem("kiwi_onboarding_completed", "1");
      localStorage.setItem("onboarded", "1");
      localStorage.removeItem("kiwi_onboarding_step");
    }
    // Launch Guided Platform Tour on Dashboard
    router.push("/?tour=true");
  };

  const currentMascotPose =
    step === 1 ? "vibing" : step === 2 ? "hacking" : "dancing";

  return (
    <div className="w-full max-w-3xl mx-auto py-1 sm:py-2 space-y-3.5 sm:space-y-4 font-sans text-stone-900 select-none">
      
      {/* Onboarding Header Banner */}
      <div className="relative overflow-hidden p-4 sm:p-5 rounded-2xl border border-sand-200/90 bg-white shadow-2xs flex flex-col sm:flex-row items-center justify-between gap-4">
        <div className="flex items-center gap-3.5 text-left w-full sm:w-auto">
          <div className="w-12 h-12 rounded-2xl bg-sand-50 border border-sand-200/90 flex items-center justify-center shadow-2xs shrink-0">
            <Logo
              variant="full-color"
              pose={currentMascotPose}
              animated={true}
              className="w-7 h-7"
            />
          </div>
          <div>
            <div className="flex items-center gap-2">
              <span className="text-[9px] font-mono font-bold uppercase tracking-wider text-stone-700 bg-sand-100 px-2 py-0.5 rounded border border-sand-200">
                PLATFORM SETUP
              </span>
              <span className="text-xs font-mono text-stone-400 font-semibold">
                Step {step} of 3
              </span>
            </div>
            <h1 className="text-lg font-bold tracking-tight text-stone-900 mt-0.5">
              Welcome to Kiwi
            </h1>
            <p className="text-xs text-stone-500 mt-0.5">
              Connect your repositories, configure model access, and launch your first automated task.
            </p>
          </div>
        </div>

        <button
          onClick={() => handleCompleteAndLaunch("")}
          className="text-xs font-semibold text-stone-400 hover:text-stone-700 transition-colors shrink-0 self-end sm:self-center cursor-pointer"
        >
          Skip setup and open dashboard &rarr;
        </button>
      </div>

      {/* 3-Step Progress Header Stepper */}
      <div className="grid grid-cols-3 gap-2">
        {[
          { num: 1, label: "Connect Codebase", icon: <FolderGit2 className="w-3.5 h-3.5" /> },
          { num: 2, label: "Model Intelligence", icon: <Key className="w-3.5 h-3.5" /> },
          { num: 3, label: "First Task", icon: <Rocket className="w-3.5 h-3.5" /> },
        ].map((s) => {
          const isCompleted = step > s.num;
          const isCurrent = step === s.num;
          return (
            <button
              key={s.num}
              onClick={() => setStep(s.num)}
              className={`p-2.5 sm:p-3 rounded-xl border text-left transition-all flex items-center gap-2.5 cursor-pointer ${
                isCurrent
                  ? "bg-white border-stone-900 shadow-2xs font-bold text-stone-900"
                  : isCompleted
                  ? "bg-sand-50/90 border-emerald-300/80 text-stone-800"
                  : "bg-white/60 border-sand-200/70 text-stone-400 opacity-60 hover:opacity-80"
              }`}
            >
              <div
                className={`w-5 h-5 rounded-lg flex items-center justify-center text-xs font-mono font-bold shrink-0 ${
                  isCompleted
                    ? "bg-emerald-100 text-emerald-800 border border-emerald-200"
                    : isCurrent
                    ? "bg-stone-900 text-white"
                    : "bg-sand-100 text-stone-400"
                }`}
              >
                {isCompleted ? <CheckCircle2 className="w-3 h-3" /> : s.num}
              </div>
              <div className="min-w-0 hidden sm:block">
                <p className="text-xs truncate">{s.label}</p>
              </div>
            </button>
          );
        })}
      </div>

      {/* STEP 1: CONNECT CODEBASE */}
      {step === 1 && (
        <div className="p-4 sm:p-5 rounded-2xl bg-white border border-sand-200/90 shadow-2xs space-y-4 animate-in fade-in zoom-in-95 duration-200">
          <div className="flex items-start justify-between gap-4">
            <div>
              <div className="flex items-center gap-2">
                <div className="p-1.5 rounded-lg bg-emerald-50 border border-emerald-200">
                  <FolderGit2 className="w-4 h-4 text-emerald-700" />
                </div>
                <h2 className="text-sm sm:text-base font-bold text-stone-900">Step 1: Connect your Repository</h2>
              </div>
              <p className="text-xs text-stone-500 mt-1 leading-relaxed">
                Link your codebase so Kiwi autonomous agents can read files, write features, and open verified pull requests.
              </p>
            </div>

            {ghConnected && (
              <span className="px-2.5 py-1 rounded-md text-[10px] font-mono font-bold bg-emerald-50 text-emerald-800 border border-emerald-200 flex items-center gap-1.5 shrink-0">
                <CheckCircle2 className="w-3.5 h-3.5 text-emerald-600" />
                <span>CONNECTED</span>
              </span>
            )}
          </div>

          {ghConnected ? (
            <div className="p-3.5 sm:p-4 rounded-xl bg-sand-50/80 border border-sand-200 flex items-center justify-between gap-3">
              <div className="flex items-center gap-3 min-w-0">
                <CheckCircle2 className="w-5 h-5 text-emerald-600 shrink-0" />
                <div className="min-w-0">
                  <p className="text-xs font-bold text-stone-900 truncate">
                    GitHub App Connected ({repos.length} repositories authorized)
                  </p>
                  <p className="text-[11px] font-mono text-stone-500 truncate mt-0.5">
                    Ready to execute autonomous tasks.
                  </p>
                </div>
              </div>
              <button
                onClick={() => setStep(2)}
                className="px-4 py-2 rounded-xl bg-charcoal-900 hover:bg-charcoal-800 text-white font-semibold text-xs shadow-sm transition-all flex items-center gap-1.5 cursor-pointer shrink-0"
              >
                <span>Continue to Step 2</span>
                <ArrowRight className="w-3.5 h-3.5 text-kiwi-400" />
              </button>
            </div>
          ) : (
            <div className="space-y-3.5">
              <div className="p-3.5 sm:p-4 rounded-xl border border-sand-200 bg-sand-50/50 flex flex-col sm:flex-row items-start sm:items-center justify-between gap-3">
                <div>
                  <p className="text-xs font-bold text-stone-900">Install the official Kiwi GitHub App (Recommended)</p>
                  <p className="text-[11px] text-stone-500 mt-0.5">
                    Provides secure, scoped, auto-refreshing repository access for autonomous agents.
                  </p>
                </div>
                <button
                  onClick={handleInstallApp}
                  disabled={busy}
                  className="px-4 py-2 rounded-xl bg-charcoal-900 hover:bg-charcoal-800 text-white font-semibold text-xs shadow-sm transition-all flex items-center gap-2 cursor-pointer shrink-0"
                >
                  {busy ? <Loader2 className="w-3.5 h-3.5 animate-spin" /> : null}
                  <span>Install GitHub App &rarr;</span>
                </button>
              </div>

              <details className="text-xs group">
                <summary className="text-stone-500 hover:text-stone-900 cursor-pointer font-medium select-none">
                  Or connect with a GitHub Personal Access Token (PAT)
                </summary>
                <div className="mt-2.5 p-3 rounded-xl border border-sand-200 bg-white space-y-2.5">
                  <div className="flex gap-2">
                    <input
                      type="password"
                      value={ghToken}
                      onChange={(e) => setGhToken(e.target.value)}
                      placeholder="github_pat_..."
                      className="flex-1 px-3 py-2 rounded-xl border border-sand-200 bg-sand-50/70 text-xs font-mono outline-none focus:border-stone-900 focus:bg-white transition-all"
                    />
                    <button
                      onClick={handleConnectPAT}
                      disabled={busy}
                      className="px-4 py-2 rounded-xl bg-charcoal-900 hover:bg-charcoal-800 text-white font-semibold text-xs shadow-sm transition-all cursor-pointer"
                    >
                      {busy ? <Loader2 className="w-3.5 h-3.5 animate-spin" /> : "Connect PAT"}
                    </button>
                  </div>
                  {err && (
                    <div className="flex items-center gap-1.5 text-rose-600 text-xs font-mono">
                      <AlertCircle className="w-3.5 h-3.5" />
                      <span>{err}</span>
                    </div>
                  )}
                </div>
              </details>
            </div>
          )}

          <div className="flex items-center justify-between pt-3 border-t border-sand-200/80 text-xs">
            <button
              type="button"
              onClick={() => {
                capture("onboarding_step_skipped", { step: 1 });
                setStep(2);
              }}
              className="text-stone-400 hover:text-stone-700 font-medium cursor-pointer"
            >
              Skip this step for now &rarr;
            </button>

            <button
              onClick={() => setStep(2)}
              className="px-4 py-1.5 rounded-xl border border-sand-200 hover:bg-sand-100 text-stone-700 font-semibold text-xs transition-all flex items-center gap-1 cursor-pointer"
            >
              <span>Next: Models</span>
              <ChevronRight className="w-3.5 h-3.5" />
            </button>
          </div>
        </div>
      )}

      {/* STEP 2: MODEL CREDENTIALS & COMPUTE */}
      {step === 2 && (
        <div className="p-4 sm:p-5 rounded-2xl bg-white border border-sand-200/90 shadow-2xs space-y-4 animate-in fade-in zoom-in-95 duration-200">
          <div>
            <div className="flex items-center gap-2">
              <div className="p-1.5 rounded-lg bg-sky-50 border border-sky-200">
                <Key className="w-4 h-4 text-sky-700" />
              </div>
              <h2 className="text-sm sm:text-base font-bold text-stone-900">Step 2: Model Intelligence &amp; Quotas</h2>
            </div>
            <p className="text-xs text-stone-500 mt-1 leading-relaxed">
              Choose how agents access frontier models for planning, code generation, and test verification.
            </p>
          </div>

          <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
            {/* Option A: Managed Kiwi Allowance */}
            <div
              onClick={() => setUseByok(false)}
              className={`p-3.5 sm:p-4 rounded-xl border text-left cursor-pointer transition-all ${
                !useByok
                  ? "border-kiwi-400 bg-kiwi-50/30 shadow-2xs"
                  : "border-sand-200 bg-sand-50/50 hover:bg-sand-50"
              }`}
            >
              <div className="flex items-center justify-between mb-1.5">
                <div className="flex items-center gap-1.5 font-bold text-xs text-stone-900">
                  <Zap className="w-3.5 h-3.5 text-kiwi-600 fill-current" />
                  <span>Kiwi Managed Frontier Allowance</span>
                </div>
                {!useByok && <CheckCircle2 className="w-4 h-4 text-kiwi-600" />}
              </div>
              <p className="text-xs text-stone-600 leading-relaxed">
                Use built-in platform quotas with zero configuration. Includes access to Claude 3.7 Sonnet, GPT-4.5, and Gemini 2.0.
              </p>
            </div>

            {/* Option B: Bring Your Own Key (BYOK) */}
            <div
              onClick={() => setUseByok(true)}
              className={`p-3.5 sm:p-4 rounded-xl border text-left cursor-pointer transition-all ${
                useByok
                  ? "border-sky-400 bg-sky-50/30 shadow-2xs"
                  : "border-sand-200 bg-sand-50/50 hover:bg-sand-50"
              }`}
            >
              <div className="flex items-center justify-between mb-1.5">
                <div className="flex items-center gap-1.5 font-bold text-xs text-stone-900">
                  <Key className="w-3.5 h-3.5 text-sky-600" />
                  <span>Bring Your Own Key (BYOK)</span>
                </div>
                {useByok && <CheckCircle2 className="w-4 h-4 text-sky-600" />}
              </div>
              <p className="text-xs text-stone-600 leading-relaxed">
                Use your own Anthropic, OpenAI, or Google AI key for unlimited token allowances and custom limits.
              </p>
            </div>
          </div>

          {useByok && (
            <div className="p-3.5 rounded-xl bg-sand-50/70 border border-sand-200 space-y-3 animate-in fade-in duration-150">
              <div className="flex flex-col sm:flex-row items-stretch sm:items-center gap-2">
                <select
                  value={modelProvider}
                  onChange={(e) => setModelProvider(e.target.value)}
                  className="px-3 py-2 rounded-xl border border-sand-200 bg-white text-xs font-semibold text-stone-800 outline-none"
                >
                  {MODEL_PROVIDERS.map((p) => (
                    <option key={p.key} value={p.key}>
                      {p.label}
                    </option>
                  ))}
                </select>

                <input
                  type="password"
                  value={modelKey}
                  onChange={(e) => setModelKey(e.target.value)}
                  placeholder={
                    MODEL_PROVIDERS.find((p) => p.key === modelProvider)?.placeholder
                  }
                  className="flex-1 px-3 py-2 rounded-xl border border-sand-200 bg-white text-xs font-mono outline-none focus:border-stone-900 transition-all"
                />
              </div>

              {err && (
                <div className="flex items-center gap-1.5 text-rose-600 text-xs font-mono">
                  <AlertCircle className="w-3.5 h-3.5" />
                  <span>{err}</span>
                </div>
              )}
            </div>
          )}

          <div className="flex items-center justify-between pt-3 border-t border-sand-200/80 text-xs">
            <button
              type="button"
              onClick={() => setStep(1)}
              className="text-stone-500 hover:text-stone-800 font-medium cursor-pointer"
            >
              &larr; Back to Step 1
            </button>

            <button
              onClick={handleSaveModelKey}
              disabled={busy}
              className="px-4 py-2 rounded-xl bg-charcoal-900 hover:bg-charcoal-800 text-white font-semibold text-xs shadow-sm transition-all flex items-center gap-1.5 cursor-pointer"
            >
              {busy ? <Loader2 className="w-3.5 h-3.5 animate-spin" /> : null}
              <span>Save &amp; Continue</span>
              <ChevronRight className="w-3.5 h-3.5 text-kiwi-400" />
            </button>
          </div>
        </div>
      )}

      {/* STEP 3: LAUNCH FIRST TASK */}
      {step === 3 && (
        <div className="p-4 sm:p-5 rounded-2xl bg-white border border-sand-200/90 shadow-2xs space-y-4 animate-in fade-in zoom-in-95 duration-200">
          <div>
            <div className="flex items-center gap-2">
              <div className="p-1.5 rounded-lg bg-amber-50 border border-amber-200">
                <Rocket className="w-4 h-4 text-amber-700" />
              </div>
              <h2 className="text-sm sm:text-base font-bold text-stone-900">Step 3: Launch your First Task</h2>
            </div>
            <p className="text-xs text-stone-500 mt-1 leading-relaxed">
              Choose a starter template or prefill your goal to see agents plan, execute, and verify changes.
            </p>
          </div>

          <div className="grid grid-cols-1 sm:grid-cols-3 gap-3">
            {STARTER_TASKS.map((t) => {
              const isSelected = selectedStarter === t.id;
              return (
                <div
                  key={t.id}
                  onClick={() => setSelectedStarter(t.id)}
                  className={`p-3 rounded-xl border text-left cursor-pointer transition-all flex flex-col justify-between ${
                    isSelected
                      ? "border-stone-900 bg-sand-50/90 shadow-2xs ring-1 ring-stone-900"
                      : "border-sand-200 bg-white hover:border-sand-300"
                  }`}
                >
                  <div>
                    <div className="flex items-center justify-between mb-1.5">
                      <span className="text-[9px] font-mono font-bold uppercase tracking-wider text-stone-500 bg-sand-100 px-1.5 py-0.2 rounded border border-sand-200">
                        {t.tag}
                      </span>
                      {isSelected && <CheckCircle2 className="w-3.5 h-3.5 text-stone-900" />}
                    </div>
                    <p className="text-xs font-bold text-stone-900 leading-snug">{t.title}</p>
                    <p className="text-[11px] text-stone-500 mt-1 line-clamp-2 leading-relaxed">
                      {t.description}
                    </p>
                  </div>
                </div>
              );
            })}
          </div>

          {repos.length > 0 && (
            <div className="space-y-1.5">
              <label className="text-xs font-bold text-stone-800">Target Repository</label>
              <select
                value={starterRepo}
                onChange={(e) => setStarterRepo(e.target.value)}
                className="w-full px-3 py-2 rounded-xl border border-sand-200 bg-sand-50/70 text-xs font-mono outline-none"
              >
                {repos.map((r) => (
                  <option key={r.full_name} value={r.url}>
                    {r.full_name}
                  </option>
                ))}
              </select>
            </div>
          )}

          <div className="flex items-center justify-between pt-3 border-t border-sand-200/80 text-xs">
            <button
              type="button"
              onClick={() => setStep(2)}
              className="text-stone-500 hover:text-stone-800 font-medium cursor-pointer"
            >
              &larr; Back to Step 2
            </button>

            <button
              onClick={() => {
                const starterObj = STARTER_TASKS.find((s) => s.id === selectedStarter);
                handleCompleteAndLaunch(
                  starterObj?.task || STARTER_TASKS[0].task,
                  starterRepo
                );
              }}
              className="px-4 py-2 rounded-xl bg-charcoal-900 hover:bg-charcoal-800 text-white font-semibold text-xs shadow-sm transition-all flex items-center gap-2 active:scale-[0.98] cursor-pointer"
            >
              <Sparkles className="w-3.5 h-3.5 text-kiwi-400" />
              <span>Launch First Task &amp; Start Tour &rarr;</span>
            </button>
          </div>
        </div>
      )}

    </div>
  );
}
