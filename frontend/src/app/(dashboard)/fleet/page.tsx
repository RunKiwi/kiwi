"use client";

import { useState, useEffect } from "react";
import { Copy, Check, RefreshCw, Server, X, Activity, AlertCircle, Cpu, MemoryStick } from "lucide-react";
import { useFleetStore, Node } from "@/store/useFleetStore";

export default function FleetPage() {
  const { nodes } = useFleetStore();
  const [isDeployModalOpen, setIsDeployModalOpen] = useState(false);

  // Modal State
  const [provider, setProvider] = useState<'AWS' | 'GCP'>('AWS');
  const [apiKey, setApiKey] = useState('');
  const [copied, setCopied] = useState(false);
  const [isRegenerating, setIsRegenerating] = useState(false);

  const generateKey = () => {
    setIsRegenerating(true);
    setTimeout(() => {
      const newKey = 'kw_live_' + Array.from(crypto.getRandomValues(new Uint8Array(16)))
        .map(b => b.toString(16).padStart(2, '0'))
        .join('');
      setApiKey(newKey);
      setIsRegenerating(false);
    }, 400);
  };

  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect
    if (isDeployModalOpen && !apiKey) generateKey();
  }, [isDeployModalOpen]); // eslint-disable-line react-hooks/exhaustive-deps

  const tfSnippet = provider === 'AWS' ? `module "kiwi_swarm" {
  source = "runkiwi/swarm/aws"
  version = "1.0.0"

  api_key = "${apiKey || '<YOUR_API_KEY>'}"
  vpc_id = var.vpc_id
  subnet_ids = var.subnet_ids
}` : `module "kiwi_swarm" {
  source = "runkiwi/swarm/gcp"
  version = "1.0.0"

  api_key = "${apiKey || '<YOUR_API_KEY>'}"
  project_id = var.project_id
  network = var.network
}`;

  const handleCopy = () => {
    navigator.clipboard.writeText(tfSnippet);
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  };

  const getStatusColor = (status: Node['status']) => {
    switch(status) {
      case 'executing': return 'text-blue-400 bg-blue-400/10 border-blue-400/30';
      case 'polling': return 'text-green-400 bg-green-400/10 border-green-400/30';
      case 'disconnected': return 'text-red-400 bg-red-400/10 border-red-400/30';
      default: return 'text-zinc-400 bg-white/5 border-white/10';
    }
  };

  const totalVCPUs = nodes.length * 8;
  const totalRAM = nodes.length * 32;
  // eslint-disable-next-line react-hooks/purity
  const now = Date.now();

  return (
    <div className="p-8 max-w-7xl mx-auto h-full flex flex-col relative">
      <div className="flex items-center justify-between mb-8">
        <div>
          <h1 className="text-3xl font-light tracking-tight text-white mb-2">Fleet Management</h1>
          <p className="text-zinc-400">
            Monitoring {nodes.length} connected nodes ({totalVCPUs} vCPUs, {totalRAM}GB RAM available).
          </p>
        </div>
        <button 
          onClick={() => setIsDeployModalOpen(true)}
          className="flex items-center gap-2 px-6 py-3 bg-white text-black font-medium rounded-lg hover:bg-zinc-200 transition-colors shadow-[0_0_20px_rgba(255,255,255,0.2)]"
        >
          <Server className="w-4 h-4" />
          Deploy Capacity
        </button>
      </div>

      {/* Node Grid */}
      <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4 pb-32">
        {nodes.map(node => (
          <div key={node.id} className="glass-panel p-5 flex flex-col hover:-translate-y-1 transition-transform">
            <div className="flex items-start justify-between mb-4">
              <div className="flex items-center gap-3">
                <div className="w-10 h-10 rounded-lg bg-white/5 flex items-center justify-center border border-white/10">
                  <Server className="w-5 h-5 text-zinc-300" />
                </div>
                <div>
                  <div className="text-sm font-medium text-white">{node.id}</div>
                  <div className="text-xs text-zinc-500 font-mono">{node.provider}</div>
                </div>
              </div>
              <div className={`text-[10px] uppercase font-bold tracking-wider px-2 py-1 rounded border flex items-center gap-1.5 ${getStatusColor(node.status)}`}>
                {node.status === 'executing' ? <Activity className="w-3 h-3" /> : 
                 node.status === 'polling' ? <div className="w-2 h-2 rounded-full bg-green-400 animate-pulse" /> : 
                 <AlertCircle className="w-3 h-3" />}
                {node.status}
              </div>
            </div>

            <div className="space-y-4 mb-4">
              <div>
                <div className="flex items-center justify-between text-xs mb-1.5">
                  <span className="text-zinc-400 flex items-center gap-1"><Cpu className="w-3 h-3" /> CPU</span>
                  <span className="text-zinc-300 font-mono">{node.cpuUsage}%</span>
                </div>
                <div className="h-1.5 w-full bg-black/40 rounded-full overflow-hidden">
                  <div className={`h-full rounded-full transition-all duration-1000 ${node.cpuUsage > 80 ? 'bg-red-400' : 'bg-blue-400'}`} style={{ width: `${node.cpuUsage}%` }} />
                </div>
              </div>
              <div>
                <div className="flex items-center justify-between text-xs mb-1.5">
                  <span className="text-zinc-400 flex items-center gap-1"><MemoryStick className="w-3 h-3" /> RAM</span>
                  <span className="text-zinc-300 font-mono">{node.ramUsage}%</span>
                </div>
                <div className="h-1.5 w-full bg-black/40 rounded-full overflow-hidden">
                  <div className={`h-full rounded-full transition-all duration-1000 ${node.cpuUsage > 80 ? 'bg-red-400' : 'bg-purple-400'}`} style={{ width: `${node.ramUsage}%` }} />
                </div>
              </div>
            </div>

            <div className="mt-auto pt-3 border-t border-white/5 flex items-center justify-between text-[10px] text-zinc-500 font-mono">
              <span>LAST SEEN</span>
              <span>{Math.floor((now - node.lastSeen.getTime()) / 60000)}m ago</span>
            </div>
          </div>
        ))}
      </div>

      {/* Provisioning Modal */}
      {isDeployModalOpen && (
        <div className="fixed inset-0 z-50 flex items-center justify-center p-4 bg-black/60 backdrop-blur-sm">
          <div className="glass-panel w-full max-w-3xl max-h-[90vh] overflow-y-auto relative animate-in fade-in zoom-in-95 duration-200">
            <button 
              onClick={() => setIsDeployModalOpen(false)}
              className="absolute top-6 right-6 p-2 rounded-lg bg-white/5 hover:bg-white/10 transition-colors text-zinc-400 hover:text-white"
            >
              <X className="w-5 h-5" />
            </button>
            
            <div className="p-8">
              <div className="mb-8">
                <h2 className="text-2xl font-light text-white mb-2">Deploy your Swarm</h2>
                <p className="text-zinc-400">Deploy the Kiwi daemon to your own cloud to securely execute tasks.</p>
              </div>

              <div className="space-y-8">
                {/* Step 1 */}
                <div>
                  <h3 className="text-lg font-medium text-white mb-4 flex items-center gap-3">
                    <span className="flex items-center justify-center w-6 h-6 rounded-full bg-white/10 text-xs font-bold border border-white/20">1</span>
                    Select Cloud Provider
                  </h3>
                  <div className="flex gap-4">
                    <button 
                      onClick={() => setProvider('AWS')}
                      className={`flex-1 p-4 rounded-xl border transition-all text-left ${provider === 'AWS' ? 'bg-white/10 border-white/30 shadow-[0_0_15px_rgba(255,255,255,0.1)]' : 'border-white/5 hover:bg-white/5'}`}
                    >
                      <div className="text-lg font-medium text-white mb-1">AWS</div>
                      <div className="text-sm text-zinc-400">Deploy via ECS/Fargate</div>
                    </button>
                    <button 
                      onClick={() => setProvider('GCP')}
                      className={`flex-1 p-4 rounded-xl border transition-all text-left ${provider === 'GCP' ? 'bg-white/10 border-white/30 shadow-[0_0_15px_rgba(255,255,255,0.1)]' : 'border-white/5 hover:bg-white/5'}`}
                    >
                      <div className="text-lg font-medium text-white mb-1">Google Cloud</div>
                      <div className="text-sm text-zinc-400">Deploy via Cloud Run</div>
                    </button>
                  </div>
                </div>

                {/* Step 2 */}
                <div>
                  <h3 className="text-lg font-medium text-white mb-4 flex items-center gap-3">
                    <span className="flex items-center justify-center w-6 h-6 rounded-full bg-white/10 text-xs font-bold border border-white/20">2</span>
                    Generate API Key
                  </h3>
                  <p className="text-sm text-zinc-400 mb-4">This key will be encrypted by the daemon&apos;s public key upon boot. It is never stored in plaintext on our servers.</p>
                  <div className="flex items-center gap-3">
                    <input 
                      type="text" 
                      readOnly
                      value={apiKey}
                      className="flex-1 bg-black/40 border border-white/10 rounded-lg px-4 py-3 text-white focus:outline-none focus:border-white/30 transition-colors font-mono"
                    />
                    <button 
                      onClick={generateKey}
                      disabled={isRegenerating}
                      className="flex items-center gap-2 px-4 py-3 bg-white/10 hover:bg-white/20 text-white rounded-lg transition-colors disabled:opacity-50"
                    >
                      <RefreshCw className={`w-4 h-4 ${isRegenerating ? 'animate-spin' : ''}`} />
                      <span>Regenerate</span>
                    </button>
                  </div>
                </div>

                {/* Step 3 */}
                <div>
                  <h3 className="text-lg font-medium text-white mb-4 flex items-center gap-3">
                    <span className="flex items-center justify-center w-6 h-6 rounded-full bg-white/10 text-xs font-bold border border-white/20">3</span>
                    Deploy via Terraform
                  </h3>
                  <div className="relative">
                    <div className="absolute top-4 right-4">
                      <button onClick={handleCopy} className="p-2 rounded bg-white/10 hover:bg-white/20 text-zinc-300 transition-colors">
                        {copied ? <Check className="w-4 h-4 text-green-400" /> : <Copy className="w-4 h-4" />}
                      </button>
                    </div>
                    <pre className="bg-black/60 border border-white/10 rounded-xl p-6 overflow-x-auto">
                      <code className="text-sm text-blue-300 font-mono">
                        {tfSnippet}
                      </code>
                    </pre>
                  </div>
                </div>
              </div>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
