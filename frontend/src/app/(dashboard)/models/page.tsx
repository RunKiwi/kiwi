"use client";

import { useEffect, useState } from "react";
import { client, RECOMMENDED_MODELS, providerLabel, modelClassLabel, planLabel, formatTokens, MODEL_CLASS_BLURB, CLASS_ORDER, type ModelEntry, type RecommendedModel, type ProviderInfo, type CatalogModel, type SpendResponse } from "@/lib/api";
import { Cpu, Plus, Trash2, Loader2, AlertCircle, Check, Sparkles, Box, ChevronRight } from "lucide-react";
import { Select } from "@/components/Select";
import Link from "next/link";

// Allowances reset on the first of the following month.
function nextResetLabel(period: string): string {
  const [y, m] = period.split("-").map(Number);
  if (!y || !m) return "next month";
  const d = new Date(Date.UTC(m === 12 ? y + 1 : y, m === 12 ? 0 : m, 1));
  return d.toLocaleDateString(undefined, { month: "short", day: "numeric" });
}

// Price per million tokens, which is the thing that actually distinguishes
// these models from each other and explains why the allowances differ so much.
function priceLabel(m: CatalogModel): string {
  const i = m.input_cost_per_m;
  const o = m.output_cost_per_m;
  if (i == null || o == null) return "price unknown";
  if (i === 0 && o === 0) return "free";
  return `$${i}/M in · $${o}/M out`;
}

export default function ModelsPage() {
  const [models, setModels] = useState<ModelEntry[]>([]);
  const [providers, setProviders] = useState<ProviderInfo[]>([]);
  const [catalog, setCatalog] = useState<CatalogModel[]>([]);
  const [spend, setSpend] = useState<SpendResponse | null>(null);
  const [name, setName] = useState("");
  const [provider, setProvider] = useState("");
  const [error, setError] = useState("");
  const [expanded, setExpanded] = useState<Record<string, boolean>>({});
  const [busy, setBusy] = useState(false);

  const load = () => {
    client.listModels().then(r => setModels(r.models)).catch(() => {});
    client.listProviders().then(r => setProviders(r.providers)).catch(() => {});
    client.listCatalogModels().then(r => setCatalog(r.models)).catch(() => {});
    
    const to = new Date().toISOString();
    const from = new Date(Date.now() - 30 * 24 * 3600 * 1000).toISOString();
    client.getSpend(from, to, "kiwi").then(r => setSpend(r)).catch(() => {});
  };
  useEffect(() => { load(); }, []);

  const connected = (prov: string) => providers.some(i => i.id === prov && i.connected);

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

  const existing = new Set([
    ...catalog.map(m => m.model_id),
    ...models.map(m => m.name)
  ]);

  const addRecommended = async (rec: RecommendedModel) => {
    setError(""); setBusy(true);
    try { await client.createModel(rec.id, rec.provider); await load(); }
    catch (e) { setError(e instanceof Error ? e.message : "Failed to add model"); }
    finally { setBusy(false); }
  };

  // Group catalog models by provider
  const catalogByProvider = catalog.reduce((acc, m) => {
    if (m.kiwi_provided) return acc;
    if (!acc[m.provider]) acc[m.provider] = [];
    acc[m.provider].push(m);
    return acc;
  }, {} as Record<string, CatalogModel[]>);

  // Group kiwi-provided models by tier
  const kiwiProvided = catalog.filter(m => m.kiwi_provided).reduce((acc, m) => {
    if (!acc[m.tier]) acc[m.tier] = [];
    acc[m.tier].push(m);
    return acc;
  }, {} as Record<string, CatalogModel[]>);

  const providerOptions = [
    { value: "", label: "Auto-detect" },
    ...providers.map(p => ({ value: p.id, label: p.display }))
  ];

  return (
    <div className="p-8 max-w-5xl mx-auto h-full flex flex-col text-white">
      <div className="mb-8">
        <h1 className="text-3xl font-light tracking-tight mb-2">Models</h1>
        <p className="text-zinc-400">Models available in the task form. Add a recommended one in a click, or enter any API model id your keys can reach.</p>
      </div>

      {/* Kiwi-provided */}
      <div className="mb-8">
        <h2 className="flex items-center gap-2 text-xs font-bold text-zinc-500 uppercase tracking-widest mb-3">
          <Box className="w-3.5 h-3.5 text-[#3b82f6]" /> Kiwi-Provided
        </h2>
        
        {spend?.allowance_stale && (
          <div className="mb-6 p-4 glass-panel border border-amber-500/20 rounded-xl text-sm text-amber-400/90">
            Your usage could not be read just now, so the remaining balances below are hidden rather than
            shown understated. Reload in a moment.
          </div>
        )}

        {spend && !spend.allowance_stale && spend.allowance && spend.allowance.length > 0 && (
          <div className="mb-6 p-6 glass-panel border border-white/10 rounded-xl">
            <div className="flex items-baseline justify-between mb-1">
              <h3 className="text-sm font-medium text-white">Your monthly allowance</h3>
              {spend.plan && <span className="text-xs text-zinc-500">{planLabel(spend.plan)}</span>}
            </div>
            <p className="text-xs text-zinc-500 mb-5">
              Tokens Kiwi pays for, reset each month. Models you connect your own key for are unlimited
              and draw on none of this.
            </p>
            <div className="flex flex-col gap-5">
              {spend.allowance.map(a => {
                const unlimited = a.granted < 0;
                const exhausted = !unlimited && a.remaining <= 0;
                const pct = unlimited ? 0 : Math.min(100, (a.used / Math.max(a.granted, 1)) * 100);
                return (
                  <div key={a.tier}>
                    <div className="flex justify-between items-baseline mb-1">
                      <span className="text-sm font-medium text-zinc-200">{modelClassLabel(a.tier)}</span>
                      <span className={`text-sm ${exhausted ? "text-red-400" : "text-zinc-400"}`}>
                        {unlimited ? (
                          "Unlimited"
                        ) : exhausted ? (
                          "Exhausted until " + nextResetLabel(a.period)
                        ) : (
                          <>
                            <span className="text-white font-medium">{formatTokens(a.remaining)}</span> left
                            <span className="text-zinc-600"> of {formatTokens(a.granted)}</span>
                          </>
                        )}
                      </span>
                    </div>
                    <div className="text-[11px] text-zinc-600 mb-2">{MODEL_CLASS_BLURB[a.tier]}</div>
                    <div className="w-full bg-zinc-800/50 rounded-full h-2 overflow-hidden border border-white/5">
                      <div
                        className={`h-full transition-all ${exhausted ? "bg-red-500/70" : "bg-[#93C645]"}`}
                        style={{ width: `${unlimited ? 100 : pct}%` }}
                      />
                    </div>
                  </div>
                );
              })}
            </div>
          </div>
        )}

        {providers.map(p => {
          if (p.kiwi_available) return null;
          return (
            <div key={p.id} className="mb-4 glass-panel p-4 border border-white/10 rounded-xl text-zinc-500 text-sm">
              <span className="font-semibold">{p.display}</span>: Coming soon
            </div>
          );
        })}

        {/* Collapsed by default. Rendering 105 cards flat, each stamped
            "Included", is what made a Free-plan account look like it had
            unlimited access to everything: the badge was the loudest thing on
            screen and it said yes 105 times. What actually differs between
            these models is what they cost you out of a fixed budget, so that
            is what the closed state shows. */}
        {CLASS_ORDER.filter(tier => kiwiProvided[tier]?.length).map(tier => {
          const tierModels = kiwiProvided[tier];
          const a = spend?.allowance?.find(x => x.tier === tier);
          const unlimited = a ? a.granted < 0 : false;
          const exhausted = !!a && !unlimited && a.remaining <= 0;
          const open = !!expanded[tier];
          return (
            <div key={tier} className="mb-3 glass-panel border border-white/10 rounded-xl overflow-hidden">
              <button
                onClick={() => setExpanded(e => ({ ...e, [tier]: !e[tier] }))}
                aria-expanded={open}
                className="w-full flex items-center gap-3 p-4 text-left hover:bg-white/[0.03] transition-colors"
              >
                <ChevronRight className={`w-4 h-4 shrink-0 text-zinc-500 transition-transform ${open ? "rotate-90" : ""}`} />
                <div className="min-w-0 flex-1">
                  <div className="flex items-baseline gap-2 flex-wrap">
                    <span className="text-sm font-medium text-white">{modelClassLabel(tier)}</span>
                    <span className="text-xs text-zinc-500">{tierModels.length} models</span>
                  </div>
                  <div className="text-[11px] text-zinc-600 mt-0.5">{MODEL_CLASS_BLURB[tier]}</div>
                </div>
                {a && (
                  <div className="shrink-0 text-right">
                    <div className={`text-sm ${exhausted ? "text-red-400" : "text-white"}`}>
                      {unlimited ? "Unlimited" : exhausted ? "0 left" : formatTokens(a.remaining) + " left"}
                    </div>
                    {!unlimited && (
                      <div className="text-[10px] text-zinc-600 uppercase tracking-widest">
                        of {formatTokens(a.granted)} / mo
                      </div>
                    )}
                  </div>
                )}
              </button>

              {open && (
                <div className="border-t border-white/5 divide-y divide-white/5">
                  {exhausted && (
                    <div className="px-4 py-2.5 text-xs text-red-400/90 bg-red-500/5">
                      This month&apos;s allowance is spent. These models will run again next month, or
                      immediately on your own provider key.
                    </div>
                  )}
                  {tierModels.map(m => (
                    <div key={m.model_id} className="px-4 py-3 flex items-center justify-between gap-3">
                      <div className="min-w-0">
                        <div className="text-sm text-zinc-200 break-words">{m.display_name}</div>
                        {m.description && (
                          <div className="text-[11px] text-zinc-500 mt-0.5 line-clamp-2">{m.description}</div>
                        )}
                        <div className="text-[11px] text-zinc-600 mt-0.5">{providerLabel(m.provider)}</div>
                      </div>
                      <div className="shrink-0 text-right text-[11px] text-zinc-500 tabular-nums">
                        {priceLabel(m)}
                      </div>
                    </div>
                  ))}
                </div>
              )}
            </div>
          );
        })}
        {Object.keys(kiwiProvided).length === 0 && !providers.some(p => !p.kiwi_available) && (
          <p className="text-zinc-500 text-sm">No Kiwi-provided models available.</p>
        )}
      </div>

      {/* Recommended — one-click add */}
      <div className="mb-8">
        <h2 className="flex items-center gap-2 text-xs font-bold text-zinc-500 uppercase tracking-widest mb-3">
          <Sparkles className="w-3.5 h-3.5 text-[#93C645]" /> Recommended
        </h2>
        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
          {RECOMMENDED_MODELS.map(rec => {
            const added = existing.has(rec.id);
            const isConnected = connected(rec.provider);
            return (
              <div key={rec.id} className="glass-panel p-4 border border-white/10 rounded-xl flex items-center justify-between gap-3">
                <div className="min-w-0">
                  <div className="text-sm text-white truncate">{rec.label}</div>
                  <div className="text-xs text-zinc-500 truncate">
                    <span>{providerLabel(rec.provider)}</span>{rec.note ? ` · ${rec.note}` : ""}
                    {!isConnected && <span className="ml-1 text-amber-500/80">(needs {providerLabel(rec.provider)} key)</span>}
                  </div>
                </div>
                {added ? (
                  <button disabled className="shrink-0 flex items-center gap-1.5 text-xs px-3 py-1.5 rounded-lg border transition-colors border-green-500/20 bg-green-500/10 text-green-400 cursor-default">
                    <Check className="w-3.5 h-3.5" /> Added
                  </button>
                ) : !isConnected ? (
                  <Link href="/integrations" className="shrink-0 flex items-center gap-1.5 text-xs px-3 py-1.5 rounded-lg border border-white/5 bg-white/5 text-zinc-500 hover:text-zinc-300 transition-colors">
                    Connect {providerLabel(rec.provider)} key &rarr;
                  </Link>
                ) : (
                  <button
                    onClick={() => addRecommended(rec)}
                    disabled={busy}
                    className="shrink-0 flex items-center gap-1.5 text-xs px-3 py-1.5 rounded-lg border transition-colors border-white/10 bg-white/5 text-white hover:bg-white/10"
                  >
                    {busy ? <Loader2 className="w-3.5 h-3.5 animate-spin" /> : <Plus className="w-3.5 h-3.5" />} Add
                  </button>
                )}
              </div>
            );
          })}
        </div>
        <p className="text-xs text-zinc-600 mt-3">Models you can run are based on your connected provider keys — connect more under <Link href="/integrations" className="underline hover:text-zinc-400">Integrations</Link>.</p>
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
            <Select ariaLabel="Provider" value={provider} onChange={setProvider} options={providerOptions} />
          </div>
          <button onClick={add} disabled={busy}
            className="flex items-center justify-center gap-2 btn-primary px-4 py-2 rounded-lg font-semibold disabled:opacity-50 h-[38px]">
            {busy ? <Loader2 className="w-4 h-4 animate-spin" /> : <Plus className="w-4 h-4" />} Add
          </button>
        </div>
        {error && <div className="flex items-center gap-2 text-red-400 text-sm mt-3"><AlertCircle className="w-4 h-4" />{error}</div>}
      </div>

      <h2 className="text-xs font-bold text-zinc-500 uppercase tracking-widest mb-3">Catalog</h2>
      {Object.keys(catalogByProvider).length === 0 ? (
        <div className="mb-8 p-4 glass-panel border border-white/10 rounded-xl text-zinc-500 text-sm">
          Connect a provider key in <Link href="/integrations" className="underline hover:text-white">Integrations</Link> to discover models.
        </div>
      ) : (
        <div className="mb-8">
          {Object.entries(catalogByProvider).sort(([a], [b]) => a.localeCompare(b)).map(([prov, provModels]) => (
            <div key={prov} className="mb-6">
              <h3 className="text-xs font-bold text-zinc-400 uppercase tracking-widest mb-2">{providerLabel(prov)}</h3>
              <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
                {provModels.map(m => (
                  <div key={m.model_id} className="glass-panel p-4 border border-white/10 rounded-xl flex items-center gap-3">
                    <Cpu className="w-5 h-5 text-zinc-400" />
                    <span className="font-mono text-sm">{m.display_name}</span>
                  </div>
                ))}
              </div>
            </div>
          ))}
        </div>
      )}

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
