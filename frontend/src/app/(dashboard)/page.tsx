"use client";

import { useState } from "react";
import { useFleetStore } from "@/store/useFleetStore";
import { Activity, Clock, CheckCircle2, XCircle, Loader2, GitPullRequest, GitMerge, GitPullRequestClosed, Bot, Sparkles, GitBranch, ChevronDown, Send } from "lucide-react";
import { TaskDrawer } from "@/components/TaskDrawer";

export default function GodView() {
  const { tasks, providers, repositories } = useFleetStore();
  const [activeDrawerTaskId, setActiveDrawerTaskId] = useState<string | null>(null);
  const [activeDropdownTaskId, setActiveDropdownTaskId] = useState<string | null>(null);
  const [isRepoDropdownOpen, setIsRepoDropdownOpen] = useState(false);
  const [isModelDropdownOpen, setIsModelDropdownOpen] = useState(false);
  
  // Task Creation State
  const [prompt, setPrompt] = useState("");
  const [selectedOrchestrator, setSelectedOrchestrator] = useState<string>("claude-3-5-sonnet");
  const [selectedWorker, setSelectedWorker] = useState<string>("gpt-4o-mini");
  const [selectedRepos, setSelectedRepos] = useState<string[]>(["r-1"]); // kiwi

  // Flatten all available models from configured providers
  const availableModels = providers
    .filter(p => p.isConfigured)
    .flatMap(p => p.availableModels.map(m => ({ ...m, providerName: p.name })));

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
      case 'completed': return 'bg-green-500/10 border-green-500/20 text-green-300';
      case 'failed': return 'bg-red-500/10 border-red-500/20 text-red-300';
      default: return 'bg-white/5 border-white/10 text-white';
    }
  };

  const getCardStyle = (phase: string) => {
    const base = "text-left glass-panel p-4 hover:-translate-y-1 hover:shadow-[0_8px_30px_rgba(255,255,255,0.05)] transition-all cursor-pointer group flex flex-col h-full relative ";
    switch (phase) {
      case 'executing': 
        return base + "border-blue-500/30 shadow-[0_0_15px_rgba(59,130,246,0.1)] before:absolute before:inset-0 before:-z-10 before:rounded-2xl before:bg-[linear-gradient(90deg,transparent,rgba(59,130,246,0.05),transparent)] before:bg-[length:200%_100%] before:animate-shimmer";
      case 'planning': 
        return base + "animate-breathing";
      case 'completed': 
        return base + "bg-green-950/20 border-green-500/20 shadow-[0_0_15px_rgba(34,197,94,0.05)]";
      case 'failed': 
        return base + "bg-red-950/20 border-red-500/20 shadow-[0_0_15px_rgba(239,68,68,0.05)]";
      default: 
        return base;
    }
  };

  return (
    <div className="p-8 max-w-7xl mx-auto h-full flex flex-col" onClick={() => setActiveDropdownTaskId(null)}>
      <div className="mb-8">
        <h1 className="text-3xl font-light tracking-tight text-white mb-2">Command Center</h1>
        <p className="text-zinc-400">Command your agents and monitor high-level goals across the Swarm.</p>
      </div>

      {/* Command Bar */}
      <div className="bg-black/60 backdrop-blur-xl rounded-2xl mb-8 flex flex-col relative z-20 shadow-[0_0_40px_rgba(0,0,0,0.5)] border border-white/10 overflow-hidden group/cmd">
        <div className="absolute inset-0 bg-gradient-to-b from-white/[0.02] to-transparent pointer-events-none" />
        <textarea 
          value={prompt}
          onChange={(e) => setPrompt(e.target.value)}
          placeholder="What would you like the Swarm to build?"
          className="w-full bg-transparent p-6 pb-4 text-white placeholder-zinc-500 focus:outline-none transition-all resize-none min-h-[120px] text-lg font-light relative z-10"
        />
        
        <div className="flex items-center justify-between px-6 pb-5 pt-0 relative z-10">
          <div className="flex items-center gap-3 flex-wrap">
            {/* Repos Select Custom Dropdown */}
            <div className="relative">
              <button 
                onClick={() => {
                  setIsRepoDropdownOpen(!isRepoDropdownOpen);
                  setIsModelDropdownOpen(false);
                }}
                className="flex items-center gap-2 px-3 py-1.5 rounded-lg bg-white/[0.03] hover:bg-white/[0.08] border border-white/10 hover:border-white/20 transition-all text-sm text-zinc-300 group-focus-within/cmd:border-purple-500/30"
              >
                <GitBranch className="w-4 h-4 text-purple-400" />
                <span className="font-medium">{repositories.find(r => r.id === selectedRepos[0])?.name || 'Repository'}</span>
                <ChevronDown className="w-3.5 h-3.5 text-zinc-500 ml-1" />
              </button>
              
              {isRepoDropdownOpen && (
                <div className="absolute top-full mt-2 left-0 w-64 bg-zinc-950 border border-white/10 rounded-xl shadow-2xl z-50 overflow-hidden py-1 animate-in fade-in slide-in-from-top-2">
                  <div className="px-3 py-2 text-[10px] font-bold text-zinc-500 uppercase tracking-widest">Target Repository</div>
                  {repositories.map(r => (
                    <button
                      key={r.id}
                      onClick={() => {
                        setSelectedRepos([r.id]);
                        setIsRepoDropdownOpen(false);
                      }}
                      className="w-full text-left px-4 py-2.5 text-sm text-zinc-300 hover:bg-white/5 hover:text-white transition-colors flex items-center gap-2"
                    >
                      <GitBranch className="w-4 h-4 text-zinc-500" />
                      {r.name}
                    </button>
                  ))}
                </div>
              )}
            </div>

            {/* Model Select Custom Dropdown */}
            <div className="relative">
              <button 
                onClick={() => {
                  setIsModelDropdownOpen(!isModelDropdownOpen);
                  setIsRepoDropdownOpen(false);
                }}
                className="flex items-center gap-2 px-3 py-1.5 rounded-lg bg-white/[0.03] hover:bg-white/[0.08] border border-white/10 hover:border-white/20 transition-all text-sm text-zinc-300 group-focus-within/cmd:border-blue-500/30"
              >
                <Bot className="w-4 h-4 text-blue-400" />
                <span className="font-medium">
                  {availableModels.find(m => m.id === selectedOrchestrator)?.name.split(' ')[1] || 'Orchestrator'} + {availableModels.find(m => m.id === selectedWorker)?.name.split(' ')[0] || 'Worker'}
                </span>
                <ChevronDown className="w-3.5 h-3.5 text-zinc-500 ml-1" />
              </button>

              {isModelDropdownOpen && (
                <div className="absolute top-full mt-2 left-0 w-80 bg-zinc-950 border border-white/10 rounded-xl shadow-2xl z-50 overflow-hidden py-1 animate-in fade-in slide-in-from-top-2 flex flex-col">
                  
                  <div className="px-4 py-3 border-b border-white/5">
                    <div className="text-[10px] font-bold text-zinc-500 uppercase tracking-widest mb-2">Orchestrator Intelligence</div>
                    <div className="space-y-1">
                      {availableModels.map(m => (
                        <button
                          key={`orch-${m.id}`}
                          onClick={() => setSelectedOrchestrator(m.id)}
                          className={`w-full text-left px-3 py-2 text-sm rounded-lg transition-colors flex items-center justify-between ${selectedOrchestrator === m.id ? 'bg-blue-500/10 text-blue-400' : 'text-zinc-300 hover:bg-white/5 hover:text-white'}`}
                        >
                          <span>{m.name}</span>
                          <span className="text-[10px] opacity-50 uppercase tracking-wider">{m.providerName}</span>
                        </button>
                      ))}
                    </div>
                  </div>

                  <div className="px-4 py-3">
                    <div className="text-[10px] font-bold text-zinc-500 uppercase tracking-widest mb-2">Worker Fleet</div>
                    <div className="space-y-1">
                      {availableModels.map(m => (
                        <button
                          key={`work-${m.id}`}
                          onClick={() => setSelectedWorker(m.id)}
                          className={`w-full text-left px-3 py-2 text-sm rounded-lg transition-colors flex items-center justify-between ${selectedWorker === m.id ? 'bg-purple-500/10 text-purple-400' : 'text-zinc-300 hover:bg-white/5 hover:text-white'}`}
                        >
                          <span>{m.name}</span>
                          <span className="text-[10px] opacity-50 uppercase tracking-wider">{m.providerName}</span>
                        </button>
                      ))}
                    </div>
                  </div>

                </div>
              )}
            </div>
          </div>

          <button className="flex items-center gap-2 bg-white hover:bg-zinc-200 text-black px-5 py-2 rounded-xl font-semibold transition-all shadow-[0_0_20px_rgba(255,255,255,0.15)] shrink-0">
            Dispatch
            <Send className="w-4 h-4" />
          </button>
        </div>
      </div>

      {/* Grid of Tasks */}
      <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4 gap-4 pb-32 relative z-10">
        {tasks.map(task => (
          <button 
            key={task.id} 
            onClick={() => setActiveDrawerTaskId(task.id)}
            className={getCardStyle(task.phase)}
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
              <div className="flex items-center gap-4">
                <div className="flex items-center gap-1.5" title={`${task.subAgents.length} Agents`}>
                  <Bot className="w-3.5 h-3.5 text-zinc-500" />
                  <span className="font-mono text-zinc-300">{task.subAgents.length}</span>
                </div>
                {(() => {
                  if (!task.pullRequests || task.pullRequests.length === 0) return null;
                  const openPRs = task.pullRequests.filter(pr => pr.status === 'open');
                  const mergedPRs = task.pullRequests.filter(pr => pr.status === 'merged');
                  const closedPRs = task.pullRequests.filter(pr => pr.status === 'closed');
                  
                  return (
                    <div className="relative">
                      <div 
                        onClick={(e) => {
                          e.stopPropagation();
                          setActiveDropdownTaskId(activeDropdownTaskId === task.id ? null : task.id);
                        }}
                        className="flex items-center gap-2 hover:bg-white/10 p-1 -m-1 rounded transition-colors cursor-pointer group/pr"
                        title="View Pull Requests"
                      >
                        {openPRs.length > 0 && (
                          <div className="flex items-center gap-1">
                            <GitPullRequest className="w-3 h-3 text-green-400" />
                            <span className="font-mono text-zinc-300 group-hover/pr:text-white transition-colors">{openPRs.length}</span>
                          </div>
                        )}
                        {mergedPRs.length > 0 && (
                          <div className="flex items-center gap-1">
                            <GitMerge className="w-3 h-3 text-purple-400" />
                            <span className="font-mono text-zinc-300 group-hover/pr:text-white transition-colors">{mergedPRs.length}</span>
                          </div>
                        )}
                        {closedPRs.length > 0 && (
                          <div className="flex items-center gap-1">
                            <GitPullRequestClosed className="w-3 h-3 text-red-400" />
                            <span className="font-mono text-zinc-300 group-hover/pr:text-white transition-colors">{closedPRs.length}</span>
                          </div>
                        )}
                      </div>

                      {/* PR Dropdown */}
                      {activeDropdownTaskId === task.id && (
                        <div 
                          className="absolute bottom-full mb-2 left-1/2 -translate-x-1/2 w-48 glass-panel bg-zinc-950/95 border border-white/10 rounded-lg shadow-[0_8px_30px_rgba(0,0,0,0.5)] z-50 overflow-hidden"
                          onClick={(e) => e.stopPropagation()}
                        >
                          {task.pullRequests.map(pr => (
                            <div 
                              key={pr.id}
                              onClick={(e) => {
                                e.stopPropagation();
                                window.open(`https://github.com/RunKiwi/kiwi/pull/${pr.id.split('-')[1]}`, "_blank");
                                setActiveDropdownTaskId(null);
                              }}
                              className="flex items-center gap-2 px-3 py-2 hover:bg-white/10 cursor-pointer transition-colors border-b border-white/5 last:border-0"
                            >
                              {pr.status === 'open' ? (
                                <GitPullRequest className="w-3.5 h-3.5 text-green-400 shrink-0" />
                              ) : pr.status === 'merged' ? (
                                <GitMerge className="w-3.5 h-3.5 text-purple-400 shrink-0" />
                              ) : (
                                <GitPullRequestClosed className="w-3.5 h-3.5 text-red-400 shrink-0" />
                              )}
                              <span className="font-mono text-xs text-zinc-300 truncate">#{pr.id.split('-')[1]}</span>
                            </div>
                          ))}
                        </div>
                      )}
                    </div>
                  );
                })()}
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
