"use client";

import { useEffect, useState } from "react";
import Link from "next/link";
import { Key, CheckCircle2, Building2, Server, Layers, Boxes, Cpu, ShieldCheck, XCircle, Bell, BellOff } from "lucide-react";
import { client, type Integration } from "@/lib/api";
import { PlanUsage } from "@/components/PlanUsage";
import { PlanComparison } from "@/components/PlanComparison";
import { isNotificationEnabled, setNotificationEnabled, requestNotificationPermission, getNotificationPermission } from "@/lib/notifications";

// Mirrors the Integrations catalog. Listed here for status only — Settings does
// not write credentials.
const PROVIDER_CREDENTIALS = [
  { key: "anthropic", label: "Anthropic", name: "ANTHROPIC_API_KEY" },
  { key: "gemini", label: "Gemini", name: "GEMINI_API_KEY" },
  { key: "git", label: "Git push token", name: "GIT_TOKEN" },
];

export default function SettingsPage() {
  const [org, setOrg] = useState<{ org_name: string; org_id: string; user_id: string; activation_state?: string; plan?: string } | null>(null);
  const [integrations, setIntegrations] = useState<Integration[]>([]);
  const [stats, setStats] = useState({ fleets: 0, daemons: 0, daemonsOnline: 0, jobs: 0, models: 0 });
  // Notification state is read after mount: Notification.permission does not
  // exist during prerender, so reading it in render would disagree with the
  // server-rendered markup and hydrate wrong.
  const [notifyOn, setNotifyOn] = useState(false);
  const [permission, setPermission] = useState<string>("default");


  useEffect(() => {
    // Browser-only values, so they can only be read once mounted — the same
    // reason the dashboard layout reads its org name this way.
    // eslint-disable-next-line react-hooks/set-state-in-effect
    setNotifyOn(isNotificationEnabled());
    setPermission(getNotificationPermission());
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
        <div className="grid grid-cols-1 sm:grid-cols-4 gap-4 text-sm">
          <div><div className="text-xs text-zinc-500 uppercase tracking-widest mb-1">Name</div><div className="text-white">{org?.org_name || "—"}</div></div>
          <div><div className="text-xs text-zinc-500 uppercase tracking-widest mb-1">Org ID</div><div className="font-mono text-zinc-300">{org?.org_id || "—"}</div></div>
          <div><div className="text-xs text-zinc-500 uppercase tracking-widest mb-1">Auth</div><div className="text-white flex items-center gap-1.5"><ShieldCheck className="w-4 h-4 text-green-400" /> API Key</div></div>
          <div>
            <div className="text-xs text-zinc-500 uppercase tracking-widest mb-1">Status</div>
            <div className="flex items-center gap-2">
              {(() => {
                // "Active" here means "can run tasks", not the paid activation step.
                // A Free org is created activation_state=inactive but runs fine on the
                // shared fleet — only a suspended org is actually blocked, so we show
                // ACTIVE for anything that isn't suspended (and isn't a paid org still
                // awaiting activation).
                const suspended = org?.activation_state === 'suspended';
                const inactivePaid = org?.activation_state !== 'active' && org?.plan !== 'free';
                const label = suspended ? 'SUSPENDED' : inactivePaid ? 'INACTIVE' : 'ACTIVE';
                const cls = suspended ? 'bg-red-500/10 text-red-400'
                  : inactivePaid ? 'bg-white/5 text-zinc-400'
                  : 'bg-green-500/10 text-green-400';
                return (
                  <span className={`inline-flex items-center px-2 py-0.5 rounded text-xs font-medium ${cls}`}>
                    {org ? label : '—'}
                  </span>
                );
              })()}
            </div>
          </div>
        </div>

        {org?.activation_state === 'suspended' && (
          <div id="activation" className="mt-4 p-4 rounded-xl border border-red-500/20 bg-red-500/10 scroll-mt-8">
            <h3 className="text-red-300 font-medium text-sm">Organization suspended</h3>
            <p className="text-red-300/80 text-sm mt-1">
              Running tasks is disabled. This can follow repeated abuse signals or exhausting your plan&apos;s limits.
              Contact{" "}
              <a href={`mailto:support@runkiwi.dev?subject=${encodeURIComponent(`Suspended org ${org?.org_id ?? ""}`)}`} className="underline hover:text-white">support</a>
              {" "}if you think this is a mistake.
            </p>
          </div>
        )}
      </div>

      {/* Plan & usage */}
      <PlanUsage />
      <PlanComparison currentPlan={org?.plan} />

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

      {/* Provider credentials — read-only here. Integrations owns the one write
          path, so the same key is not enterable from three different screens
          with three different forms. */}
      <div className="glass-panel p-6">
        <h2 className="text-lg font-medium text-white flex items-center gap-2 mb-5"><Key className="w-5 h-5 text-blue-400" /> Provider credentials</h2>
        <div className="space-y-3">
          {PROVIDER_CREDENTIALS.map(row => {
            const isConnected = integrations.some(i => i.key === row.key && i.connected);
            return (
              <div key={row.key} className="flex items-center justify-between gap-4 py-2 border-b border-white/5 last:border-0">
                <div className="min-w-0">
                  <div className="text-sm text-zinc-200">{row.label}</div>
                  <div className="font-mono text-[11px] text-zinc-500">{row.name}</div>
                </div>
                <span className={`flex items-center gap-1.5 text-xs shrink-0 ${isConnected ? "text-green-400" : "text-zinc-500"}`}>
                  {isConnected ? <CheckCircle2 className="w-3.5 h-3.5" /> : <XCircle className="w-3.5 h-3.5" />}
                  {isConnected ? "Connected" : "Not connected"}
                </span>
              </div>
            );
          })}
        </div>
        <p className="text-xs text-zinc-500 mt-4">
          Keys are encrypted at rest and never shown again.
          <Link href="/integrations" className="underline ml-1 hover:text-zinc-300">Add or replace a key in Integrations</Link>.
        </p>
      </div>

      {/* Desktop notifications */}
      <div className="glass-panel p-6">
        <h2 className="text-lg font-medium text-white flex items-center gap-2 mb-2">
          <Bell className="w-5 h-5 text-amber-400" /> Job notifications
        </h2>
        <p className="text-sm text-zinc-400 mb-4">
          Get a desktop notification when a job finishes, so you do not have to watch the tab.
        </p>

        <div className="flex items-center justify-between gap-4 p-4 rounded-xl border border-white/10 bg-black/20">
          <div className="flex flex-col min-w-0">
            <span className="text-sm font-medium text-white">Notify me when a job finishes</span>
            {permission === "denied" ? (
              <span className="text-xs text-amber-400/90">
                Blocked by your browser. Allow notifications for this site in the address bar, then switch this on.
              </span>
            ) : permission === "unsupported" ? (
              <span className="text-xs text-zinc-500">This browser does not support notifications.</span>
            ) : (
              <span className="text-xs text-zinc-500">
                {notifyOn ? "On — sent once per job, when it reaches a final state." : "Off"}
              </span>
            )}
          </div>

          <button
            type="button"
            role="switch"
            aria-checked={notifyOn}
            disabled={permission === "denied" || permission === "unsupported"}
            onClick={async () => {
              if (notifyOn) {
                setNotificationEnabled(false);
                setNotifyOn(false);
                return;
              }
              const granted = await requestNotificationPermission();
              setNotifyOn(granted);
              setPermission(getNotificationPermission());
            }}
            className={`flex items-center gap-2 px-4 py-2 rounded-xl text-xs font-semibold border transition-all shrink-0 disabled:opacity-40 disabled:cursor-not-allowed ${
              notifyOn
                ? "border-green-500/40 bg-green-500/20 text-green-300"
                : "border-white/10 bg-white/5 text-zinc-400 hover:text-white"
            }`}
          >
            {notifyOn ? <Bell className="w-4 h-4 text-green-400" /> : <BellOff className="w-4 h-4 text-zinc-400" />}
            <span>{notifyOn ? "On" : "Turn on"}</span>
          </button>
        </div>
      </div>
    </div>
  );
}
