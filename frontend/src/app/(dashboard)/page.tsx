"use client";

import { useEffect, useState } from "react";
import { useFleetStore } from "@/store/useFleetStore";
import { Activity, Clock, CheckCircle2, XCircle, Loader2, GitPullRequest, Bot, ArrowRight, FolderGit2, AlertCircle, ChevronDown, Server, ExternalLink } from "lucide-react";
import { TaskDrawer } from "@/components/TaskDrawer";
import { Select } from "@/components/Select";
import { useRouter } from "next/navigation";
import { client, BUILTIN_MODELS, DEFAULT_PLANNER_MODEL, DEFAULT_WORKER_MODEL, type Fleet, type ModelEntry, type GithubRepo, type UsageResponse, type Integration } from "@/lib/api";

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
  const [u, setU] = useState<UsageResponse | null>(null);

  const [isSubmitting, setIsSubmitting] = useState(false);
  const [submitError, setSubmitError] = useState("");
  const [submitSuccess, setSubmitSuccess] = useState<string | null>(null);

  const router = useRouter();

  useEffect(() => {
    loadJobs();
    const interval = setInterval(() => loadJobs(), 3000);
    return () => clearInterval(interval);
  }, [loadJobs]);

  useEffect(() => {
    client.listFleets().then(r => setFleets(r.fleets)).catch(() => {});
    client.listModels().then(r => setCustomModels(r.models)).catch(() => {});
    client.getUsage().then(setU).catch(() => setU(null));
    // GitHub repos are best-effort — only available once the integration is connected.
    client.listGithubRepos().then(r => setRepos(r.repos)).catch(() => {});

    // Load integrations once, then use them for two things: the first-run
    // onboarding redirect, and the M14 model default (prefer the provider the
    // org actually has a key for, so a BYOK user isn't defaulted to a model
    // they can't call). Jobs are only needed for the first-run check.
    const firstRun = !sessionStorage.getItem("onboarded");
    Promise.all([client.listIntegrations(), firstRun ? client.listJobs() : Promise.resolve(null)])
      .then(([ints, jbs]) => {
        const connected = (key: string) =>
          ints.integrations.some((i: Integration) => i.key === key && i.connected);
        // Default to Gemini when it's the only model key connected.
        if (connected("gemini") && !connected("anthropic")) {
          setPlannerModel("gemini-2.0-flash");
          setWorkerModel("gemini-flash-latest");
        }
        if (firstRun) {
          const hasInt = ints.integrations.some((i: Integration) => i.connected);
          const hasJob = (jbs?.jobs.length ?? 0) > 0;
          if (!hasInt && !hasJob) router.push("/onboarding");
          sessionStorage.setItem("onboarded", "1");
        }
      }).catch(() => {});
  }, [router]);

  // Show the fleet selector only once we positively know the org is not Free
  // (Free work always routes to the shared fleet, so the control is a no-op there).
  const showFleetSelector = !!u && u.plan !== "free";

  // Close the PR popover on Escape or any click outside the popover / its trigger.
  useEffect(() => {
    if (!openPrJob) return;
    const onKey = (e: KeyboardEvent) => { if (e.key === "Escape") setOpenPrJob(null); };
    const onDown = (e: MouseEvent) => {
      const t = e.target as HTMLElement;
      if (t.closest(".pr-popover") || t.closest("[data-pr-trigger]")) return;
      setOpenPrJob(null);
    };
    document.addEventListener("keydown", onKey);
    document.addEventListener("mousedown", onDown);
    return () => {
      document.removeEventListener("keydown", onKey);
      document.removeEventListener("mousedown", onDown);
    };
  }, [openPrJob]);

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

  // The 4 job states. Like the earlier design, each card sits on a neutral
  // near-black base (not navy — navy muddies the tint into grey) so a flat
  // whole-card colour wash reads true. Plus a matching border, badge, and glow.
  const STATUS: Record<string, { label: string; Icon: typeof Activity; color: string; border: string; wash: string; glow: string; spin?: boolean }> = {
    QUEUED: { label: "Queued", Icon: Loader2, color: "#E8A153", border: "rgba(232,161,83,0.32)", wash: "rgba(232,161,83,0.14)", glow: "rgba(232,161,83,0.10)", spin: true },
    RUNNING: { label: "Running", Icon: Activity, color: "#5A9DF5", border: "rgba(59,130,246,0.34)", wash: "rgba(59,130,246,0.15)", glow: "rgba(59,130,246,0.12)" },
    SUCCEEDED: { label: "Succeeded", Icon: CheckCircle2, color: "#93C645", border: "rgba(147,198,69,0.30)", wash: "rgba(147,198,69,0.13)", glow: "rgba(147,198,69,0.09)" },
    FAILED: { label: "Failed", Icon: XCircle, color: "#EF6060", border: "rgba(239,68,68,0.30)", wash: "rgba(239,68,68,0.14)", glow: "rgba(239,68,68,0.09)" },
  };
  // Neutral near-black card base — lets the status wash read as true colour.
  const CARD_BASE = "#0C0D10";
  const statusOf = (s: string) => STATUS[s] ?? STATUS.QUEUED;

  const prLabel = (url: string) => {
    // Render a compact "owner/repo#123" from a GitHub PR URL when possible.
    const m = url.match(/github\.com\/([^/]+)\/([^/]+)\/pull\/(\d+)/);
    return m ? `${m[1]}/${m[2]}#${m[3]}` : url.replace(/^https?:\/\//, "");
  };

  // Job ids are `job_` + 16 hex; show a friendly short form (job_a3f19c…).
  const shortId = (id: string) => (id.length > 12 ? id.slice(0, 10) : id);

  const fieldClass = "field text-sm";
  const labelClass = "block text-[10px] font-bold text-zinc-500 uppercase tracking-widest mb-2";
  // Which repo (full_name) the current repoUrl corresponds to, for the select.
  const selectedRepo = repos.find(r => r.url === repoUrl)?.full_name ?? "";

  return (
    <div className="p-8 max-w-6xl mx-auto h-full flex flex-col">
      <div className="mb-8">
        <p className="eyebrow mb-3"><span className="dot"></span> Tasks</p>
        <h1 className="text-[32px] font-semibold tracking-tight text-white mb-2">What should the swarm build?</h1>
        <p className="text-zinc-400 max-w-2xl">Describe the goal in plain English. Kiwi plans it, runs a swarm of agents, and opens one verified pull request — everything else is optional.</p>
      </div>

      {/* Composer — one compact input with an inline control rail underneath. */}
      <div className="glass-panel mb-6 flex flex-col relative z-20 overflow-visible p-4">
        <textarea
          id="task"
          value={task}
          onChange={(e) => setTask(e.target.value)}
          placeholder="Describe what to build or fix, e.g. “The /api/report endpoint returns stale data — fix it and add a test.”"
          className="field border-0 bg-transparent rounded-lg px-2 py-1.5 resize-none min-h-[76px] text-base leading-relaxed focus:shadow-none"
        />

        {/* Control rail: repo · plan · worker chips, then Launch. */}
        <div className="flex flex-wrap items-center gap-2 pt-3 mt-1 border-t border-white/5">
          {/* Repository — searchable when repos are available, else a URL input. */}
          {repos.length > 0 ? (
            <Select
              variant="chip" searchable label="Repo" ariaLabel="Repository"
              icon={<FolderGit2 className="w-3.5 h-3.5 text-zinc-400 shrink-0" />}
              value={selectedRepo} onChange={onPickRepo} placeholder="Select…"
              options={repos.map(r => ({ value: r.full_name, label: r.full_name, hint: r.private ? "private" : undefined }))}
            />
          ) : (
            <label className="chip">
              <FolderGit2 className="w-3.5 h-3.5 text-zinc-400 shrink-0" />
              <span className="k">Repo</span>
              <input type="text" value={repoUrl} onChange={e => setRepoUrl(e.target.value)} placeholder="github.com/you/repo"
                className="bg-transparent outline-none border-0 text-sm font-mono text-white placeholder:text-zinc-600 w-[190px]" />
            </label>
          )}

          {/* Planner & verifier */}
          <Select
            variant="chip" searchable label="Plan" ariaLabel="Planner & verifier model"
            icon={<span className="pdot" style={{ background: "#93C645" }} />}
            value={plannerModel} onChange={setPlannerModel}
            options={modelOptions.map(m => ({ value: m, label: m }))}
          />

          {/* Worker */}
          <Select
            variant="chip" searchable label="Work" ariaLabel="Worker model"
            icon={<span className="pdot" style={{ background: "#E8A153" }} />}
            value={workerModel} onChange={setWorkerModel}
            options={modelOptions.map(m => ({ value: m, label: m }))}
          />

          {/* Advanced toggle */}
          <button type="button" onClick={() => setShowAdvanced(v => !v)}
            className="chip cursor-pointer text-zinc-400 hover:text-white">
            <ChevronDown className={`w-3.5 h-3.5 transition-transform ${showAdvanced ? "rotate-180" : ""}`} />
            <span className="text-xs">Advanced</span>
          </button>

          <div className="flex-1" />

          <button onClick={handleSubmit} disabled={isSubmitting} className="btn-primary px-5 py-2 shrink-0">
            {isSubmitting ? <><Loader2 className="w-4 h-4 animate-spin" /> Launching…</> : <>Launch <ArrowRight className="w-4 h-4" /></>}
          </button>
        </div>

        {/* Advanced options — hidden by default to keep the composer compact. */}
        {showAdvanced && (
          <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4 pt-4 mt-3 border-t border-white/5">
            {showFleetSelector && (
              <div>
                <label className={labelClass}>Fleet</label>
                <Select
                  ariaLabel="Fleet" value={fleetId} onChange={setFleetId}
                  options={[{ value: "", label: "Any available fleet" }, ...fleets.map(f => ({ value: f.id, label: f.name, hint: f.type === "byoc" ? "BYOC" : "Managed" }))]}
                />
              </div>
            )}
            {repos.length > 0 && (
              <div>
                <label className={labelClass}>Repository URL <span className="text-zinc-600 normal-case font-normal">(override)</span></label>
                <input type="text" value={repoUrl} onChange={e => setRepoUrl(e.target.value)} placeholder="…or paste a URL" className={fieldClass} />
              </div>
            )}
            <div>
              <label className={labelClass}>Git ref</label>
              <input type="text" value={ref} onChange={e => setRef(e.target.value)} placeholder="main" className={fieldClass} />
            </div>
            <div>
              <label className={labelClass}>Target file <span className="text-zinc-600 normal-case font-normal">(optional)</span></label>
              <input type="text" value={file} onChange={e => setFile(e.target.value)} placeholder="let the agent decide" className={fieldClass} />
            </div>
            <div>
              <label className={labelClass}>Test command <span className="text-zinc-600 normal-case font-normal">(optional)</span></label>
              <input type="text" value={testCmd} onChange={e => setTestCmd(e.target.value)} placeholder="e.g. go test ./..." className={fieldClass} />
            </div>
            <div>
              <label className={labelClass}>Max workers</label>
              <input type="number" min="1" max="10" value={maxWorkers} onChange={e => setMaxWorkers(parseInt(e.target.value) || 1)} className={fieldClass} />
            </div>
          </div>
        )}

        {/* Status line */}
        {(submitError || submitSuccess) && (
          <div className="pt-3 mt-1">
            {submitError && (
              <div className="flex items-center gap-2 text-red-400 text-sm">
                <AlertCircle className="w-4 h-4 shrink-0" />
                {/* Activation is a paid-tier concept; a Free org runs without it and
                    never hits this gate, so only surface the "activate" nudge for a
                    known non-Free plan — Free always sees the real server error. */}
                {u?.plan !== "free" && (submitError.includes("402") || submitError.toLowerCase().includes("activate") || submitError.toLowerCase().includes("payment required")) ? (
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
                Launched — <button className="underline" onClick={() => setActiveDrawerTaskId(submitSuccess)}>{shortId(submitSuccess)}</button>
              </div>
            )}
          </div>
        )}
      </div>

      {/* Grid of Jobs */}
      {jobs.length === 0 ? (
        <div className="flex flex-col items-center justify-center text-center py-20 text-zinc-500">
          <Server className="w-10 h-10 mb-3 text-zinc-700" />
          No jobs yet — describe a goal above and launch your first one.
        </div>
      ) : (
        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4 gap-4 pb-32 relative z-10">
          {jobs.map(job => {
            const m = statusOf(job.status);
            const Icon = m.Icon;
            return (
            <div key={job.job_id} role="button" tabIndex={0}
              onClick={() => setActiveDrawerTaskId(job.job_id)}
              onKeyDown={(e) => { if (e.key === "Enter") setActiveDrawerTaskId(job.job_id); }}
              style={{
                background: `linear-gradient(0deg, ${m.wash}, ${m.wash}), ${CARD_BASE}`,
                borderColor: m.border,
                boxShadow: `0 4px 30px rgba(0,0,0,0.5), 0 0 15px -2px ${m.glow}`,
              }}
              className="group relative text-left rounded-2xl p-4 border flex flex-col h-full cursor-pointer card-hover">
              <div className="flex items-center justify-between gap-2 mb-3">
                <span title={job.job_id} className="font-mono text-xs text-zinc-500 truncate min-w-0 group-hover:text-zinc-300 transition-colors">{shortId(job.job_id)}</span>
                <div className="inline-flex items-center gap-1.5 rounded-full border px-2 py-0.5 text-[10px] font-bold uppercase tracking-wider shrink-0"
                  style={{ color: m.color, borderColor: m.border, background: m.wash }}>
                  <Icon className={`w-3 h-3 shrink-0 ${m.spin ? "animate-spin" : ""}`} />
                  {m.label}
                </div>
              </div>

              <h3 className="text-sm font-medium text-white mb-5 line-clamp-2 leading-snug flex-1">
                {job.task?.trim() || `Job ${shortId(job.job_id)}`}
              </h3>

              <div className="pt-3 border-t border-white/5 mt-auto flex items-center justify-between gap-2 text-xs text-zinc-400">
                {/* Left: a compact PR count that opens a polished popover; else repo, else time. */}
                <div className="min-w-0 relative">
                  {job.pr_urls && job.pr_urls.length > 0 ? (
                    <>
                      <button
                        data-pr-trigger
                        onClick={(e) => { e.stopPropagation(); setOpenPrJob(openPrJob === job.job_id ? null : job.job_id); }}
                        className="flex items-center gap-1.5 rounded-full border border-green-500/25 bg-green-500/10 pl-2 pr-2.5 py-1 text-green-300 hover:text-green-200 hover:border-green-500/40 hover:bg-green-500/15 transition-colors"
                        title={`${job.pr_urls.length} pull request${job.pr_urls.length > 1 ? "s" : ""}`}
                        aria-expanded={openPrJob === job.job_id}
                      >
                        <GitPullRequest className="w-3.5 h-3.5 shrink-0" />
                        <span className="font-mono text-[11px] font-semibold">{job.pr_urls.length}</span>
                        <span className="text-[11px]">PR{job.pr_urls.length > 1 ? "s" : ""}</span>
                      </button>
                      {openPrJob === job.job_id && (
                        <div
                          onClick={(e) => e.stopPropagation()}
                          className="pr-popover absolute bottom-full left-0 mb-2 z-50 w-72 rounded-xl border border-white/10 bg-[#0E1A24]/95 backdrop-blur-xl shadow-[0_24px_60px_-16px_rgba(0,0,0,0.85)] p-1.5"
                        >
                          <div className="flex items-center gap-2 px-2 py-1.5 mb-1 border-b border-white/5">
                            <GitPullRequest className="w-3.5 h-3.5 text-green-400 shrink-0" />
                            <span className="text-[11px] font-semibold uppercase tracking-wider text-zinc-300">Pull requests</span>
                            <span className="ml-auto font-mono text-[10px] text-zinc-400 bg-white/5 rounded-full px-1.5 py-0.5">{job.pr_urls.length}</span>
                          </div>
                          <div className="flex flex-col gap-0.5 max-h-56 overflow-y-auto">
                            {job.pr_urls.map((url) => (
                              <a key={url} href={url} target="_blank" rel="noreferrer"
                                onClick={() => setOpenPrJob(null)}
                                className="group/pr flex items-center gap-2.5 px-2 py-1.5 rounded-lg text-xs text-zinc-200 hover:bg-white/[0.06] transition-colors">
                                <span className="w-6 h-6 rounded-md bg-green-500/10 border border-green-500/20 flex items-center justify-center shrink-0">
                                  <GitPullRequest className="w-3.5 h-3.5 text-green-400" />
                                </span>
                                <span className="font-mono truncate flex-1">{prLabel(url)}</span>
                                <ExternalLink className="w-3.5 h-3.5 text-zinc-500 group-hover/pr:text-zinc-300 transition-colors shrink-0" />
                              </a>
                            ))}
                          </div>
                        </div>
                      )}
                    </>
                  ) : job.repo ? (
                    <span className="flex items-center gap-1.5 font-mono text-zinc-400 truncate">
                      <FolderGit2 className="w-3.5 h-3.5 text-zinc-500 shrink-0" />{job.repo}
                    </span>
                  ) : (
                    <span className="flex items-center gap-1.5 text-zinc-500">
                      <Clock className="w-3 h-3 shrink-0" />{new Date(job.created_at).toLocaleTimeString()}
                    </span>
                  )}
                </div>
                {/* Right: task count. */}
                <div className="flex items-center gap-1.5 shrink-0" title={`${job.task_count} task${job.task_count !== 1 ? "s" : ""}`}>
                  <Bot className="w-3.5 h-3.5 text-zinc-500" />
                  <span>{job.task_count} task{job.task_count !== 1 ? "s" : ""}</span>
                </div>
              </div>
            </div>
            );
          })}
        </div>
      )}

      <TaskDrawer taskId={activeDrawerTaskId} onClose={() => setActiveDrawerTaskId(null)} />
    </div>
  );
}
