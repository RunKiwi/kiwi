"use client";

import { useFleetStore } from "@/store/useFleetStore";
import { Cpu, Key, CheckCircle2, XCircle } from "lucide-react";

export default function ModelsPage() {
  const { models } = useFleetStore();

  return (
    <div className="p-8 max-w-5xl mx-auto h-full flex flex-col">
      <div className="mb-8">
        <h1 className="text-3xl font-light tracking-tight text-white mb-2">Language Models</h1>
        <p className="text-zinc-400">Configure your Frontier and Worker models for the Swarm to utilize.</p>
      </div>

      <div className="space-y-4">
        {models.map(model => (
          <div key={model.id} className="glass-panel p-6 flex items-center justify-between">
            <div className="flex items-center gap-4">
              <div className="w-12 h-12 rounded-xl bg-white/5 flex items-center justify-center border border-white/10">
                <Cpu className="w-6 h-6 text-zinc-300" />
              </div>
              <div>
                <h3 className="text-lg font-medium text-white">{model.name}</h3>
                <p className="text-sm text-zinc-400">{model.provider}</p>
              </div>
            </div>
            
            <div className="flex items-center gap-6">
              <div className="flex items-center gap-2">
                {model.isConfigured ? (
                  <span className="flex items-center gap-1.5 text-sm text-green-400 bg-green-400/10 px-3 py-1.5 rounded-full border border-green-400/20">
                    <CheckCircle2 className="w-4 h-4" /> Connected
                  </span>
                ) : (
                  <span className="flex items-center gap-1.5 text-sm text-zinc-500 bg-white/5 px-3 py-1.5 rounded-full border border-white/10">
                    <XCircle className="w-4 h-4" /> Not Configured
                  </span>
                )}
              </div>
              <div className="flex items-center gap-2 relative w-64">
                <Key className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-zinc-500" />
                <input 
                  type="password"
                  placeholder={model.isConfigured ? "sk-..." : "Enter API Key"}
                  className="w-full bg-black/40 border border-white/10 rounded-lg pl-10 pr-4 py-2.5 text-sm text-white focus:outline-none focus:border-white/30 transition-colors"
                />
              </div>
            </div>
          </div>
        ))}
      </div>
    </div>
  );
}
