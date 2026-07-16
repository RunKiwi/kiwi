"use client";

import { useFleetStore } from "@/store/useFleetStore";
import Link from "next/link";
import { Activity, Clock, Server, CheckCircle2, XCircle, Loader2 } from "lucide-react";

export default function GodView() {
  const { nodes, tasks } = useFleetStore();

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
    <div className="p-8 max-w-7xl mx-auto h-full flex flex-col">
      <div className="flex items-center justify-between mb-8">
        <div>
          <h1 className="text-3xl font-light tracking-tight text-white mb-2">The Swarm God View</h1>
          <p className="text-zinc-400">Monitoring {tasks.length} tasks executing across {nodes.length} nodes.</p>
        </div>
        
        <div className="flex gap-4">
          <div className="glass px-4 py-2 flex items-center gap-2">
            <Server className="w-4 h-4 text-zinc-400" />
            <span className="text-sm text-white font-medium">{nodes.filter(n => n.status === 'executing').length} Active Nodes</span>
          </div>
        </div>
      </div>

      {/* Grid of Tasks */}
      <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4 gap-4 pb-8">
        {tasks.map(task => (
          <Link 
            key={task.id} 
            href={`/task/${task.id}`}
            className="glass-panel p-4 hover:-translate-y-1 hover:shadow-[0_8px_30px_rgba(255,255,255,0.05)] transition-all cursor-pointer group flex flex-col h-full"
          >
            <div className="flex items-start justify-between mb-3">
              <span className="font-mono text-xs text-zinc-500 group-hover:text-white transition-colors">{task.id}</span>
              <div className={`flex items-center gap-1.5 px-2 py-0.5 rounded-full border text-[10px] uppercase font-bold tracking-wider ${getPhaseColor(task.phase)}`}>
                {getPhaseIcon(task.phase)}
                {task.phase}
              </div>
            </div>
            
            <h3 className="text-sm font-medium text-white mb-4 line-clamp-2 leading-snug flex-1">
              {task.title}
            </h3>

            <div className="pt-3 border-t border-white/5 mt-auto flex items-center justify-between text-xs text-zinc-400">
              <div className="flex items-center gap-1.5">
                <Server className="w-3 h-3" />
                <span className="font-mono">{task.nodeId.split('-')[0]}</span>
              </div>
              <div className="flex items-center gap-1.5">
                <Clock className="w-3 h-3" />
                <span>2m ago</span>
              </div>
            </div>
          </Link>
        ))}
      </div>
    </div>
  );
}
