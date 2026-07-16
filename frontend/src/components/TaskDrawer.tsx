/* eslint-disable react-hooks/set-state-in-effect */
"use client";

import { useEffect, useState } from "react";
import { useFleetStore } from "@/store/useFleetStore";
import { X, Terminal, GitCommit, Users, Server, CheckCircle2, Activity, Loader2 } from "lucide-react";

interface TaskDrawerProps {
  taskId: string | null;
  onClose: () => void;
}

export function TaskDrawer({ taskId, onClose }: TaskDrawerProps) {
  const { tasks } = useFleetStore();
  const currentTask = tasks.find(t => t.id === taskId);
  
  // Keep a ref/state of the last known task so the drawer doesn't go blank while animating out
  const [task, setTask] = useState(currentTask);
  
  useEffect(() => {
    if (currentTask) setTask(currentTask);
  }, [currentTask]);
  
  const [activeSubAgentId, setActiveSubAgentId] = useState<string | null>(null);
  const [logs, setLogs] = useState<string[]>([]);

  useEffect(() => {
    if (task && task.subAgents.length > 0 && !activeSubAgentId) {
      setActiveSubAgentId(task.subAgents[0].id);
    }
  }, [task, activeSubAgentId]);

  useEffect(() => {
    setLogs(["[SYSTEM] Connecting to sub-agent sandbox..."]);
    const interval = setInterval(() => {
      setLogs(prev => [
        ...prev, 
        `[AGENT-${activeSubAgentId?.slice(-2)}] Executing step ${prev.length}...`,
        `[STDOUT] success OK ${Date.now()}`
      ]);
    }, 1500);
    return () => clearInterval(interval);
  }, [activeSubAgentId]);

  if (!task) return (
    <div className={`fixed inset-y-0 right-0 w-[800px] max-w-full bg-[#050505]/95 backdrop-blur-2xl border-l border-white/10 shadow-[-20px_0_50px_rgba(0,0,0,0.8)] transition-transform duration-500 ease-[cubic-bezier(0.32,0.72,0,1)] z-50 flex flex-col translate-x-full`}></div>
  );

  const fakeDiff = `diff --git a/src/main.go b/src/main.go
index 832f91a..d4b9c2a 100644
--- a/src/main.go
+++ b/src/main.go
@@ -14,6 +14,7 @@
 func init() {
-    log.Println("Starting...")
+    log.Println("Initialized via Swarm")
+    runtime.Scale(4)
 }`;

  const getPhaseIcon = (phase: string) => {
    switch (phase) {
      case 'executing': return <Activity className="w-4 h-4 text-blue-400" />;
      case 'planning': return <Loader2 className="w-4 h-4 text-purple-400 animate-spin" />;
      case 'completed': return <CheckCircle2 className="w-4 h-4 text-green-400" />;
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
              {task.title}
              <span className="px-2 py-0.5 rounded-full text-[10px] uppercase font-bold tracking-wider bg-white/10 text-white">
                {task.phase}
              </span>
            </h2>
            <p className="text-sm text-zinc-400 font-mono mt-1">Goal ID: {task.id}</p>
          </div>
        </div>
        <button onClick={onClose} className="p-2 hover:bg-white/10 rounded-full transition-colors text-zinc-400 hover:text-white">
          <X className="w-6 h-6" />
        </button>
      </div>

      {/* Drawer Content - 2 Column Split (Left: Agents, Right: Logs & Diff stacked) */}
      <div className="flex-1 flex overflow-hidden">
        
        {/* Column 1: Sub-Agents List */}
        <div className="w-1/3 flex flex-col border-r border-white/5 bg-black/20">
          <div className="flex items-center gap-2 px-4 py-3 border-b border-white/5 text-xs font-medium text-zinc-300 uppercase tracking-wider">
            <Users className="w-4 h-4 text-zinc-400" />
            Active Swarm
          </div>
          <div className="flex-1 overflow-y-auto p-3 space-y-3">
            {task.subAgents.map(agent => (
              <button 
                key={agent.id}
                onClick={() => setActiveSubAgentId(agent.id)}
                className={`w-full text-left p-4 rounded-xl border transition-all ${
                  activeSubAgentId === agent.id 
                    ? 'bg-white/10 border-white/20 shadow-sm' 
                    : 'bg-black/40 border-transparent hover:bg-white/5'
                }`}
              >
                <div className="flex items-center justify-between mb-3">
                  <span className={`text-[10px] uppercase font-bold px-2 py-1 rounded-md ${agent.role === 'master' ? 'bg-purple-500/20 text-purple-300' : 'bg-blue-500/20 text-blue-300'}`}>
                    {agent.role}
                  </span>
                  {getPhaseIcon(agent.phase)}
                </div>
                <div className="text-sm font-medium text-white mb-2">{agent.title}</div>
                <div className="text-xs text-zinc-500 font-mono flex items-center gap-1.5">
                  <Server className="w-3 h-3" /> {agent.nodeId}
                </div>
              </button>
            ))}
          </div>
        </div>

        {/* Column 2: Logs & Diff (Stacked vertically) */}
        <div className="w-2/3 flex flex-col">
          
          {/* Top Half: Logs */}
          <div className="flex-1 flex flex-col border-b border-white/5 min-h-0">
            <div className="flex items-center gap-2 px-4 py-3 border-b border-white/5 bg-black/40 text-xs font-medium text-zinc-300 uppercase tracking-wider">
              <Terminal className="w-4 h-4 text-zinc-400" />
              Live Logs
            </div>
            <div className="flex-1 overflow-y-auto p-4 bg-[#0a0a0a] font-mono text-xs leading-relaxed">
              {logs.map((log, i) => (
                <div key={i} className={`mb-1.5 ${log.startsWith('[STDOUT]') ? 'text-zinc-400' : 'text-blue-300'}`}>
                  {log}
                </div>
              ))}
            </div>
          </div>

          {/* Bottom Half: Diff */}
          <div className="flex-1 flex flex-col bg-black/20 min-h-0">
            <div className="flex items-center gap-2 px-4 py-3 border-b border-white/5 bg-black/40 text-xs font-medium text-zinc-300 uppercase tracking-wider">
              <GitCommit className="w-4 h-4 text-zinc-400" />
              Workspace Diff
            </div>
            <div className="flex-1 p-4 overflow-y-auto">
              <pre className="text-xs font-mono leading-relaxed">
                {fakeDiff.split('\n').map((line, i) => {
                  let color = 'text-zinc-300';
                  let bg = 'bg-transparent';
                  if (line.startsWith('+')) {
                    color = 'text-green-400';
                    bg = 'bg-green-500/10';
                  } else if (line.startsWith('-')) {
                    color = 'text-red-400';
                    bg = 'bg-red-500/10';
                  } else if (line.startsWith('@@')) {
                    color = 'text-blue-400';
                  }
                  return (
                    <div key={i} className={`px-2 py-0.5 ${bg} ${color}`}>
                      {line}
                    </div>
                  );
                })}
              </pre>
            </div>
          </div>

        </div>

      </div>
    </div>
  );
}
