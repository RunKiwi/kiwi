"use client";

import { useState, useEffect } from "react";
import { Copy, Check, RefreshCw } from "lucide-react";

export default function OnboardingPage() {
  const [provider, setProvider] = useState<'AWS' | 'GCP'>('AWS');
  const [apiKey, setApiKey] = useState('');
  const [copied, setCopied] = useState(false);
  const [isRegenerating, setIsRegenerating] = useState(false);

  const generateKey = () => {
    setIsRegenerating(true);
    // Simulate generation delay for visual feedback
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
    generateKey();
  }, []);

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

  return (
    <div className="p-8 max-w-4xl mx-auto">
      <div className="mb-8">
        <h1 className="text-3xl font-light tracking-tight text-white mb-2">Deploy your Swarm</h1>
        <p className="text-zinc-400">Deploy the Kiwi daemon to your own cloud to securely execute tasks.</p>
      </div>

      <div className="glass-panel p-8">
        <div className="space-y-8">
          
          {/* Step 1 */}
          <div>
            <h2 className="text-lg font-medium text-white mb-4 flex items-center gap-3">
              <span className="flex items-center justify-center w-6 h-6 rounded-full bg-white/10 text-xs font-bold border border-white/20">1</span>
              Select Cloud Provider
            </h2>
            <div className="flex gap-4">
              <button 
                onClick={() => setProvider('AWS')}
                className={`flex-1 p-4 rounded-xl border transition-all ${provider === 'AWS' ? 'bg-white/10 border-white/30 shadow-[0_0_15px_rgba(255,255,255,0.1)]' : 'border-white/5 hover:bg-white/5'}`}
              >
                <div className="text-lg font-medium text-white mb-1">AWS</div>
                <div className="text-sm text-zinc-400">Deploy via ECS/Fargate</div>
              </button>
              <button 
                onClick={() => setProvider('GCP')}
                className={`flex-1 p-4 rounded-xl border transition-all ${provider === 'GCP' ? 'bg-white/10 border-white/30 shadow-[0_0_15px_rgba(255,255,255,0.1)]' : 'border-white/5 hover:bg-white/5'}`}
              >
                <div className="text-lg font-medium text-white mb-1">Google Cloud</div>
                <div className="text-sm text-zinc-400">Deploy via Cloud Run</div>
              </button>
            </div>
          </div>

          {/* Step 2 */}
          <div>
            <h2 className="text-lg font-medium text-white mb-4 flex items-center gap-3">
              <span className="flex items-center justify-center w-6 h-6 rounded-full bg-white/10 text-xs font-bold border border-white/20">2</span>
              Generate API Key
            </h2>
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
            <h2 className="text-lg font-medium text-white mb-4 flex items-center gap-3">
              <span className="flex items-center justify-center w-6 h-6 rounded-full bg-white/10 text-xs font-bold border border-white/20">3</span>
              Deploy via Terraform
            </h2>
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
  );
}
