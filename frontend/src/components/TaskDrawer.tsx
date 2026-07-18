"use client";

import { useEffect } from "react";
import { useFleetStore } from "@/store/useFleetStore";
import { X, Activity, Loader2, CheckCircle2, GitPullRequest } from "lucide-react";

interface TaskDrawerProps {
  taskId: string | null;
  onClose: () => void;
}

export function TaskDrawer({ taskId, onClose }: TaskDrawerProps) {
  const { currentJob, loadJob } = useFleetStore();
  
  useEffect(() => {
    if (!taskId) return;

    let isPolling = true;

    const fetchAndCheck = async () => {
      if (!isPolling) return;
      await loadJob(taskId);
      const state = useFleetStore.getState();
      if (state.currentJob && state.currentJob.tasks && state.currentJob.tasks.length > 0) {
        const isTerminal = state.currentJob.tasks.every(
          t => t.status === 'SUCCEEDED' || t.status === 'FAILED'
        );
        if (isTerminal) {
          isPolling = false;
        }
      }
    };

    fetchAndCheck();
    
    const interval = setInterval(() => {
      if (isPolling) {
        fetchAndCheck();
      } else {
        clearInterval(interval);
      }
    }, 2500);

    return () => {
      isPolling = false;
      clearInterval(interval);
    };
  }, [taskId, loadJob]);

  if (!taskId && !currentJob) return (
    <div className={`fixed inset-y-0 right-0 w-[800px] max-w-full bg-[#050505]/95 backdrop-blur-2xl border-l border-white/10 shadow-[-20px_0_50px_rgba(0,0,0,0.8)] transition-transform duration-500 ease-[cubic-bezier(0.32,0.72,0,1)] z-50 flex flex-col translate-x-full`}></div>
  );

  const getPhaseIcon = (phase: string) => {
    switch (phase) {
      case 'RUNNING':
      case 'LEASED': return <Activity className="w-4 h-4 text-blue-400" />;
      case 'QUEUED': return <Loader2 className="w-4 h-4 text-purple-400 animate-spin" />;
      case 'SUCCEEDED': return <CheckCircle2 className="w-4 h-4 text-green-400" />;
      default: return null;
    }
  };

  return (
    <div className={`fixed inset-y-0 right-0 w-[800px] max-w-full bg-[#050505]/95 backdrop-blur-2xl border-l border-white/10 shadow-[-20px_0_50px_rgba(0,0,0,0.8)] transition-transform duration-500 ease-[cubic-bezier(0.32,0.72,0,1)] z-50 flex flex-col ${taskId ? 'translate-x-0' : 'translate-x-full'}`}>
      
      {/* Drawer Header */}
      <div className="flex items-center justify-between p-6 border-b border-white/5 bg-black/40">
        <div className="flex items-center gap-4">
          <div>
            <h2 className="text-xl font-medium text-white flex items-center gap-3">
              Job Details
              {currentJob && (
                <span className="px-2 py-0.5 rounded-full text-[10px] uppercase font-bold tracking-wider bg-white/10 text-white">
                  {currentJob.tasks.length} tasks
                </span>
              )}
            </h2>
            <p className="text-sm text-zinc-400 font-mono mt-1">Job ID: {taskId}</p>
          </div>
        </div>
        <button onClick={onClose} className="p-2 hover:bg-white/10 rounded-full transition-colors text-zinc-400 hover:text-white">
          <X className="w-6 h-6" />
        </button>
      </div>

      <div className="flex-1 flex overflow-hidden p-6 text-white overflow-y-auto">
         {currentJob ? (
           <div className="w-full flex flex-col gap-4">
             <h3 className="text-lg font-semibold">Tasks</h3>
             {currentJob.tasks.map(task => (
               <div key={task.id} className="p-4 glass-panel flex flex-col gap-2 border border-white/10 rounded-xl">
                 <div className="flex justify-between">
                   <span className="font-mono text-sm">{task.id}</span>
                   <span className="text-xs px-2 py-1 bg-white/10 rounded-md flex items-center gap-2">
                     {getPhaseIcon(task.status)} {task.status}
                   </span>
                 </div>
                 {task.result_url && (
                   <a href={task.result_url} target="_blank" rel="noreferrer" className="text-blue-400 text-sm hover:underline flex items-center gap-2 mt-2">
                     <GitPullRequest className="w-4 h-4" /> View PR
                   </a>
                 )}
                 {task.result_detail && (
                   <div className="text-red-400 text-xs mt-2">{task.result_detail}</div>
                 )}
               </div>
             ))}
           </div>
         ) : (
           <div className="text-zinc-500">Loading...</div>
         )}
      </div>
    </div>
  );
}
