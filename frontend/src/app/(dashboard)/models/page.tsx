"use client";

import { useState } from "react";
import { useFleetStore } from "@/store/useFleetStore";
import { Cpu, Key, CheckCircle2, XCircle, Plus, X } from "lucide-react";
import { SiAnthropic, SiGoogle, SiMeta } from "react-icons/si";
import { TbBrandOpenai } from "react-icons/tb";

export default function ModelsPage() {
  const { providers } = useFleetStore();
  const [isConnectModalOpen, setIsConnectModalOpen] = useState(false);
  const [newProvider, setNewProvider] = useState('OpenAI');
  const [newKey, setNewKey] = useState('');

  const handleConnect = () => {
    // In a real app, this would validate the key and save it to the backend/store
    // For now, just close the modal
    setIsConnectModalOpen(false);
    setNewKey('');
  };

  const getProviderIcon = (name: string) => {
    switch(name) {
      case 'OpenAI': return <TbBrandOpenai className="w-6 h-6 text-white" />;
      case 'Anthropic': return <SiAnthropic className="w-6 h-6 text-[#d97757]" />;
      case 'Google': return <SiGoogle className="w-6 h-6 text-blue-400" />;
      case 'Meta': return <SiMeta className="w-6 h-6 text-blue-600" />;
      default: return <Cpu className="w-6 h-6 text-zinc-300" />; // Fallback for Cohere and others
    }
  };

  return (
    <div className="p-8 max-w-5xl mx-auto h-full flex flex-col relative">
      <div className="flex items-center justify-between mb-8">
        <div>
          <h1 className="text-3xl font-light tracking-tight text-white mb-2">LLM Providers</h1>
          <p className="text-zinc-400">Configure your LLM providers to give your Swarm access to Frontier and Worker models.</p>
        </div>
        <button 
          onClick={() => setIsConnectModalOpen(true)}
          className="flex items-center gap-2 px-6 py-3 bg-white text-black font-medium rounded-lg hover:bg-zinc-200 transition-colors shadow-[0_0_20px_rgba(255,255,255,0.2)]"
        >
          <Plus className="w-4 h-4" />
          Add Provider
        </button>
      </div>

      <div className="space-y-4 pb-32">
        {providers.map(provider => (
          <div key={provider.id} className="glass-panel p-6 flex flex-col gap-4">
            <div className="flex items-center justify-between">
              <div className="flex items-center gap-4">
                <div className="w-12 h-12 rounded-xl bg-white/5 flex items-center justify-center border border-white/10">
                  {getProviderIcon(provider.name)}
                </div>
                <div>
                  <h3 className="text-lg font-medium text-white">{provider.name}</h3>
                  <div className="flex items-center gap-2 mt-1">
                    {provider.isConfigured ? (
                      <span className="flex items-center gap-1.5 text-xs text-green-400 font-medium">
                        <CheckCircle2 className="w-3.5 h-3.5" /> Connected
                      </span>
                    ) : (
                      <span className="flex items-center gap-1.5 text-xs text-zinc-500 font-medium">
                        <XCircle className="w-3.5 h-3.5" /> Not Configured
                      </span>
                    )}
                  </div>
                </div>
              </div>
              
              <div className="flex items-center gap-6">
                <div className="flex items-center gap-2 relative w-64">
                  <Key className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-zinc-500" />
                  <input 
                    type="password"
                    placeholder={provider.isConfigured ? "sk-..." : "Enter API Key"}
                    className="w-full bg-black/40 border border-white/10 rounded-lg pl-10 pr-4 py-2.5 text-sm text-white focus:outline-none focus:border-white/30 transition-colors"
                  />
                </div>
              </div>
            </div>

            {provider.isConfigured && provider.availableModels.length > 0 && (
              <div className="pt-4 border-t border-white/5">
                <p className="text-xs text-zinc-500 uppercase tracking-wider font-bold mb-3">Available Models</p>
                <div className="flex flex-wrap gap-2">
                  {provider.availableModels.map(m => (
                    <span key={m.id} className="text-xs bg-white/5 border border-white/10 px-2.5 py-1 rounded text-zinc-300">
                      {m.name}
                    </span>
                  ))}
                </div>
              </div>
            )}
          </div>
        ))}
      </div>

      {/* Connect Modal */}
      {isConnectModalOpen && (
        <div className="fixed inset-0 z-50 flex items-center justify-center p-4 bg-black/60 backdrop-blur-sm">
          <div className="glass-panel w-full max-w-lg relative animate-in fade-in zoom-in-95 duration-200">
            <button 
              onClick={() => setIsConnectModalOpen(false)}
              className="absolute top-6 right-6 p-2 rounded-lg bg-white/5 hover:bg-white/10 transition-colors text-zinc-400 hover:text-white"
            >
              <X className="w-5 h-5" />
            </button>
            
            <div className="p-8">
              <div className="mb-8">
                <h2 className="text-2xl font-light text-white mb-2">Connect Provider</h2>
                <p className="text-zinc-400">Add an API key to grant your Swarm access to new models.</p>
              </div>

              <div className="space-y-6">
                <div>
                  <label className="block text-sm font-medium text-zinc-300 mb-2">Provider</label>
                  <select 
                    value={newProvider}
                    onChange={(e) => setNewProvider(e.target.value)}
                    className="w-full bg-black/40 border border-white/10 rounded-lg px-4 py-3 text-white focus:outline-none focus:border-white/30 transition-colors cursor-pointer appearance-none"
                  >
                    <option value="OpenAI" className="bg-zinc-900">OpenAI</option>
                    <option value="Anthropic" className="bg-zinc-900">Anthropic</option>
                    <option value="Google" className="bg-zinc-900">Google (Gemini)</option>
                    <option value="Cohere" className="bg-zinc-900">Cohere</option>
                    <option value="Meta" className="bg-zinc-900">Meta (Llama)</option>
                  </select>
                </div>

                <div>
                  <label className="block text-sm font-medium text-zinc-300 mb-2">API Key</label>
                  <div className="relative">
                    <Key className="absolute left-4 top-1/2 -translate-y-1/2 w-4 h-4 text-zinc-500" />
                    <input 
                      type="password"
                      value={newKey}
                      onChange={(e) => setNewKey(e.target.value)}
                      placeholder="sk-..."
                      className="w-full bg-black/40 border border-white/10 rounded-lg pl-12 pr-4 py-3 text-white focus:outline-none focus:border-white/30 transition-colors font-mono"
                    />
                  </div>
                </div>

                <button 
                  onClick={handleConnect}
                  className="w-full py-3 bg-white text-black font-medium rounded-lg hover:bg-zinc-200 transition-colors mt-2"
                >
                  Connect
                </button>
              </div>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
