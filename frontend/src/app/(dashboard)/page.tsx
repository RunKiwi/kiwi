"use client";

import { useEffect, useState } from "react";
import { useFleetStore } from "@/store/useFleetStore";
import { Activity, Clock, CheckCircle2, XCircle, Loader2, GitPullRequest, Bot, Rocket, AlertCircle, ChevronDown, Server, ExternalLink } from "lucide-react";
import { TaskDrawer } from "@/components/TaskDrawer";
import { client, BUILTIN_MODELS, DEFAULT_PLANNER_MODEL, DEFAULT_WORKER_MODEL, type Fleet, type ModelEntry, type GithubRepo } from "@/lib/api";

export default function CommandCenter() {
  const { jobs, loadJobs } = useFleetStore();
  const [activeDrawerTaskId, setActiveDrawerTaskId] = useState<string | null>(null);
  // Which job's PR list popover is open (job_id), if any.
  const [openPrJob, setOpenPrJob] = useState<string | null>(null);

  // Form State — only task + repo are required. Everything else is a hint.
  const [task, setTask] = useState("");
  const [repoUrl, setRepoUrl] = useState("");
  const [fleetId, setFleetId] = useState("");
  const [plannerModel, setPlannerModel] = useState(DEFAULT_PLANNER_MODEL);
  const [workerModel, setWorkerModel] = useState(DEFAULT_WORKER_MODEL);
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
        model: workerModel,
        planner_model: plannerModel,
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
      case 'QUEUED': return <Loader2 className="w-4 h-4 text-amber-400 animate-spin" />;
      case 'SUCCEEDED': return <CheckCircle2 className="w-4 h-4 text-green-400" />;
      case 'FAILED': return <XCircle className="w-4 h-4 text-red-400" />;
      default: return null;
    }
  };

  const getPhaseColor = (phase: string) => {
    switch (phase) {
      case 'RUNNING': return 'bg-blue-500/10 border-blue-500/30 text-blue-300';
      case 'QUEUED': return 'bg-amber-500/10 border-amber-500/30 text-amber-300';
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

  const prLabel = (url: string) => {
    // Render a compact "repo#123" from a GitHub PR URL when possible.
    const m = url.match(/github\.com\/([^/]+)\/([^/]+)\/pull\/(\d+)/);
    return m ? `${m[2]}#${m[3]}` : url.replace(/^https?:\/\//, "");
  };

  const fieldClass = "field text-sm";
  const labelClass = "block text-[10px] font-bold text-zinc-500 uppercase tracking-widest mb-2";

  return (
    <div className="p-8 max-w-6xl mx-auto h-full flex flex-col">
      <div className="mb-8">
        <p className="eyebrow mb-3"><span className="dot"></span> Command Center</p>
        <h1 className="text-[32px] font-semibold tracking-tight text-white mb-2">What should the swarm build?</h1>
        <p className="text-zinc-400 max-w-2xl">Describe the goal in plain English. Kiwi plans it, runs a swarm of agents, and opens one verified pull request — everything else is optional.</p>
      </div>

      {/* Composer */}
      <div className="glass-panel mb-6 flex flex-col relative z-20 overflow-visible">
        <div className="p-6 pb-4 relative z-10 flex flex-col gap-4">
          {/* Task — the hero input */}
          <div>
            <label htmlFor="task" className={labelClass}>Task</label>
            <textarea
              id="task"
              value={task}
              onChange={(e) => setTask(e.target.value)}
              placeholder="Describe what to build or fix, e.g. “The /api/report endpoint returns stale data — fix it and add a test.”"
              className="field rounded-xl px-4 py-3.5 resize-none min-h-[120px] text-base leading-relaxed"
            />
          </div>

          <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
            {/* Repository */}
            <div className="lg:col-span-2">
              <label className={labelClass}>Repository</label>
              {repos.length > 0 ? (
                <div className="flex gap-2">
                  <select onChange={e => onPickRepo(e.target.value)} className={fieldClass} defaultValue="">
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
              <select value={fleetId} onChange={e => setFleetId(e.target.value)} className={fieldClass}>
                <option value="">Any available fleet</option>
                {fleets.map(f => <option key={f.id} value={f.id}>{f.name} · {f.type === "byoc" ? "BYOC" : "Managed"}</option>)}
              </select>
            </div>

            {/* Planner & verifier model */}
            <div>
              <label className={labelClass}>Planner &amp; verifier</label>
              <select value={plannerModel} onChange={e => setPlannerModel(e.target.value)} className={fieldClass}>
                {modelOptions.map(m => <option key={m} value={m}>{m}</option>)}
              </select>
            </div>

            {/* Worker model */}
            <div>
              <label className={labelClass}>Worker</label>
              <select value={workerModel} onChange={e => setWorkerModel(e.target.value)} className={fieldClass}>
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

        <div className="flex items-center justify-between gap-4 px-6 pb-5 pt-3 relative z-10 border-t border-white/5">
          <div className="flex-1 min-w-0">
            {submitError && (
              <div className="flex items-center gap-2 text-red-400 text-sm">
                <AlertCircle className="w-4 h-4 shrink-0" />
                {submitError.includes("402") || submitError.toLowerCase().includes("activate") || submitError.toLowerCase().includes("payment required") ? (
                  <span>
                    Your organization is inactive. You can preview tasks, but you must
                    <a href="/settings#activation" className="underline ml-1 font-medium hover:text-white">activate to run</a>.
                  </span>
                ) : (
                  <span>{submitError}</span>
                )}
              </div>
            )}
            {submitSuccess && (
              <div className="flex items-center gap-2 text-green-400 text-sm">
                <CheckCircle2 className="w-4 h-4 shrink-0" />
                Launched — <button className="underline" onClick={() => setActiveDrawerTaskId(submitSuccess)}>{submitSuccess}</button>
              </div>
            )}
          </div>
          <button onClick={handleSubmit} disabled={isSubmitting} className="btn-primary px-6 py-2.5 shrink-0">
            {isSubmitting ? <Loader2 className="w-4 h-4 animate-spin" /> : <Rocket className="w-4 h-4" />}
            {isSubmitting ? 'Launching…' : 'Launch'}
          </button>
        </div>
      </div>

      {/* Grid of Jobs */}
      {jobs.length === 0 ? (
        <div className="flex flex-col items-center justify-center text-center py-20 text-zinc-500">
          <Server className="w-10 h-10 mb-3 text-zinc-700" />
          No jobs yet — describe a goal above and launch your first one.
        </div>
      ) : (
        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4 gap-4 pb-32 relative z-10">
          {jobs.map(job => (
            <div key={job.job_id} role="button" tabIndex={0}
              onClick={() => setActiveDrawerTaskId(job.job_id)}
              onKeyDown={(e) => { if (e.key === "Enter") setActiveDrawerTaskId(job.job_id); }}
              className={getCardStyle(job.status)}>
              <div className="flex items-start justify-between mb-3">
                <span className="font-mono text-xs text-zinc-500 group-hover:text-white transition-colors">{job.job_id}</span>
                <div className={`flex items-center gap-1.5 px-2 py-0.5 rounded-full border text-[10px] uppercase font-bold tracking-wider ${getPhaseColor(job.status)}`}>
                  {getPhaseIcon(job.status)}{job.status}
                </div>
              </div>
              <h3 className="text-sm font-medium text-white mb-6 line-clamp-2 leading-snug flex-1">Job {job.job_id}</h3>
              <div className="pt-3 border-t border-white/5 mt-auto flex items-center justify-between text-xs text-zinc-400">
                <div className="flex items-center gap-4">
                  <div className="flex items-center gap-1.5" title={`${job.task_count} tasks`}>
                    <Bot className="w-3.5 h-3.5 text-zinc-500" /><span className="font-mono text-zinc-300">{job.task_count}</span>
                  </div>
                  {job.pr_urls && job.pr_urls.length > 0 && (
                    <div className="relative">
                      <button
                        onClick={(e) => { e.stopPropagation(); setOpenPrJob(openPrJob === job.job_id ? null : job.job_id); }}
                        className="flex items-center gap-1 text-green-400 hover:text-green-300 transition-colors"
                        title={`${job.pr_urls.length} pull request${job.pr_urls.length > 1 ? "s" : ""}`}
                      >
                        <GitPullRequest className="w-3.5 h-3.5" /><span className="font-mono">{job.pr_urls.length}</span>
                      </button>
                      {openPrJob === job.job_id && (
                        <div
                          onClick={(e) => e.stopPropagation()}
                          className="absolute bottom-6 left-0 z-30 w-64 bg-[#0E1A24] border border-white/10 rounded-xl shadow-2xl p-1.5 flex flex-col gap-0.5"
                        >
                          <div className="px-2 py-1 text-[10px] uppercase tracking-widest text-zinc-500">Pull requests</div>
                          {job.pr_urls.map((url) => (
                            <a key={url} href={url} target="_blank" rel="noreferrer"
                              className="flex items-center gap-2 px-2 py-1.5 rounded-md text-xs text-zinc-200 hover:bg-white/5">
                              <GitPullRequest className="w-3.5 h-3.5 text-green-400 shrink-0" />
                              <span className="font-mono truncate flex-1">{prLabel(url)}</span>
                              <ExternalLink className="w-3 h-3 text-zinc-500 shrink-0" />
                            </a>
                          ))}
                        </div>
                      )}
                    </div>
                  )}
                </div>
                <div className="flex items-center gap-1.5">
                  <Clock className="w-3 h-3 text-zinc-500" /><span>{new Date(job.created_at).toLocaleTimeString()}</span>
                </div>
              </div>
            </div>
          ))}
        </div>
      )}

      <TaskDrawer taskId={activeDrawerTaskId} onClose={() => setActiveDrawerTaskId(null)} />
    </div>
  );
}
