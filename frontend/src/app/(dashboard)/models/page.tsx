"use client";

import { useEffect, useState } from "react";
import { client, BUILTIN_MODELS, RECOMMENDED_MODELS, type ModelEntry, type RecommendedModel } from "@/lib/api";
import { Cpu, Plus, Trash2, Loader2, AlertCircle, Check, Sparkles } from "lucide-react";

export default function ModelsPage() {
  const [models, setModels] = useState<ModelEntry[]>([]);
  const [name, setName] = useState("");
  const [provider, setProvider] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

  const load = () => client.listModels().then(r => setModels(r.models)).catch(() => {});
  useEffect(() => { load(); }, []);

  const add = async () => {
    setError("");
    if (!name.trim()) { setError("Model id is required (e.g. gemini-2.5-flash)."); return; }
    setBusy(true);
    try {
      await client.createModel(name.trim(), provider.trim());
      setName(""); setProvider("");
      await load();
    } catch (e) {
      setError(e instanceof Error ? e.message : "Failed to add model");
    } finally { setBusy(false); }
  };

  const remove = async (id: string) => {
    try { await client.deleteModel(id); await load(); } catch { /* ignore */ }
  };

  const existing = new Set([...BUILTIN_MODELS, ...models.map(m => m.name)]);
  const addRecommended = async (rec: RecommendedModel) => {
    setError(""); setBusy(true);
    try { await client.createModel(rec.id, rec.provider); await load(); }
    catch (e) { setError(e instanceof Error ? e.message : "Failed to add model"); }
    finally { setBusy(false); }
  };

  return (
    <div className="p-8 max-w-5xl mx-auto h-full flex flex-col text-white">
      <div className="mb-8">
        <h1 className="text-3xl font-light tracking-tight mb-2">Models</h1>
        <p className="text-zinc-400">Models available in the task form. Add a recommended one in a click, or enter any API model id your keys can reach.</p>
      </div>

      {/* Recommended — one-click add */}
      <div className="mb-8">
        <h2 className="flex items-center gap-2 text-xs font-bold text-zinc-500 uppercase tracking-widest mb-3">
          <Sparkles className="w-3.5 h-3.5 text-[#93C645]" /> Recommended
        </h2>
        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
          {RECOMMENDED_MODELS.map(rec => {
            const added = existing.has(rec.id);
            return (
              <div key={rec.id} className="glass-panel p-4 border border-white/10 rounded-xl flex items-center justify-between gap-3">
                <div className="min-w-0">
                  <div className="text-sm text-white truncate">{rec.label}</div>
                  <div className="text-xs text-zinc-500 truncate">
                    <span className="capitalize">{rec.provider}</span>{rec.note ? ` · ${rec.note}` : ""}
                  </div>
                </div>
                <button
                  onClick={() => addRecommended(rec)}
                  disabled={added || busy}
                  className={`shrink-0 flex items-center gap-1.5 text-xs px-3 py-1.5 rounded-lg border transition-colors ${added ? "border-green-500/20 bg-green-500/10 text-green-400 cursor-default" : "border-white/10 bg-white/5 text-white hover:bg-white/10"}`}
                >
                  {added ? <><Check className="w-3.5 h-3.5" /> Added</> : <><Plus className="w-3.5 h-3.5" /> Add</>}
                </button>
              </div>
            );
          })}
        </div>
        <p className="text-xs text-zinc-600 mt-3">Automatic discovery from your stored provider key is coming soon.</p>
      </div>

      <div className="glass-panel border border-white/10 rounded-2xl p-5 mb-8">
        <div className="flex flex-col md:flex-row gap-3 md:items-end">
          <div className="flex-1">
            <label className="block text-[10px] font-bold text-zinc-500 uppercase tracking-widest mb-1.5">Model id</label>
            <input value={name} onChange={e => setName(e.target.value)} placeholder="gemini-2.5-flash"
              className="w-full field text-sm" />
          </div>
          <div className="w-full md:w-52">
            <label className="block text-[10px] font-bold text-zinc-500 uppercase tracking-widest mb-1.5">Provider</label>
            <select value={provider} onChange={e => setProvider(e.target.value)}
              className="w-full field text-sm">
              <option value="">Auto-detect</option>
              <option value="anthropic">Anthropic</option>
              <option value="gemini">Gemini</option>
              <option value="codex">Codex</option>
            </select>
          </div>
          <button onClick={add} disabled={busy}
            className="flex items-center justify-center gap-2 btn-primary px-4 py-2 rounded-lg font-semibold disabled:opacity-50 h-[38px]">
            {busy ? <Loader2 className="w-4 h-4 animate-spin" /> : <Plus className="w-4 h-4" />} Add
          </button>
        </div>
        {error && <div className="flex items-center gap-2 text-red-400 text-sm mt-3"><AlertCircle className="w-4 h-4" />{error}</div>}
      </div>

      <h2 className="text-xs font-bold text-zinc-500 uppercase tracking-widest mb-3">Built-in</h2>
      <div className="grid grid-cols-1 md:grid-cols-3 gap-4 mb-8">
        {BUILTIN_MODELS.map(m => (
          <div key={m} className="glass-panel p-4 border border-white/10 rounded-xl flex items-center gap-3">
            <Cpu className="w-5 h-5 text-zinc-400" />
            <span className="font-mono text-sm">{m}</span>
          </div>
        ))}
      </div>

      <h2 className="text-xs font-bold text-zinc-500 uppercase tracking-widest mb-3">Custom</h2>
      {models.length === 0 ? (
        <p className="text-zinc-500 text-sm">No custom models yet.</p>
      ) : (
        <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
          {models.map(m => (
            <div key={m.id} className="glass-panel p-4 border border-white/10 rounded-xl flex items-center justify-between group">
              <div className="flex items-center gap-3 min-w-0">
                <Cpu className="w-5 h-5 text-zinc-400 shrink-0" />
                <div className="min-w-0">
                  <div className="font-mono text-sm truncate">{m.name}</div>
                  <div className="text-xs text-zinc-500">{m.provider || "auto"}</div>
                </div>
              </div>
              <button onClick={() => remove(m.id)} className="text-zinc-600 hover:text-red-400 transition-colors opacity-0 group-hover:opacity-100">
                <Trash2 className="w-4 h-4" />
              </button>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
