"use client";

import { useState } from "react";
import { AreaChart, Area, XAxis, YAxis, CartesianGrid, Tooltip, ResponsiveContainer } from 'recharts';
import { Key, DollarSign, Trash2, Plus } from "lucide-react";

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
  const [keys, setKeys] = useState([
    { id: '1', name: 'Production Daemon', key: 'kw_live_...x8f9', created: '2026-07-10' },
    { id: '2', name: 'Staging Daemon', key: 'kw_test_...a2b1', created: '2026-07-15' },
  ]);

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

        {/* API Keys */}
        <div className="glass-panel p-6 flex flex-col">
          <div className="flex items-center justify-between mb-6">
            <h2 className="text-lg font-medium text-white flex items-center gap-2">
              <Key className="w-5 h-5 text-blue-400" />
              API Keys
            </h2>
            <button className="flex items-center gap-1 text-xs font-medium bg-white text-black px-3 py-1.5 rounded-md hover:bg-zinc-200 transition-colors">
              <Plus className="w-3 h-3" /> Create Key
            </button>
          </div>

          <div className="space-y-3">
            {keys.map(k => (
              <div key={k.id} className="flex items-center justify-between p-3 rounded-lg bg-black/40 border border-white/5">
                <div>
                  <div className="text-sm font-medium text-white">{k.name}</div>
                  <div className="flex items-center gap-2 mt-1">
                    <span className="font-mono text-xs text-zinc-500">{k.key}</span>
                    <span className="text-[10px] text-zinc-600">&middot; Created {k.created}</span>
                  </div>
                </div>
                <button 
                  onClick={() => setKeys(keys.filter(key => key.id !== k.id))}
                  className="p-2 text-zinc-500 hover:text-red-400 hover:bg-red-400/10 rounded transition-colors"
                >
                  <Trash2 className="w-4 h-4" />
                </button>
              </div>
            ))}
            {keys.length === 0 && (
              <div className="text-center py-8 text-sm text-zinc-500 border border-dashed border-white/10 rounded-lg">
                No active API keys.
              </div>
            )}
          </div>
        </div>

      </div>
    </div>
  );
}
