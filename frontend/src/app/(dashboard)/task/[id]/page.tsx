"use client";

import { useEffect, useState } from "react";
import { useParams, useRouter } from "next/navigation";
import { useFleetStore } from "@/store/useFleetStore";
import { ArrowLeft, Terminal, GitCommit, Square } from "lucide-react";

export default function TaskDeepDivePage() {
  const params = useParams();
  const router = useRouter();
  const { tasks } = useFleetStore();
  
  const taskId = params.id as string;
  const task = tasks.find(t => t.id === taskId);
  
  const [logs, setLogs] = useState<string[]>([
    "[SYSTEM] Connecting to daemon AWS-node-01...",
    "[SYSTEM] Secure channel established via X25519."
  ]);

  useEffect(() => {
    if (task?.phase === 'executing' || task?.phase === 'planning') {
      const interval = setInterval(() => {
        setLogs(prev => [
          ...prev, 
          `[AGENT] Executing command step ${prev.length}...`,
          `[STDOUT] success OK ${Date.now()}`
        ]);
      }, 2000);
      return () => clearInterval(interval);
    }
  }, [task]);

  if (!task) return <div className="p-8 text-white">Task not found</div>;

  const fakeDiff = `diff --git a/src/auth.go b/src/auth.go
index 832f91a..d4b9c2a 100644
--- a/src/auth.go
+++ b/src/auth.go
@@ -14,6 +14,7 @@
 func VerifyToken(token string) bool {
-    return token == "secret"
+    // TODO: implement real JWT validation
+    return len(token) > 10
 }`;

  return (
    <div className="h-full flex flex-col">
      {/* Header */}
      <header className="flex items-center justify-between p-4 border-b border-white/5 bg-black/20">
        <div className="flex items-center gap-4">
          <button onClick={() => router.back()} className="p-2 hover:bg-white/10 rounded transition-colors text-zinc-400 hover:text-white">
            <ArrowLeft className="w-5 h-5" />
          </button>
          <div>
            <h1 className="text-lg font-medium text-white flex items-center gap-3">
              {task.title}
              <span className="px-2 py-0.5 rounded text-[10px] uppercase font-bold tracking-wider bg-blue-500/10 border border-blue-500/30 text-blue-300">
                {task.phase}
              </span>
            </h1>
            <p className="text-xs text-zinc-500 font-mono mt-1">ID: {task.id} &middot; Node: {task.nodeId}</p>
          </div>
        </div>
        
        <div className="flex gap-2">
          <button className="flex items-center gap-2 px-3 py-1.5 rounded bg-red-500/20 text-red-400 hover:bg-red-500/30 transition-colors text-sm font-medium">
            <Square className="w-4 h-4" /> Stop Task
          </button>
        </div>
      </header>

      {/* Split Pane */}
      <div className="flex-1 flex overflow-hidden">
        {/* Left Pane: Logs */}
        <div className="w-1/2 flex flex-col border-r border-white/5">
          <div className="flex items-center gap-2 px-4 py-2 border-b border-white/5 bg-black/40">
            <Terminal className="w-4 h-4 text-zinc-400" />
            <span className="text-xs font-medium text-zinc-300 uppercase tracking-wider">Live Agent Logs</span>
          </div>
          <div className="flex-1 overflow-y-auto p-4 bg-[#0a0a0a] font-mono text-xs">
            {logs.map((log, i) => (
              <div key={i} className={`mb-1 ${log.startsWith('[STDOUT]') ? 'text-zinc-400' : 'text-blue-300'}`}>
                {log}
              </div>
            ))}
          </div>
        </div>

        {/* Right Pane: Diff */}
        <div className="w-1/2 flex flex-col bg-black/20">
          <div className="flex items-center gap-2 px-4 py-2 border-b border-white/5 bg-black/40">
            <GitCommit className="w-4 h-4 text-zinc-400" />
            <span className="text-xs font-medium text-zinc-300 uppercase tracking-wider">Sandbox Workspace Diff</span>
          </div>
          <div className="flex-1 p-4 overflow-y-auto">
            <pre className="text-xs font-mono leading-relaxed">
              {fakeDiff.split('\\n').map((line, i) => {
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
  );
}
