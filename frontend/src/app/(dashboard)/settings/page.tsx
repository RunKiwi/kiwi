"use client";

import { useEffect, useState } from "react";
import { Key, CheckCircle2, Loader2, Building2, Server, Layers, Boxes, Cpu, ShieldCheck, XCircle } from "lucide-react";
import { client, type Integration } from "@/lib/api";

export default function SettingsPage() {
  const [org, setOrg] = useState<{ org_name: string; org_id: string; user_id: string } | null>(null);
  const [integrations, setIntegrations] = useState<Integration[]>([]);
  const [stats, setStats] = useState({ fleets: 0, daemons: 0, daemonsOnline: 0, jobs: 0, models: 0 });

  const [anthropicKey, setAnthropicKey] = useState("");
  const [geminiKey, setGeminiKey] = useState("");
  const [gitToken, setGitToken] = useState("");
  const [saving, setSaving] = useState<string | null>(null);
  const [saved, setSaved] = useState<string | null>(null);

  useEffect(() => {
    client.validate().then(setOrg).catch(() => {});
    client.listIntegrations().then(r => setIntegrations(r.integrations)).catch(() => {});
    Promise.all([
      client.listFleets().then(r => r.fleets.length).catch(() => 0),
      client.listDaemons().then(d => ({ total: d.length, online: d.filter(x => x.online).length })).catch(() => ({ total: 0, online: 0 })),
      client.listJobs().then(r => r.jobs.length).catch(() => 0),
      client.listModels().then(r => r.models.length).catch(() => 0),
    ]).then(([fleets, daemons, jobs, models]) =>
      setStats({ fleets, daemons: daemons.total, daemonsOnline: daemons.online, jobs, models }));
  }, []);

  const save = async (label: string, name: string, kind: string, value: string, clear: (v: string) => void) => {
    if (!value.trim()) return;
    setSaving(label); setSaved(null);
    try { await client.setCredential(name, kind, value); setSaved(label); clear(""); setTimeout(() => setSaved(null), 3000); }
    catch { alert("Failed to save credential"); }
    finally { setSaving(null); }
  };

  const statCards = [
    { label: "Fleets", value: stats.fleets, icon: Layers },
    { label: "Daemons", value: `${stats.daemonsOnline}/${stats.daemons}`, sub: "online", icon: Server },
    { label: "Jobs", value: stats.jobs, icon: Boxes },
    { label: "Models", value: stats.models, sub: "custom", icon: Cpu },
  ];

  return (
    <div className="p-8 max-w-5xl mx-auto flex flex-col gap-8">
      <div>
        <h1 className="text-3xl font-light tracking-tight text-white mb-2">Settings</h1>
        <p className="text-zinc-400">Your organization, connections, and provider credentials.</p>
      </div>

      {/* Organization */}
      <div className="glass-panel p-6">
        <h2 className="text-lg font-medium text-white flex items-center gap-2 mb-4"><Building2 className="w-5 h-5 text-zinc-300" /> Organization</h2>
        <div className="grid grid-cols-1 sm:grid-cols-3 gap-4 text-sm">
          <div><div className="text-xs text-zinc-500 uppercase tracking-widest mb-1">Name</div><div className="text-white">{org?.org_name || "—"}</div></div>
          <div><div className="text-xs text-zinc-500 uppercase tracking-widest mb-1">Org ID</div><div className="font-mono text-zinc-300">{org?.org_id || "—"}</div></div>
          <div><div className="text-xs text-zinc-500 uppercase tracking-widest mb-1">Auth</div><div className="text-white flex items-center gap-1.5"><ShieldCheck className="w-4 h-4 text-green-400" /> API Key</div></div>
        </div>
      </div>

      {/* Overview stats (real data) */}
      <div className="grid grid-cols-2 lg:grid-cols-4 gap-4">
        {statCards.map(s => (
          <div key={s.label} className="glass-panel p-5 flex flex-col gap-2">
            <s.icon className="w-5 h-5 text-zinc-400" />
            <div className="text-2xl font-light text-white">{s.value} {s.sub && <span className="text-xs text-zinc-500">{s.sub}</span>}</div>
            <div className="text-xs text-zinc-500 uppercase tracking-widest">{s.label}</div>
          </div>
        ))}
      </div>

      {/* Integrations status */}
      <div className="glass-panel p-6">
        <h2 className="text-lg font-medium text-white mb-4">Connections</h2>
        <div className="flex flex-wrap gap-2">
          {integrations.map(i => (
            <span key={i.key} className={`flex items-center gap-1.5 px-3 py-1.5 rounded-full text-xs border ${i.connected ? "bg-green-500/10 border-green-500/20 text-green-300" : "bg-white/5 border-white/10 text-zinc-500"}`}>
              {i.connected ? <CheckCircle2 className="w-3.5 h-3.5" /> : <XCircle className="w-3.5 h-3.5" />}{i.key}
            </span>
          ))}
        </div>
      </div>

      {/* Provider credentials */}
      <div className="glass-panel p-6">
        <h2 className="text-lg font-medium text-white flex items-center gap-2 mb-5"><Key className="w-5 h-5 text-blue-400" /> Provider Credentials</h2>
        <div className="space-y-5">
          {([
            { label: "Anthropic", name: "ANTHROPIC_API_KEY", kind: "llm", ph: "sk-ant-…", val: anthropicKey, set: setAnthropicKey },
            { label: "Gemini", name: "GEMINI_API_KEY", kind: "llm", ph: "AIza…", val: geminiKey, set: setGeminiKey },
            { label: "GitHub token", name: "GIT_TOKEN", kind: "git", ph: "github_pat_…", val: gitToken, set: setGitToken },
          ]).map(row => (
            <div key={row.label}>
              <label className="block text-sm font-medium text-zinc-300 mb-1.5">{row.label}</label>
              <div className="flex gap-2">
                <input type="password" value={row.val} onChange={e => row.set(e.target.value)} placeholder={row.ph}
                  className="flex-1 bg-white/5 border border-white/10 rounded-lg px-3 py-2 text-sm text-white focus:border-blue-500/50 focus:outline-none" />
                <button onClick={() => save(row.label, row.name, row.kind, row.val, row.set)} disabled={saving === row.label || !row.val.trim()}
                  className="bg-white text-black px-4 py-2 rounded-lg text-sm font-medium hover:bg-zinc-200 disabled:opacity-50 flex items-center gap-2 min-w-[80px] justify-center">
                  {saving === row.label ? <Loader2 className="w-4 h-4 animate-spin" /> : saved === row.label ? <CheckCircle2 className="w-4 h-4 text-green-600" /> : 'Save'}
                </button>
              </div>
            </div>
          ))}
        </div>
        <p className="text-xs text-zinc-500 mt-4">Keys are encrypted at rest and never shown again. Manage all connections under Integrations.</p>
      </div>
    </div>
  );
}
