"use client";

import { useFleetStore } from "@/store/useFleetStore";
import { Link2, CheckCircle2, XCircle } from "lucide-react";
import { SiGithub, SiJira, SiLinear, SiDiscord } from "react-icons/si";
import { TbBrandSlack } from "react-icons/tb";

export default function IntegrationsPage() {
  const { integrations } = useFleetStore();

  const getIcon = (name: string) => {
    switch(name) {
      case 'GitHub': return <SiGithub className="w-8 h-8 text-white" />;
      case 'Slack': return <TbBrandSlack className="w-8 h-8 text-pink-500" />;
      case 'Jira': return <SiJira className="w-8 h-8 text-blue-500" />;
      case 'Linear': return <SiLinear className="w-8 h-8 text-indigo-400" />;
      case 'Discord': return <SiDiscord className="w-8 h-8 text-indigo-500" />;
      default: return <Link2 className="w-8 h-8 text-zinc-300" />;
    }
  };

  return (
    <div className="p-8 max-w-7xl mx-auto h-full flex flex-col">
      <div className="mb-8">
        <h1 className="text-3xl font-light tracking-tight text-white mb-2">Integrations</h1>
        <p className="text-zinc-400">Connect external tools to reference context and output results across channels.</p>
      </div>

      <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-6">
        {integrations.map(integration => (
          <div key={integration.id} className="glass-panel p-6 flex flex-col">
            <div className="flex items-start justify-between mb-6">
              <div className="w-16 h-16 rounded-2xl bg-white/5 flex items-center justify-center border border-white/10">
                {getIcon(integration.name)}
              </div>
              <div className="flex items-center gap-2">
                {integration.status === 'connected' ? (
                  <span className="flex items-center gap-1.5 text-xs font-bold uppercase tracking-wider text-green-400 bg-green-400/10 px-2.5 py-1 rounded border border-green-400/20">
                    <CheckCircle2 className="w-3 h-3" /> Connected
                  </span>
                ) : (
                  <span className="flex items-center gap-1.5 text-xs font-bold uppercase tracking-wider text-zinc-500 bg-white/5 px-2.5 py-1 rounded border border-white/10">
                    <XCircle className="w-3 h-3" /> Disconnected
                  </span>
                )}
              </div>
            </div>
            
            <div className="mb-6">
              <h3 className="text-xl font-medium text-white mb-1">{integration.name}</h3>
              {integration.workspace && (
                <p className="text-sm text-zinc-400">Workspace: <span className="text-zinc-300 font-medium">{integration.workspace}</span></p>
              )}
            </div>

            <div className="mt-auto">
              <button 
                className={`w-full py-2.5 rounded-lg text-sm font-medium transition-colors border ${
                  integration.status === 'connected' 
                    ? 'bg-white/5 border-white/10 text-white hover:bg-white/10' 
                    : 'bg-white text-black hover:bg-zinc-200 border-transparent shadow-[0_0_15px_rgba(255,255,255,0.1)]'
                }`}
              >
                {integration.status === 'connected' ? 'Configure' : 'Connect'}
              </button>
            </div>
          </div>
        ))}
      </div>
    </div>
  );
}
