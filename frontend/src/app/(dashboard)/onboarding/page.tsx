"use client";

import { useState, useEffect } from "react";
import { CheckCircle2, ChevronRight, Loader2, AlertCircle, Key, FolderGit2, Rocket, Sparkles } from "lucide-react";
import { useRouter } from "next/navigation";
import { client, type Integration, type GithubRepo } from "@/lib/api";

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
        <h1 className="text-4xl font-semibold tracking-tight text-white mb-3">Welcome to Kiwi</h1>
        <p className="text-zinc-400 text-base max-w-xl mx-auto">
          Set up your repository connection, model credentials, and launch your first swarm task in 3 simple steps.
        </p>
      </div>

      <div className="space-y-6">
        {/* Step 1: Connect Repository */}
        <div
          className={`glass-panel p-6 transition-all duration-300 ${
            step === 1 ? "border-white/20 shadow-[0_0_30px_rgba(255,255,255,0.08)] scale-[1.01]" : step > 1 ? "border-green-500/20 bg-green-950/10" : "opacity-60"
          }`}
        >
          <div className="flex items-start gap-4">
            <div
              className={`flex items-center justify-center w-8 h-8 rounded-full text-sm font-bold shrink-0 ${
                step > 1 ? "bg-[#93C645] text-black" : "bg-white text-black"
              }`}
            >
              {step > 1 ? <CheckCircle2 className="w-5 h-5" /> : "1"}
            </div>
            <div className="flex-1 min-w-0">
              <div className="flex items-center justify-between">
                <h2 className="text-xl font-medium text-white mb-1 flex items-center gap-2">
                  <FolderGit2 className="w-5 h-5 text-zinc-400" /> Connect your Repository
                </h2>
                {step > 1 && (
                  <button type="button" onClick={() => setStep(1)} className="text-xs text-zinc-500 hover:text-zinc-300 underline">
                    Edit
                  </button>
                )}
              </div>
              <p className="text-zinc-400 text-sm mb-4">
                Link your codebase so Kiwi agents can analyze, plan, and submit pull requests. Provide a GitHub Personal Access Token (`repo` scope).
              </p>
              {step === 1 && (
                <div className="flex flex-col gap-3 max-w-md pt-2">
                  <div className="flex gap-2">
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
                  {err && (
                    <div className="flex items-center gap-2 text-red-400 text-sm">
                      <AlertCircle className="w-4 h-4 shrink-0" /> {err}
                    </div>
                  )}
                  <button
                    type="button"
                    onClick={() => setStep(2)}
                    className="text-xs text-zinc-500 hover:text-zinc-300 text-left mt-1 underline"
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
          className={`glass-panel p-6 transition-all duration-300 ${
            step === 2 ? "border-white/20 shadow-[0_0_30px_rgba(255,255,255,0.08)] scale-[1.01]" : step > 2 ? "border-green-500/20 bg-green-950/10" : "opacity-60"
          }`}
        >
          <div className="flex items-start gap-4">
            <div
              className={`flex items-center justify-center w-8 h-8 rounded-full text-sm font-bold shrink-0 ${
                step > 2 ? "bg-[#93C645] text-black" : step === 2 ? "bg-blue-500 text-white" : "bg-white/20 text-white"
              }`}
            >
              {step > 2 ? <CheckCircle2 className="w-5 h-5" /> : "2"}
            </div>
            <div className="flex-1 min-w-0">
              <div className="flex items-center justify-between">
                <h2 className="text-xl font-medium text-white mb-1 flex items-center gap-2">
                  <Key className="w-5 h-5 text-zinc-400" /> Model Credentials
                </h2>
                {step > 2 && (
                  <button type="button" onClick={() => setStep(2)} className="text-xs text-zinc-500 hover:text-zinc-300 underline">
                    Edit
                  </button>
                )}
              </div>
              <p className="text-zinc-400 text-sm mb-4">
                Kiwi runs on your own model key. Add an Anthropic, Gemini or OpenAI key to power the planner and worker agents.
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
                    <div className="flex items-center gap-2 text-red-400 text-sm">
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
                      onClick={() => setStep(3)}
                      className="text-xs text-zinc-400 hover:text-white transition-colors"
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
          className={`glass-panel p-6 transition-all duration-300 ${
            step === 3 ? "border-white/20 shadow-[0_0_30px_rgba(255,255,255,0.08)] scale-[1.01]" : "opacity-60"
          }`}
        >
          <div className="flex items-start gap-4">
            <div
              className={`flex items-center justify-center w-8 h-8 rounded-full text-sm font-bold shrink-0 ${
                step === 3 ? "bg-[#93C645] text-black" : "bg-white/20 text-white"
              }`}
            >
              3
            </div>
            <div className="flex-1 min-w-0">
              <h2 className="text-xl font-medium text-white mb-1 flex items-center gap-2">
                <Rocket className="w-5 h-5 text-[#93C645]" /> Launch Your First Task
              </h2>
              <p className="text-zinc-400 text-sm mb-4">
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
                              ? "border-[#93C645]/50 bg-[#93C645]/10 text-white"
                              : "border-white/10 bg-black/20 text-zinc-400 hover:text-zinc-200 hover:bg-white/5"
                          }`}
                        >
                          <div className="flex items-center gap-2 mb-1.5 font-semibold text-xs text-white">
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
                      <span className="text-[10px] font-bold text-zinc-500 uppercase tracking-widest">
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
                      onClick={() => handleLaunchStarter("")}
                      className="text-xs text-zinc-400 hover:text-white transition-colors"
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
