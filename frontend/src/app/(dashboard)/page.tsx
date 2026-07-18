"use client";

import { useEffect, useState } from "react";
import { useFleetStore } from "@/store/useFleetStore";
import { Activity, Clock, CheckCircle2, XCircle, Loader2, GitPullRequest, Bot } from "lucide-react";
import { TaskDrawer } from "@/components/TaskDrawer";

export default function GodView() {
  const { jobs, loadJobs } = useFleetStore();
  const [activeDrawerTaskId, setActiveDrawerTaskId] = useState<string | null>(null);

  useEffect(() => {
    loadJobs();
  }, [loadJobs]);

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
      case 'RUNNING': 
        return base + "border-blue-500/30 shadow-[0_0_15px_rgba(59,130,246,0.1)] before:absolute before:inset-0 before:-z-10 before:rounded-2xl before:bg-[linear-gradient(90deg,transparent,rgba(59,130,246,0.05),transparent)] before:bg-[length:200%_100%] before:animate-shimmer";
      case 'QUEUED': 
        return base + "animate-breathing";
      case 'SUCCEEDED': 
        return base + "bg-green-950/20 border-green-500/20 shadow-[0_0_15px_rgba(34,197,94,0.05)]";
      case 'FAILED': 
        return base + "bg-red-950/20 border-red-500/20 shadow-[0_0_15px_rgba(239,68,68,0.05)]";
      default: 
        return base;
    }
  };

  return (
    <div className="p-8 max-w-7xl mx-auto h-full flex flex-col">
      <div className="mb-8">
        <h1 className="text-3xl font-light tracking-tight text-white mb-2">Command Center</h1>
        <p className="text-zinc-400">Command your agents and monitor high-level goals across the Swarm.</p>
      </div>

      {/* Grid of Tasks */}
      <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4 gap-4 pb-32 relative z-10">
        {jobs.map(job => (
          <button 
            key={job.job_id} 
            onClick={() => setActiveDrawerTaskId(job.job_id)}
            className={getCardStyle(job.status)}
          >
            <div className="flex items-start justify-between mb-3">
              <span className="font-mono text-xs text-zinc-500 group-hover:text-white transition-colors">{job.job_id}</span>
              <div className={`flex items-center gap-1.5 px-2 py-0.5 rounded-full border text-[10px] uppercase font-bold tracking-wider ${getPhaseColor(job.status)}`}>
                {getPhaseIcon(job.status)}
                {job.status}
              </div>
            </div>
            
            <h3 className="text-sm font-medium text-white mb-6 line-clamp-2 leading-snug flex-1">
              Job: {job.job_id}
            </h3>

            <div className="pt-3 border-t border-white/5 mt-auto flex items-center justify-between text-xs text-zinc-400">
              <div className="flex items-center gap-4">
                <div className="flex items-center gap-1.5" title={`${job.task_count} Tasks`}>
                  <Bot className="w-3.5 h-3.5 text-zinc-500" />
                  <span className="font-mono text-zinc-300">{job.task_count}</span>
                </div>
                {job.pr_urls && job.pr_urls.length > 0 && (
                  <div className="flex items-center gap-1">
                    <GitPullRequest className="w-3 h-3 text-green-400" />
                    <span className="font-mono text-zinc-300 transition-colors">{job.pr_urls.length}</span>
                  </div>
                )}
              </div>
              <div className="flex items-center gap-1.5">
                <Clock className="w-3 h-3 text-zinc-500" />
                <span>{new Date(job.created_at).toLocaleTimeString()}</span>
              </div>
            </div>
          </button>
        ))}
      </div>

      <TaskDrawer 
        taskId={activeDrawerTaskId} 
        onClose={() => setActiveDrawerTaskId(null)} 
      />
    </div>
  );
}
