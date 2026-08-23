"use client";

import { useState, useEffect } from "react";
import { CheckCircle2, ChevronRight, Loader2, AlertCircle, Key, FolderGit2, Rocket, Sparkles } from "lucide-react";
import { useRouter } from "next/navigation";
import { client, type Integration, type GithubRepo } from "@/lib/api";
import { capture } from "@/lib/analytics";

const STARTER_TASKS = [
  {
    id: "healthz",
    title: "Add /healthz API endpoint",
    description: "Add a /healthz endpoint returning 200 OK and uptime JSON to the server router",
    task: "Add a /healthz endpoint returning 200 OK and server uptime to the Go HTTP server router",
  },
  {
    id: "sidebar",
    title: "Fix responsive drawer layout",
    description: "Fix responsive drawer z-index and flex layout wrapping on mobile viewports",
    task: "Fix responsive drawer z-index and flex layout wrapping on mobile viewports",
  },
  {
    id: "tests",
    title: "Add unit tests for utilities",
    description: "Add unit tests for string formatting and error handling helpers",
    task: "Add unit tests for string formatting and error handling helpers",
  },
];

// The model providers offered at signup, and the credential name each key is
// stored under. Kept as one table so the dropdown, the placeholder and the
// saved credential name cannot drift apart — a key stored under a name the
// backend never looks up connects nothing and reports success.
const MODEL_PROVIDERS = [
  { key: "anthropic", label: "Anthropic", credName: "ANTHROPIC_API_KEY", placeholder: "sk-ant-…" },
  { key: "gemini", label: "Gemini", credName: "GEMINI_API_KEY", placeholder: "AIza…" },
  { key: "openai", label: "OpenAI", credName: "OPENAI_API_KEY", placeholder: "sk-…" },
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

  const [ghToken, setGhToken] = useState("");
  const [modelProvider, setModelProvider] = useState("anthropic");
  const [modelKey, setModelKey] = useState("");
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState("");
  const [selectedStarter, setSelectedStarter] = useState(STARTER_TASKS[0].id);
  // A prefilled task with no repository cannot be launched — the composer
  // requires one — so step 3 picks the repo here rather than dropping the user
  // on the dashboard with a half-filled form.
  const [repos, setRepos] = useState<GithubRepo[]>([]);
  const [starterRepo, setStarterRepo] = useState("");

  useEffect(() => {
    if (step !== 3) return;
    client.listGithubRepos()
      .then(r => {
        setRepos(r.repos);
        setStarterRepo(prev => prev || r.repos[0]?.url || "");
      })
      .catch(() => {});
  }, [step]);

  // Persist step to localStorage whenever it changes
  useEffect(() => {
    if (typeof window !== "undefined") {
      localStorage.setItem("kiwi_onboarding_step", String(step));
    }
  }, [step]);

  // Step 1: Poll for GitHub connection
  useEffect(() => {
    if (step !== 1) return;
    const checkGH = async () => {
      try {
        const res = await client.listIntegrations();
        const gh = res.integrations.find((i: Integration) => i.key === "github");
        if (gh?.connected && step === 1) {
          setStep(2);
        }
      } catch {
        /* best-effort */
      }
    };
    checkGH();
    const interval = setInterval(checkGH, 3000);
    return () => clearInterval(interval);
  }, [step]);

  // Fetches the signed install link rather than navigating straight at the
  // endpoint: it sits behind bearer auth and a top-level navigation carries no
  // Authorization header.
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

  const handleConnectRepo = async () => {
    const val = ghToken.trim();
    if (!val) {
      setErr("Paste a GitHub PAT first.");
      return;
    }
    setBusy(true);
    setErr("");
    try {
      await client.setCredential("GITHUB_TOKEN", "github", val);
      capture("repo_connected", { surface: "onboarding" });
      const res = await client.listIntegrations();
      const gh = res.integrations.find((i: Integration) => i.key === "github");
      if (gh?.connected) {
        setStep(2);
      } else {
        setStep(2); // Proceed to step 2 after credential saved
      }
    } catch (e) {
      setErr(e instanceof Error ? e.message : "Failed to connect");
    } finally {
      setBusy(false);
    }
  };

  const handleSaveModelKey = async () => {
    const val = modelKey.trim();
    if (!val) {
      // Key entry is optional if user already has global key or wants to skip
      capture("onboarding_step_skipped", { step: 2 });
      setStep(3);
      return;
    }
    setBusy(true);
    setErr("");
    try {
      // Name and kind must match the Integrations catalog exactly, or the key is
      // stored under something the backend never looks up. Every model provider
      // is kind "llm"; the provider is carried by the credential name.
      const secretName = MODEL_PROVIDERS.find((p) => p.key === modelProvider)?.credName ?? "ANTHROPIC_API_KEY";
      await client.setCredential(secretName, "llm", val);
      capture("model_key_added", { provider: modelProvider, surface: "onboarding" });
      setStep(3);
    } catch (e) {
      setErr(e instanceof Error ? e.message : "Failed to save model credential");
    } finally {
      setBusy(false);
    }
  };

  const handleLaunchStarter = (taskText: string, repoUrl = "") => {
    if (typeof window !== "undefined") {
      localStorage.setItem("kiwi_starter_task", taskText);
      localStorage.setItem("kiwi_starter_repo", repoUrl);
      localStorage.setItem("onboarded", "1");
      localStorage.removeItem("kiwi_onboarding_step");
    }
    router.push("/");
  };

  return (
    <div className="p-8 max-w-3xl mx-auto min-h-[85vh] flex flex-col justify-center">
      <div className="text-center mb-10">
        <p className="eyebrow justify-center mb-3">
          <span className="dot" /> Onboarding
        </p>
        <h1 className="text-4xl font-semibold tracking-tight text-stone-900 mb-3">Welcome to Kiwi</h1>
        <p className="text-stone-500 text-base max-w-xl mx-auto">
          Set up your repository connection, model credentials, and launch your first swarm task in 3 simple steps.
        </p>
      </div>

      <div className="space-y-6">
        {/* Step 1: Connect Repository */}
        <div
          className={`bg-white shadow-2xs p-6 transition-all duration-300 ${
            step === 1 ? "border-sand-200 shadow-[0_0_24px_-4px_rgba(147,198,69,0.35)] scale-[1.01]" : step > 1 ? "border-emerald-200 bg-emerald-50" : "opacity-60"
          }`}
        >
          <div className="flex items-start gap-4">
            <div
              className={`flex items-center justify-center w-8 h-8 rounded-full text-sm font-bold shrink-0 ${
                step > 1 ? "bg-kiwi-500 text-white" : "bg-white border border-sand-300 text-stone-600"
              }`}
            >
              {step > 1 ? <CheckCircle2 className="w-5 h-5" /> : "1"}
            </div>
            <div className="flex-1 min-w-0">
              <div className="flex items-center justify-between">
                <h2 className="text-xl font-medium text-stone-900 mb-1 flex items-center gap-2">
                  <FolderGit2 className="w-5 h-5 text-stone-500" /> Connect your Repository
                </h2>
                {step > 1 && (
                  <button type="button" onClick={() => setStep(1)} className="text-xs text-stone-400 hover:text-stone-700 underline">
                    Edit
                  </button>
                )}
              </div>
              <p className="text-stone-500 text-sm mb-4">
                Link your codebase so Kiwi agents can analyze, plan, and submit pull requests. Install the GitHub App and pick the repositories Kiwi may touch.
              </p>
              {step === 1 && (
                <div className="flex flex-col gap-3 max-w-md pt-2">
                  <button
                    onClick={handleInstallApp}
                    disabled={busy}
                    className="flex items-center justify-center gap-2 btn-primary px-5 py-2.5 transition-colors disabled:opacity-50"
                  >
                    {busy ? <Loader2 className="w-4 h-4 animate-spin" /> : null}
                    Install the GitHub App
                  </button>
                  <p className="text-xs text-stone-400">
                    Access covers only the repositories you select, expires
                    hourly, and you can revoke it from GitHub at any time.
                  </p>

                  <details className="pt-1">
                    <summary className="text-xs text-stone-400 cursor-pointer hover:text-stone-700">
                      Use a personal access token instead
                    </summary>
                    <div className="flex gap-2 pt-3">
                    <input
                      type="password"
                      value={ghToken}
                      onChange={(e) => setGhToken(e.target.value)}
                      placeholder="github_pat_..."
                      className="flex-1 field text-sm"
                    />
                    <button
                      onClick={handleConnectRepo}
                      disabled={busy}
                      className="flex items-center justify-center gap-2 btn-primary px-5 py-2.5 transition-colors disabled:opacity-50"
                    >
                      {busy ? <Loader2 className="w-4 h-4 animate-spin" /> : null}
                      Connect
                    </button>
                    </div>
                  </details>
                  {err && (
                    <div className="flex items-center gap-2 text-rose-600 text-sm">
                      <AlertCircle className="w-4 h-4 shrink-0" /> {err}
                    </div>
                  )}
                  <button
                    type="button"
                    onClick={() => {
                      capture("onboarding_step_skipped", { step: 1 });
                      setStep(2);
                    }}
                    className="text-xs text-stone-400 hover:text-stone-700 text-left mt-1 underline"
                  >
                    Skip for now →
                  </button>
                </div>
              )}
            </div>
          </div>
        </div>

        {/* Step 2: Shared Model Credential */}
        <div
          className={`bg-white shadow-2xs p-6 transition-all duration-300 ${
            step === 2 ? "border-sand-200 shadow-[0_0_24px_-4px_rgba(147,198,69,0.35)] scale-[1.01]" : step > 2 ? "border-emerald-200 bg-emerald-50" : "opacity-60"
          }`}
        >
          <div className="flex items-start gap-4">
            <div
              className={`flex items-center justify-center w-8 h-8 rounded-full text-sm font-bold shrink-0 ${
                step > 2 ? "bg-kiwi-500 text-white" : step === 2 ? "bg-sky-600 text-white" : "bg-sand-200 text-stone-500"
              }`}
            >
              {step > 2 ? <CheckCircle2 className="w-5 h-5" /> : "2"}
            </div>
            <div className="flex-1 min-w-0">
              <div className="flex items-center justify-between">
                <h2 className="text-xl font-medium text-stone-900 mb-1 flex items-center gap-2">
                  <Key className="w-5 h-5 text-stone-500" /> Model Credentials
                </h2>
                {step > 2 && (
                  <button type="button" onClick={() => setStep(2)} className="text-xs text-stone-400 hover:text-stone-700 underline">
                    Edit
                  </button>
                )}
              </div>
              <p className="text-stone-500 text-sm mb-4">
                Kiwi provides access to hosted models with a daily quota, but you can also bring your own key. Add an Anthropic, Gemini or OpenAI key to bypass quotas and power the planner and worker agents.
              </p>
              {step === 2 && (
                <div className="flex flex-col gap-3 max-w-md pt-2">
                  <div className="flex items-center gap-2">
                    <select
                      value={modelProvider}
                      onChange={(e) => setModelProvider(e.target.value)}
                      className="field text-sm w-36 py-2"
                    >
                      {MODEL_PROVIDERS.map((p) => (
                        <option key={p.key} value={p.key}>{p.label}</option>
                      ))}
                    </select>
                    <input
                      type="password"
                      value={modelKey}
                      onChange={(e) => setModelKey(e.target.value)}
                      placeholder={MODEL_PROVIDERS.find((p) => p.key === modelProvider)?.placeholder}
                      className="flex-1 field text-sm"
                    />
                  </div>
                  {err && (
                    <div className="flex items-center gap-2 text-rose-600 text-sm">
                      <AlertCircle className="w-4 h-4 shrink-0" /> {err}
                    </div>
                  )}
                  <div className="flex items-center gap-3 pt-2">
                    <button
                      onClick={handleSaveModelKey}
                      disabled={busy}
                      className="flex items-center gap-2 btn-primary px-6 py-2.5 transition-colors disabled:opacity-50"
                    >
                      {busy ? <Loader2 className="w-4 h-4 animate-spin" /> : null}
                      Save &amp; Continue
                    </button>
                    <button
                      type="button"
                      onClick={() => {
                        capture("onboarding_step_skipped", { step: 2 });
                        setStep(3);
                      }}
                      className="text-xs text-stone-500 hover:text-stone-900 transition-colors"
                    >
                      Skip (use default)
                    </button>
                  </div>
                </div>
              )}
            </div>
          </div>
        </div>

        {/* Step 3: Starter Task Launcher */}
        <div
          className={`bg-white shadow-2xs p-6 transition-all duration-300 ${
            step === 3 ? "border-sand-200 shadow-[0_0_24px_-4px_rgba(147,198,69,0.35)] scale-[1.01]" : "opacity-60"
          }`}
        >
          <div className="flex items-start gap-4">
            <div
              className={`flex items-center justify-center w-8 h-8 rounded-full text-sm font-bold shrink-0 ${
                step === 3 ? "bg-kiwi-500 text-white" : "bg-sand-200 text-stone-500"
              }`}
            >
              3
            </div>
            <div className="flex-1 min-w-0">
              <h2 className="text-xl font-medium text-stone-900 mb-1 flex items-center gap-2">
                <Rocket className="w-5 h-5 text-[#93C645]" /> Launch Your First Task
              </h2>
              <p className="text-stone-500 text-sm mb-4">
                Choose a starter goal template below to pre-fill the command center composer and kick off your first agent swarm.
              </p>
              {step === 3 && (
                <div className="flex flex-col gap-4 pt-2">
                  <div className="grid grid-cols-1 md:grid-cols-3 gap-3">
                    {STARTER_TASKS.map((t) => {
                      const isSelected = selectedStarter === t.id;
                      return (
                        <div
                          key={t.id}
                          onClick={() => setSelectedStarter(t.id)}
                          className={`p-3.5 rounded-xl border text-left cursor-pointer transition-all ${
                            isSelected
                              ? "border-[#93C645]/50 bg-[#93C645]/10 text-stone-900"
                              : "border-sand-200 bg-sand-50 text-stone-500 hover:text-stone-800 hover:bg-sand-50"
                          }`}
                        >
                          <div className="flex items-center gap-2 mb-1.5 font-semibold text-xs text-stone-900">
                            <Sparkles className="w-3.5 h-3.5 text-[#93C645]" />
                            {t.title}
                          </div>
                          <p className="text-[11px] leading-relaxed line-clamp-3">{t.description}</p>
                        </div>
                      );
                    })}
                  </div>

                  {repos.length > 0 ? (
                    <label className="flex flex-col gap-1.5">
                      <span className="text-[10px] font-bold text-stone-400 uppercase tracking-widest">
                        Repository
                      </span>
                      <select
                        value={starterRepo}
                        onChange={e => setStarterRepo(e.target.value)}
                        className="field text-sm max-w-md"
                      >
                        {repos.map(r => (
                          <option key={r.full_name} value={r.url}>{r.full_name}</option>
                        ))}
                      </select>
                    </label>
                  ) : (
                    <p className="text-xs text-amber-400/90">
                      No repositories yet. Connect GitHub in step 1, then pick one here.
                    </p>
                  )}

                  <div className="flex items-center gap-3 pt-3">
                    <button
                      onClick={() => {
                        const starterObj = STARTER_TASKS.find((s) => s.id === selectedStarter);
                        handleLaunchStarter(starterObj?.task || STARTER_TASKS[0].task, starterRepo);
                      }}
                      disabled={!starterRepo}
                      className="flex items-center gap-2 btn-primary px-6 py-2.5 transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
                    >
                      <span>Open in composer</span>
                      <ChevronRight className="w-4 h-4" />
                    </button>
                    <button
                      onClick={() => {
                        capture("onboarding_step_skipped", { step: 3 });
                        handleLaunchStarter("");
                      }}
                      className="text-xs text-stone-500 hover:text-stone-900 transition-colors"
                    >
                      Skip to dashboard
                    </button>
                  </div>
                </div>
              )}
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}
