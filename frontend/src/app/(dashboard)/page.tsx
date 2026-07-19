"use client";

import { useEffect, useState } from "react";
import { useFleetStore } from "@/store/useFleetStore";
import { Activity, Clock, CheckCircle2, XCircle, Loader2, GitPullRequest, Bot, Send, AlertCircle, ChevronDown, Server } from "lucide-react";
import { TaskDrawer } from "@/components/TaskDrawer";
import { client, BUILTIN_MODELS, type Fleet, type ModelEntry, type GithubRepo } from "@/lib/api";

export default function GodView() {
  const { jobs, loadJobs } = useFleetStore();
  const [activeDrawerTaskId, setActiveDrawerTaskId] = useState<string | null>(null);

  // Form State — only task + repo are required. Everything else is a hint.
  const [task, setTask] = useState("");
  const [repoUrl, setRepoUrl] = useState("");
  const [fleetId, setFleetId] = useState("");
  const [model, setModel] = useState(BUILTIN_MODELS[0]);
  const [ref, setRef] = useState("main");
  const [file, setFile] = useState("");
  const [testCmd, setTestCmd] = useState("");
  const [maxWorkers, setMaxWorkers] = useState(1);
  const [showAdvanced, setShowAdvanced] = useState(false);

  // Options loaded from the control plane.
  const [fleets, setFleets] = useState<Fleet[]>([]);
  const [customModels, setCustomModels] = useState<ModelEntry[]>([]);
  const [repos, setRepos] = useState<GithubRepo[]>([]);

  const [isSubmitting, setIsSubmitting] = useState(false);
  const [submitError, setSubmitError] = useState("");
  const [submitSuccess, setSubmitSuccess] = useState<string | null>(null);

  useEffect(() => {
    loadJobs();
    const interval = setInterval(() => loadJobs(), 3000);
    return () => clearInterval(interval);
  }, [loadJobs]);

  useEffect(() => {
    client.listFleets().then(r => setFleets(r.fleets)).catch(() => {});
    client.listModels().then(r => setCustomModels(r.models)).catch(() => {});
    // GitHub repos are best-effort — only available once the integration is connected.
    client.listGithubRepos().then(r => setRepos(r.repos)).catch(() => {});
  }, []);

  const modelOptions = Array.from(new Set([...BUILTIN_MODELS, ...customModels.map(m => m.name)]));

  const handleSubmit = async () => {
    setSubmitError("");
    setSubmitSuccess(null);
    if (!task.trim() || !repoUrl.trim()) {
      setSubmitError("A task and a repository are required.");
      return;
    }
    setIsSubmitting(true);
    try {
      const resp = await client.submitPlan({
        task,
        repo_url: repoUrl,
        ref: ref || "main",
        file,
        test_cmd: testCmd,
        model,
        max_workers: maxWorkers,
        fleet_id: fleetId,
      });
      setSubmitSuccess(resp.job_id);
      setTask("");
      loadJobs();
    } catch (err) {
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

  const getPhaseIcon = (phase: string) => {
    switch (phase) {
      case 'RUNNING': return <Activity className="w-4 h-4 text-blue-400" />;
      case 'QUEUED': return <Loader2 className="w-4 h-4 text-purple-400 animate-spin" />;
      case 'SUCCEEDED': return <CheckCircle2 className="w-4 h-4 text-green-400" />;
      case 'FAILED': return <XCircle className="w-4 h-4 text-red-400" />;
      default: return null;
    }
  };

  const getPhaseColor = (phase: string) => {
    switch (phase) {
      case 'RUNNING': return 'bg-blue-500/10 border-blue-500/30 text-blue-300';
      case 'QUEUED': return 'bg-purple-500/10 border-purple-500/30 text-purple-300';
      case 'SUCCEEDED': return 'bg-green-500/10 border-green-500/20 text-green-300';
      case 'FAILED': return 'bg-red-500/10 border-red-500/20 text-red-300';
      default: return 'bg-white/5 border-white/10 text-white';
    }
  };

  const getCardStyle = (phase: string) => {
    const base = "text-left glass-panel p-4 hover:-translate-y-1 hover:shadow-[0_8px_30px_rgba(255,255,255,0.05)] transition-all cursor-pointer group flex flex-col h-full relative ";
    switch (phase) {
      case 'RUNNING': return base + "border-blue-500/30 shadow-[0_0_15px_rgba(59,130,246,0.1)]";
      case 'QUEUED': return base + "animate-breathing";
      case 'SUCCEEDED': return base + "bg-green-950/20 border-green-500/20 shadow-[0_0_15px_rgba(34,197,94,0.05)]";
      case 'FAILED': return base + "bg-red-950/20 border-red-500/20 shadow-[0_0_15px_rgba(239,68,68,0.05)]";
      default: return base;
    }
  };

  const fieldClass = "w-full bg-white/5 border border-white/10 rounded-lg px-3 py-2 text-sm text-white focus:border-purple-500/50 focus:outline-none transition-colors";
  const labelClass = "block text-[10px] font-bold text-zinc-500 uppercase tracking-widest mb-1.5";

  return (
    <div className="p-8 max-w-7xl mx-auto h-full flex flex-col">
      <div className="mb-8">
        <h1 className="text-3xl font-light tracking-tight text-white mb-2">Command Center</h1>
        <p className="text-zinc-400">Describe the goal. Kiwi plans it and dispatches agents — the rest is optional.</p>
      </div>

      {/* Command Bar Form */}
      <div className="bg-black/60 backdrop-blur-xl rounded-2xl mb-8 flex flex-col relative z-20 shadow-[0_0_40px_rgba(0,0,0,0.5)] border border-white/10 overflow-hidden">
        <div className="p-6 pb-4 relative z-10 flex flex-col gap-4">
          <textarea
            value={task}
            onChange={(e) => setTask(e.target.value)}
            placeholder="What would you like the Swarm to build or fix?"
            className="w-full bg-transparent text-white placeholder-zinc-500 focus:outline-none resize-none min-h-[110px] text-lg font-light"
          />

          <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
            {/* Repository */}
            <div className="lg:col-span-2">
              <label className={labelClass}>Repository</label>
              {repos.length > 0 ? (
                <div className="flex gap-2">
                  <select onChange={e => onPickRepo(e.target.value)} className={fieldClass + " bg-zinc-900 appearance-none"} defaultValue="">
                    <option value="" disabled>Select a repo…</option>
                    {repos.map(r => <option key={r.full_name} value={r.full_name}>{r.full_name}{r.private ? " (private)" : ""}</option>)}
                  </select>
                </div>
              ) : (
                <input type="text" value={repoUrl} onChange={e => setRepoUrl(e.target.value)} placeholder="https://github.com/you/repo" className={fieldClass} />
              )}
              {repos.length > 0 && (
                <input type="text" value={repoUrl} onChange={e => setRepoUrl(e.target.value)} placeholder="…or paste a URL" className={fieldClass + " mt-2"} />
              )}
            </div>

            {/* Fleet */}
            <div>
              <label className={labelClass}>Fleet</label>
              <select value={fleetId} onChange={e => setFleetId(e.target.value)} className={fieldClass + " bg-zinc-900 appearance-none"}>
                <option value="">Any available fleet</option>
                {fleets.map(f => <option key={f.id} value={f.id}>{f.name} · {f.type === "byoc" ? "BYOC" : "Self-managed"}</option>)}
              </select>
            </div>

            {/* Model */}
            <div>
              <label className={labelClass}>Model</label>
              <select value={model} onChange={e => setModel(e.target.value)} className={fieldClass + " bg-zinc-900 appearance-none"}>
                {modelOptions.map(m => <option key={m} value={m}>{m}</option>)}
              </select>
            </div>
          </div>

          {/* Advanced (optional) */}
          <button type="button" onClick={() => setShowAdvanced(v => !v)} className="flex items-center gap-1.5 text-xs text-zinc-400 hover:text-white transition-colors w-fit">
            <ChevronDown className={`w-3.5 h-3.5 transition-transform ${showAdvanced ? "rotate-180" : ""}`} />
            Advanced options
          </button>
          {showAdvanced && (
            <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-4 pt-1">
              <div>
                <label className={labelClass}>Target file <span className="text-zinc-600 normal-case font-normal">(optional)</span></label>
                <input type="text" value={file} onChange={e => setFile(e.target.value)} placeholder="let the agent decide" className={fieldClass} />
              </div>
              <div>
                <label className={labelClass}>Test command <span className="text-zinc-600 normal-case font-normal">(optional)</span></label>
                <input type="text" value={testCmd} onChange={e => setTestCmd(e.target.value)} placeholder="e.g. go test ./..." className={fieldClass} />
              </div>
              <div>
                <label className={labelClass}>Git ref</label>
                <input type="text" value={ref} onChange={e => setRef(e.target.value)} placeholder="main" className={fieldClass} />
              </div>
              <div>
                <label className={labelClass}>Max workers</label>
                <input type="number" min="1" max="10" value={maxWorkers} onChange={e => setMaxWorkers(parseInt(e.target.value) || 1)} className={fieldClass} />
              </div>
            </div>
          )}
        </div>

        <div className="flex items-center justify-between px-6 pb-5 pt-2 relative z-10">
          <div className="flex-1">
            {submitError && (
              <div className="flex items-center gap-2 text-red-400 text-sm">
                <AlertCircle className="w-4 h-4" />
                {submitError.includes("402") || submitError.toLowerCase().includes("activate") || submitError.toLowerCase().includes("payment required") ? (
                  <span>
                    Your organization is inactive. You can preview tasks, but you must 
                    <a href="/settings" className="underline ml-1 font-medium hover:text-white">Activate to Run</a>.
                  </span>
                ) : (
                  <span>{submitError}</span>
                )}
              </div>
            )}
            {submitSuccess && (
              <div className="flex items-center gap-2 text-green-400 text-sm">
                <CheckCircle2 className="w-4 h-4" />
                Dispatched — <button className="underline" onClick={() => setActiveDrawerTaskId(submitSuccess)}>{submitSuccess}</button>
              </div>
            )}
          </div>
          <button
            onClick={handleSubmit}
            disabled={isSubmitting}
            className="flex items-center gap-2 bg-white hover:bg-zinc-200 text-black px-5 py-2 rounded-xl font-semibold transition-all shadow-[0_0_20px_rgba(255,255,255,0.15)] shrink-0 disabled:opacity-50 disabled:cursor-not-allowed"
          >
            {isSubmitting ? 'Dispatching...' : 'Dispatch'}
            {!isSubmitting && <Send className="w-4 h-4" />}
          </button>
        </div>
      </div>

      {/* Grid of Jobs */}
      {jobs.length === 0 ? (
        <div className="flex flex-col items-center justify-center text-center py-20 text-zinc-500">
          <Server className="w-10 h-10 mb-3 text-zinc-700" />
          No jobs yet — describe a goal above and dispatch your first one.
        </div>
      ) : (
        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4 gap-4 pb-32 relative z-10">
          {jobs.map(job => (
            <button key={job.job_id} onClick={() => setActiveDrawerTaskId(job.job_id)} className={getCardStyle(job.status)}>
              <div className="flex items-start justify-between mb-3">
                <span className="font-mono text-xs text-zinc-500 group-hover:text-white transition-colors">{job.job_id}</span>
                <div className={`flex items-center gap-1.5 px-2 py-0.5 rounded-full border text-[10px] uppercase font-bold tracking-wider ${getPhaseColor(job.status)}`}>
                  {getPhaseIcon(job.status)}{job.status}
                </div>
              </div>
              <h3 className="text-sm font-medium text-white mb-6 line-clamp-2 leading-snug flex-1">Job: {job.job_id}</h3>
              <div className="pt-3 border-t border-white/5 mt-auto flex items-center justify-between text-xs text-zinc-400">
                <div className="flex items-center gap-4">
                  <div className="flex items-center gap-1.5" title={`${job.task_count} Tasks`}>
                    <Bot className="w-3.5 h-3.5 text-zinc-500" /><span className="font-mono text-zinc-300">{job.task_count}</span>
                  </div>
                  {job.pr_urls && job.pr_urls.length > 0 && (
                    <div className="flex items-center gap-1">
                      <GitPullRequest className="w-3 h-3 text-green-400" /><span className="font-mono text-zinc-300">{job.pr_urls.length}</span>
                    </div>
                  )}
                </div>
                <div className="flex items-center gap-1.5">
                  <Clock className="w-3 h-3 text-zinc-500" /><span>{new Date(job.created_at).toLocaleTimeString()}</span>
                </div>
              </div>
            </button>
          ))}
        </div>
      )}

      <TaskDrawer taskId={activeDrawerTaskId} onClose={() => setActiveDrawerTaskId(null)} />
    </div>
  );
}
