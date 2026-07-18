"use client";

import { useState } from "react";
import { AreaChart, Area, XAxis, YAxis, CartesianGrid, Tooltip, ResponsiveContainer } from 'recharts';
import { Key, DollarSign, CheckCircle2, Loader2 } from "lucide-react";
import { client } from "@/lib/api";

const mockCostData = [
  { name: 'Mon', cost: 12.5 },
  { name: 'Tue', cost: 18.2 },
  { name: 'Wed', cost: 25.1 },
  { name: 'Thu', cost: 14.8 },
  { name: 'Fri', cost: 32.4 },
  { name: 'Sat', cost: 45.0 },
  { name: 'Sun', cost: 28.9 },
];

export default function SettingsPage() {
  const [anthropicKey, setAnthropicKey] = useState("");
  const [geminiKey, setGeminiKey] = useState("");
  const [gitToken, setGitToken] = useState("");
  
  const [savingAnthropic, setSavingAnthropic] = useState(false);
  const [savingGemini, setSavingGemini] = useState(false);
  const [savingGit, setSavingGit] = useState(false);

  const [anthropicSaved, setAnthropicSaved] = useState(false);
  const [geminiSaved, setGeminiSaved] = useState(false);
  const [gitSaved, setGitSaved] = useState(false);

  const handleSaveCredential = async (
    name: string, 
    kind: string, 
    value: string, 
    setSaving: (v: boolean) => void,
    setSaved: (v: boolean) => void,
    setValue: (v: string) => void
  ) => {
    if (!value.trim()) return;
    setSaving(true);
    setSaved(false);
    try {
      await client.setCredential(name, kind, value);
      setSaved(true);
      setValue(""); // clear the input on success as we never render stored values
      setTimeout(() => setSaved(false), 3000); // clear success msg after 3s
    } catch (err) {
      console.error(err);
      alert("Failed to save credential");
    } finally {
      setSaving(false);
    }
  };

  return (
    <div className="p-8 max-w-5xl mx-auto flex flex-col gap-8">
      <div>
        <h1 className="text-3xl font-light tracking-tight text-white mb-2">Settings & Billing</h1>
        <p className="text-zinc-400">Manage your organization&apos;s API keys and monitor Swarm execution costs.</p>
      </div>

      <div className="grid grid-cols-1 lg:grid-cols-2 gap-8">
        {/* Billing Chart */}
        <div className="glass-panel p-6 flex flex-col">
          <div className="flex items-center justify-between mb-6">
            <h2 className="text-lg font-medium text-white flex items-center gap-2">
              <DollarSign className="w-5 h-5 text-green-400" />
              Token Usage Costs
            </h2>
            <span className="text-2xl font-light text-white">$176.90 <span className="text-sm text-zinc-500">/ week</span></span>
          </div>
          
          <div className="flex-1 min-h-[250px]">
            <ResponsiveContainer width="100%" height="100%">
              <AreaChart data={mockCostData} margin={{ top: 10, right: 10, left: -20, bottom: 0 }}>
                <defs>
                  <linearGradient id="colorCost" x1="0" y1="0" x2="0" y2="1">
                    <stop offset="5%" stopColor="#4ade80" stopOpacity={0.3}/>
                    <stop offset="95%" stopColor="#4ade80" stopOpacity={0}/>
                  </linearGradient>
                </defs>
                <CartesianGrid strokeDasharray="3 3" stroke="rgba(255,255,255,0.05)" vertical={false} />
                <XAxis dataKey="name" stroke="rgba(255,255,255,0.3)" fontSize={12} tickLine={false} axisLine={false} />
                <YAxis stroke="rgba(255,255,255,0.3)" fontSize={12} tickLine={false} axisLine={false} tickFormatter={(v) => `$${v}`} />
                <Tooltip 
                  contentStyle={{ backgroundColor: 'rgba(0,0,0,0.8)', border: '1px solid rgba(255,255,255,0.1)', borderRadius: '8px' }}
                  itemStyle={{ color: '#4ade80' }}
                />
                <Area type="monotone" dataKey="cost" stroke="#4ade80" strokeWidth={2} fillOpacity={1} fill="url(#colorCost)" />
              </AreaChart>
            </ResponsiveContainer>
          </div>
        </div>

        {/* API Credentials */}
        <div className="glass-panel p-6 flex flex-col">
          <div className="flex items-center justify-between mb-6">
            <h2 className="text-lg font-medium text-white flex items-center gap-2">
              <Key className="w-5 h-5 text-blue-400" />
              Provider Credentials
            </h2>
          </div>

          <div className="space-y-6">
            {/* Anthropic */}
            <div>
              <label className="block text-sm font-medium text-zinc-300 mb-1.5">Anthropic API Key</label>
              <div className="flex gap-2">
                <input 
                  type="password" 
                  value={anthropicKey} 
                  onChange={e => setAnthropicKey(e.target.value)}
                  placeholder="sk-ant-..." 
                  className="flex-1 bg-white/5 border border-white/10 rounded-lg px-3 py-2 text-sm text-white focus:border-blue-500/50 focus:outline-none transition-colors" 
                />
                <button 
                  onClick={() => handleSaveCredential('ANTHROPIC_API_KEY', 'llm', anthropicKey, setSavingAnthropic, setAnthropicSaved, setAnthropicKey)}
                  disabled={savingAnthropic || !anthropicKey.trim()}
                  className="bg-white text-black px-4 py-2 rounded-lg text-sm font-medium hover:bg-zinc-200 transition-colors disabled:opacity-50 flex items-center gap-2 min-w-[80px] justify-center"
                >
                  {savingAnthropic ? <Loader2 className="w-4 h-4 animate-spin" /> : (anthropicSaved ? <CheckCircle2 className="w-4 h-4 text-green-600" /> : 'Save')}
                </button>
              </div>
            </div>

            {/* Gemini */}
            <div>
              <label className="block text-sm font-medium text-zinc-300 mb-1.5">Gemini API Key</label>
              <div className="flex gap-2">
                <input 
                  type="password" 
                  value={geminiKey} 
                  onChange={e => setGeminiKey(e.target.value)}
                  placeholder="AIzaSy..." 
                  className="flex-1 bg-white/5 border border-white/10 rounded-lg px-3 py-2 text-sm text-white focus:border-blue-500/50 focus:outline-none transition-colors" 
                />
                <button 
                  onClick={() => handleSaveCredential('GEMINI_API_KEY', 'llm', geminiKey, setSavingGemini, setGeminiSaved, setGeminiKey)}
                  disabled={savingGemini || !geminiKey.trim()}
                  className="bg-white text-black px-4 py-2 rounded-lg text-sm font-medium hover:bg-zinc-200 transition-colors disabled:opacity-50 flex items-center gap-2 min-w-[80px] justify-center"
                >
                  {savingGemini ? <Loader2 className="w-4 h-4 animate-spin" /> : (geminiSaved ? <CheckCircle2 className="w-4 h-4 text-green-600" /> : 'Save')}
                </button>
              </div>
            </div>

            {/* Git */}
            <div>
              <label className="block text-sm font-medium text-zinc-300 mb-1.5">GitHub Personal Access Token</label>
              <div className="flex gap-2">
                <input 
                  type="password" 
                  value={gitToken} 
                  onChange={e => setGitToken(e.target.value)}
                  placeholder="ghp_..." 
                  className="flex-1 bg-white/5 border border-white/10 rounded-lg px-3 py-2 text-sm text-white focus:border-blue-500/50 focus:outline-none transition-colors" 
                />
                <button 
                  onClick={() => handleSaveCredential('GIT_TOKEN', 'git', gitToken, setSavingGit, setGitSaved, setGitToken)}
                  disabled={savingGit || !gitToken.trim()}
                  className="bg-white text-black px-4 py-2 rounded-lg text-sm font-medium hover:bg-zinc-200 transition-colors disabled:opacity-50 flex items-center gap-2 min-w-[80px] justify-center"
                >
                  {savingGit ? <Loader2 className="w-4 h-4 animate-spin" /> : (gitSaved ? <CheckCircle2 className="w-4 h-4 text-green-600" /> : 'Save')}
                </button>
              </div>
              <p className="text-xs text-zinc-500 mt-2">Requires repo scope for PRs.</p>
            </div>
          </div>
        </div>

      </div>
    </div>
  );
}
