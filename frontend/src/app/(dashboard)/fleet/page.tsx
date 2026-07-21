"use client";

import { useFleetStore } from "@/store/useFleetStore";
import { useEffect, useState } from "react";
import { Server, Activity, Clock, Plus, Cloud, Building2, Loader2, KeyRound, Copy, Check, AlertCircle } from "lucide-react";
import { client, type Fleet, type UsageResponse } from "@/lib/api";

export default function FleetPage() {
  const { daemons, loadDaemons } = useFleetStore();
  const [fleets, setFleets] = useState<Fleet[]>([]);
  // Plan/usage gates the whole surface. `usageLoaded` lets us hold the
  // plan-dependent UI until we know the plan, so a Free user never flashes the
  // Pro fleet-CRUD (nor a Pro user the Free runtime card) on first paint.
  const [u, setU] = useState<UsageResponse | null>(null);
  const [usageLoaded, setUsageLoaded] = useState(false);
  const [name, setName] = useState("");
  const [type, setType] = useState<"managed" | "byoc">("managed");
  const [busy, setBusy] = useState(false);
  // A minted join token is scoped to one fleet — track which fleet it belongs to
  // so daemons enrol into the fleet whose tasks they should lease.
  const [token, setToken] = useState<{ fleetId: string; value: string } | null>(null);
  const [copied, setCopied] = useState(false);
  const [err, setErr] = useState("");

  const loadFleets = () => client.listFleets().then(r => setFleets(r.fleets)).catch(() => {});

  useEffect(() => {
    loadDaemons();
    loadFleets();
    client.getUsage().then(setU).catch(() => setU(null)).finally(() => setUsageLoaded(true));
    const interval = setInterval(loadDaemons, 5000);
    return () => clearInterval(interval);
  }, [loadDaemons]);

  const create = async () => {
    setErr("");
    if (!name.trim()) { setErr("Give the fleet a name."); return; }
    setBusy(true);
    try { await client.createFleet(name.trim(), type); setName(""); await loadFleets(); }
    catch (e) { setErr(e instanceof Error ? e.message : "Failed to create fleet"); }
    finally { setBusy(false); }
  };

  const mintToken = async (fleetId: string) => {
    try { const r = await client.mintJoinToken(fleetId); setToken({ fleetId, value: r.join_token }); }
    catch { /* ignore */ }
  };

  const copyToken = () => {
    if (token) { navigator.clipboard?.writeText(token.value); setCopied(true); setTimeout(() => setCopied(false), 1500); }
  };

  const isFree = u?.plan === "free";
  const hasCap = (u?.agent_minutes_limit ?? 0) > 0;
  const daemonsOnline = daemons.filter(d => d.online).length;
  // Positively-known views only: hold both until usage resolves. On a failed
  // fetch (`u === null` after load) we fall back to the Pro surface — harmless,
  // since the backend still refuses fleet creation for a Free org.
  const showFree = usageLoaded && isFree;
  const showPro = usageLoaded && !isFree;

  return (
    <div className="p-8 max-w-7xl mx-auto h-full flex flex-col text-white">
      <div className="mb-8">
        <h1 className="text-3xl font-light tracking-tight mb-2">Fleets</h1>
        <p className="text-zinc-400">Groups of execution capacity. <b>Managed</b> runs on Kiwi; <b>BYOC</b> runs daemons in your own cloud.</p>
      </div>

      {/* Hold the plan-dependent surface until usage resolves, so neither view flashes. */}
      {!usageLoaded && (
        <div className="glass-panel border border-white/10 rounded-2xl p-5 mb-6 h-24 animate-pulse" />
      )}

      {/* Create fleet */}
      {showPro && (
        <div className="glass-panel border border-white/10 rounded-2xl p-5 mb-6">
          <div className="flex flex-col md:flex-row gap-3 md:items-end">
            <div className="flex-1">
              <label className="block text-[10px] font-bold text-zinc-500 uppercase tracking-widest mb-1.5">Fleet name</label>
              <input value={name} onChange={e => setName(e.target.value)} placeholder="production"
                className="w-full field text-sm" />
            </div>
            <div>
              <label className="block text-[10px] font-bold text-zinc-500 uppercase tracking-widest mb-1.5">Type</label>
              <div className="flex bg-white/5 border border-white/10 rounded-xl p-1 gap-1">
                <button onClick={() => setType("managed")} className={`flex items-center gap-2 px-3 py-2 rounded-lg text-sm border transition-colors ${type === "managed" ? "bg-white/10 border-white/30 text-white" : "bg-white/5 border-white/10 text-zinc-400"}`}>
                  <Cloud className="w-4 h-4" /> Managed
                </button>
                <button onClick={() => setType("byoc")} className={`flex items-center gap-2 px-3 py-2 rounded-lg text-sm border transition-colors ${type === "byoc" ? "bg-white/10 border-white/30 text-white" : "bg-white/5 border-white/10 text-zinc-400"}`}>
                  <Server className="w-4 h-4" /> BYOC
                </button>
              </div>
            </div>
            <button onClick={create} disabled={busy} className="flex items-center justify-center gap-2 btn-primary px-4 py-2 rounded-lg font-semibold disabled:opacity-50 h-[38px]">
              {busy ? <Loader2 className="w-4 h-4 animate-spin" /> : <Plus className="w-4 h-4" />} Create
            </button>
          </div>
          {err && <div className="flex items-center gap-2 text-red-400 text-sm mt-3"><AlertCircle className="w-4 h-4 shrink-0" />{err}</div>}
          {type === "byoc" && (
            <p className="mt-4 pt-4 border-t border-white/5 text-sm text-zinc-400 flex items-center gap-2">
              <KeyRound className="w-4 h-4" /> After creating a BYOC fleet, generate a per-fleet join token below to enrol a daemon into it.
            </p>
          )}
        </div>
      )}

      {/* Fleets or Runtime */}
      {showFree ? (
        <div className="glass-panel p-6 border border-white/10 rounded-2xl mb-8 flex flex-col gap-3">
          <div className="flex items-center gap-3 mb-2">
            <Cloud className="w-6 h-6 text-green-400" />
            <div>
              <div className="font-medium text-base text-white">Running on <span className="font-bold">Kiwi Managed</span> (shared)</div>
              {/* Free runs a per-org daemon on demand: it starts on submit and is
                  reclaimed when idle. So "no daemon" is the normal resting state,
                  not an outage — show a neutral "idle", never a red "offline". */}
              <div className="text-sm text-zinc-400 flex items-center gap-2">
                <span className="relative flex h-2 w-2">
                  {daemonsOnline > 0 && <span className="animate-ping absolute inline-flex h-full w-full rounded-full bg-green-400 opacity-75"></span>}
                  <span className={`relative inline-flex rounded-full h-2 w-2 ${daemonsOnline > 0 ? 'bg-green-500' : 'bg-amber-500'}`}></span>
                </span>
                {daemonsOnline > 0 ? "daemon online" : "idle · starts on your next task"}
              </div>
            </div>
          </div>
          {u && (
            <div className="text-sm text-zinc-300">
              {u.agent_minutes_used.toFixed(1)} {hasCap ? `/ ${u.agent_minutes_limit}` : ""} agent-min this month
            </div>
          )}
          <a href="/settings" className="text-sm text-blue-400 hover:text-blue-300 transition-colors inline-block mt-2 font-medium">
            Upgrade for a dedicated fleet &rarr;
          </a>
        </div>
      ) : showPro && fleets.length > 0 && (
        <div className="grid grid-cols-1 md:grid-cols-3 gap-4 mb-8">
          {fleets.map(f => (
            <div key={f.id} className="glass-panel p-4 border border-white/10 rounded-xl flex flex-col gap-3">
              <div className="flex items-center gap-3">
                {f.type === "byoc" ? <Building2 className="w-5 h-5 text-blue-400" /> : <Cloud className="w-5 h-5 text-green-400" />}
                <div>
                  <div className="font-medium text-sm">{f.name}</div>
                  <div className="text-xs text-zinc-500">{f.type === "byoc" ? "BYOC" : "Managed"}</div>
                </div>
              </div>
              {f.type === "byoc" && (
                <button onClick={() => mintToken(f.id)}
                  className="flex items-center justify-center gap-2 text-xs bg-white/5 border border-white/10 hover:bg-white/10 rounded-lg px-3 py-1.5">
                  <KeyRound className="w-3.5 h-3.5" /> Generate join token
                </button>
              )}
              {token?.fleetId === f.id && (
                <div className="flex items-center gap-2 bg-black/40 border border-white/10 rounded-lg px-3 py-2">
                  <code className="text-xs text-green-300 truncate flex-1">{token.value}</code>
                  <button onClick={copyToken} className="text-zinc-400 hover:text-white shrink-0">{copied ? <Check className="w-4 h-4 text-green-400" /> : <Copy className="w-4 h-4" />}</button>
                </div>
              )}
            </div>
          ))}
        </div>
      )}

      {/* Daemons */}
      {showPro && (
        <>
          <h2 className="text-xs font-bold text-zinc-500 uppercase tracking-widest mb-3">Daemons</h2>
          {daemons.length === 0 ? (
            <div className="glass-panel border border-white/5 rounded-2xl p-10 flex flex-col items-center justify-center text-center">
              <Server className="w-10 h-10 text-zinc-700 mb-3" />
              <p className="text-zinc-400 max-w-md">No daemons yet. Create a BYOC fleet and register a daemon with a join token, or wait for your managed daemon to be launched.</p>
            </div>
          ) : (
        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-6">
          {daemons.map(d => (
            <div key={d.id} className="glass-panel p-6 border border-white/5 rounded-2xl group flex flex-col">
              <div className="flex items-start gap-3 mb-6">
                <div className="w-10 h-10 rounded-xl bg-white/5 border border-white/10 flex items-center justify-center">
                  <Server className="w-5 h-5 text-zinc-300" />
                </div>
                <div>
                  <h3 className="text-sm font-medium truncate max-w-[150px]" title={d.id}>{d.id}</h3>
                  <div className="flex items-center gap-1.5 mt-1">
                    <span className="relative flex h-2 w-2">
                      {d.online && <span className="animate-ping absolute inline-flex h-full w-full rounded-full bg-green-400 opacity-75"></span>}
                      <span className={`relative inline-flex rounded-full h-2 w-2 ${d.online ? 'bg-green-500' : 'bg-red-500'}`}></span>
                    </span>
                    <span className="text-xs text-zinc-500 font-medium">{d.online ? 'Online' : 'Offline'}</span>
                  </div>
                </div>
              </div>
              <div className="mt-auto space-y-3 border-t border-white/5 pt-4">
                <div className="flex items-center justify-between text-xs">
                  <div className="flex items-center gap-1.5 text-zinc-500"><Cloud className="w-3.5 h-3.5" /><span>Fleet</span></div>
                  <span className="text-zinc-300">{d.fleet_id ? (fleets.find(f => f.id === d.fleet_id)?.name ?? d.fleet_id) : 'Unassigned'}</span>
                </div>
                <div className="flex items-center justify-between text-xs">
                  <div className="flex items-center gap-1.5 text-zinc-500"><Activity className="w-3.5 h-3.5" /><span>Last Seen</span></div>
                  <span className="text-zinc-300">{d.last_seen_at ? new Date(d.last_seen_at).toLocaleTimeString() : 'Never'}</span>
                </div>
                <div className="flex items-center justify-between text-xs">
                  <div className="flex items-center gap-1.5 text-zinc-500"><Clock className="w-3.5 h-3.5" /><span>Registered</span></div>
                  <span className="text-zinc-300">{new Date(d.created_at).toLocaleDateString()}</span>
                </div>
              </div>
            </div>
          ))}
        </div>
      )}
      </>
      )}
    </div>
  );
}
