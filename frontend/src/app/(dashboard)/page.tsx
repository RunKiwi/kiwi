"use client";

import { useState } from "react";
import { useFleetStore } from "@/store/useFleetStore";
import { Activity, Clock, Users, CheckCircle2, XCircle, Loader2 } from "lucide-react";
import { TaskDrawer } from "@/components/TaskDrawer";

export default function GodView() {
  const { nodes, tasks } = useFleetStore();
  const [activeDrawerTaskId, setActiveDrawerTaskId] = useState<string | null>(null);

  const getPhaseIcon = (phase: string) => {
    switch (phase) {
      case 'executing': return <Activity className="w-4 h-4 text-blue-400" />;
      case 'planning': return <Loader2 className="w-4 h-4 text-purple-400 animate-spin" />;
      case 'completed': return <CheckCircle2 className="w-4 h-4 text-green-400" />;
      case 'failed': return <XCircle className="w-4 h-4 text-red-400" />;
      default: return null;
    }
  };

  const getPhaseColor = (phase: string) => {
    switch (phase) {
      case 'executing': return 'bg-blue-500/10 border-blue-500/30 text-blue-300';
      case 'planning': return 'bg-purple-500/10 border-purple-500/30 text-purple-300';
      case 'completed': return 'bg-green-500/10 border-green-500/30 text-green-300';
      case 'failed': return 'bg-red-500/10 border-red-500/30 text-red-300';
      default: return 'bg-white/5 border-white/10 text-white';
    }
  };

  return (
    <div className="p-8 max-w-7xl mx-auto h-full flex flex-col relative">
      <div className="flex items-center justify-between mb-8">
        <div>
          <h1 className="text-3xl font-light tracking-tight text-white mb-2">The Swarm God View</h1>
          <p className="text-zinc-400">Monitoring {tasks.length} high-level goals across {nodes.length} nodes.</p>
        </div>
      </div>

      {/* Grid of Tasks */}
      <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4 gap-4 pb-32">
        {tasks.map(task => (
          <button 
            key={task.id} 
            onClick={() => setActiveDrawerTaskId(task.id)}
            className="text-left glass-panel p-4 hover:-translate-y-1 hover:shadow-[0_8px_30px_rgba(255,255,255,0.05)] transition-all cursor-pointer group flex flex-col h-full"
          >
            <div className="flex items-start justify-between mb-3">
              <span className="font-mono text-xs text-zinc-500 group-hover:text-white transition-colors">{task.id}</span>
              <div className={`flex items-center gap-1.5 px-2 py-0.5 rounded-full border text-[10px] uppercase font-bold tracking-wider ${getPhaseColor(task.phase)}`}>
                {getPhaseIcon(task.phase)}
                {task.phase}
              </div>
            </div>
            
            <h3 className="text-sm font-medium text-white mb-6 line-clamp-2 leading-snug flex-1">
              {task.title}
            </h3>

            <div className="pt-3 border-t border-white/5 mt-auto flex items-center justify-between text-xs text-zinc-400">
              <div className="flex items-center gap-1.5">
                <Users className="w-3 h-3 text-zinc-500" />
                <span className="font-mono text-zinc-300">{task.subAgents.length} Agents</span>
              </div>
              <div className="flex items-center gap-1.5">
                <Clock className="w-3 h-3 text-zinc-500" />
                <span>2m ago</span>
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
