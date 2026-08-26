"use client";

import React, { useEffect, useState } from "react";
import Link from "next/link";
import {
  CreditCard,
  Building2,
  Server,
  Layers,
  ShieldCheck,
  Bell,
  BellOff,
  Receipt,
  ExternalLink,
  Copy,
  Check,
  Gauge,
  Coins,
} from "lucide-react";
import { client, api, type ValidateResponse, type UsageResponse } from "@/lib/api";
import { PlanUsage } from "@/components/PlanUsage";
import { PlanComparison } from "@/components/PlanComparison";
import { UpgradeButton } from "@/components/UpgradeButton";
import { Logo } from "@/components/Logo";
import {
  isNotificationEnabled,
  setNotificationEnabled,
  requestNotificationPermission,
  getNotificationPermission,
} from "@/lib/notifications";

export default function SettingsPage() {
  const [org, setOrg] = useState<ValidateResponse | null>(null);
  const [usage, setUsage] = useState<UsageResponse | null>(null);
  const [copiedOrgId, setCopiedOrgId] = useState(false);
  const [stats, setStats] = useState({ fleets: 0, daemons: 0, daemonsOnline: 0, jobs: 0, models: 0 });

  // Notification state
  const [notifyOn, setNotifyOn] = useState(false);
  const [permission, setPermission] = useState<string>("default");

  // Default model source: which payer a submit with no explicit worker
  // model falls back to (a channel-unbound Slack trigger, most commonly).
  const [modelSource, setModelSourceState] = useState<"kiwi" | "byok">("kiwi");
  const [modelSourceSaving, setModelSourceSaving] = useState(false);

  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect
    setNotifyOn(isNotificationEnabled());
    setPermission(getNotificationPermission());
    client.validate().then(setOrg).catch(() => {});
    api.getUsage().then(setUsage).catch(() => {});
    api.getModelSource().then((r) => setModelSourceState(r.default_model_source === "byok" ? "byok" : "kiwi")).catch(() => {});
    Promise.all([
      client.listFleets().then((r) => r.fleets.length).catch(() => 0),
      client.listDaemons().then((d) => ({ total: d.length, online: d.filter((x) => x.online).length })).catch(() => ({ total: 0, online: 0 })),
      client.listJobs().then((r) => r.jobs.length).catch(() => 0),
      client.listModels().then((r) => r.models.length).catch(() => 0),
    ]).then(([fleets, daemons, jobs, models]) =>
      setStats({ fleets, daemons: daemons.total, daemonsOnline: daemons.online, jobs, models })
    );
  }, []);

  const handleToggleNotify = async () => {
    if (!notifyOn) {
      const granted = await requestNotificationPermission();
      setNotifyOn(granted);
      setPermission(getNotificationPermission());
    } else {
      setNotificationEnabled(false);
      setNotifyOn(false);
    }
  };

  const handleSetModelSource = async (source: "kiwi" | "byok") => {
    if (source === modelSource || modelSourceSaving) return;
    const previous = modelSource;
    setModelSourceState(source);
    setModelSourceSaving(true);
    try {
      await api.setModelSource(source);
    } catch {
      setModelSourceState(previous);
    } finally {
      setModelSourceSaving(false);
    }
  };

  const copyOrgId = () => {
    if (!org?.org_id) return;
    navigator.clipboard.writeText(org.org_id);
    setCopiedOrgId(true);
    setTimeout(() => setCopiedOrgId(false), 2000);
  };

  const isSuspended = org?.activation_state === "suspended";
  const planName = (org?.plan || "free").toUpperCase();

  return (
    <div className="max-w-6xl mx-auto flex flex-col gap-6 w-full font-sans text-stone-900">
      {/* ================= HERO HEADER WITH ANIMATED MASCOT ================= */}
      <div className="relative overflow-hidden p-6 rounded-3xl border border-sand-200 bg-gradient-to-r from-sand-100/90 via-white to-amber-50/70 backdrop-blur-xl flex flex-wrap items-center justify-between gap-4 shadow-2xs group">
        <div
          className="absolute inset-0 opacity-[0.035] pointer-events-none"
          style={{
            backgroundImage: `url("data:image/svg+xml,%3Csvg viewBox='0 0 200 200' xmlns='http://www.w3.org/2000/svg'%3E%3Cfilter id='n'%3E%3CfeTurbulence type='fractalNoise' baseFrequency='0.8' numOctaves='3'/%3E%3C/filter%3E%3Crect width='100%25' height='100%25' filter='url(%23n)'/%3E%3C/svg%3E")`,
          }}
        />
        <div className="absolute -top-12 -right-12 w-36 h-36 bg-amber-400/20 rounded-full blur-3xl group-hover:scale-110 transition-transform" />

        <div className="relative z-10 flex items-center gap-3.5">
          <div className="w-12 h-12 rounded-2xl bg-white border border-sand-200/90 shadow-2xs flex items-center justify-center shrink-0">
            <Logo variant="full-color" pose={org?.plan === "pro" ? "flying" : "vibing"} animated={true} className="w-8 h-8" />
          </div>
          <div>
            <h1 className="text-xl font-bold tracking-tight text-stone-900 flex items-center gap-2.5">
              <span>Plans &amp; Billing</span>
              <span className={`text-xs font-mono font-bold px-2 py-0.5 rounded-md border ${
                org?.plan === "pro"
                  ? "bg-amber-100 text-amber-900 border-amber-200"
                  : org?.plan === "enterprise"
                  ? "bg-purple-100 text-purple-900 border-purple-200"
                  : "bg-sand-100 text-stone-700 border-sand-200"
              }`}>
                {planName} TIER
              </span>
            </h1>
            <p className="text-xs text-stone-600 mt-0.5 max-w-2xl leading-relaxed">
              Manage subscription plans, monthly agent-minutes, concurrent runner limits, and billing invoices.
            </p>
          </div>
        </div>

        {org?.plan === "free" ? (
          <div className="relative z-10">
            <UpgradeButton variant="full" label="Upgrade to Pro" />
          </div>
        ) : (
          <div className="relative z-10 flex items-center gap-2">
            <a
              href="mailto:support@runkiwi.dev?subject=Billing%20Inquiry"
              className="px-3.5 py-2 rounded-xl bg-white hover:bg-sand-100 border border-sand-200 text-stone-700 font-semibold text-xs shadow-2xs flex items-center gap-1.5 transition-all cursor-pointer"
            >
              <span>Contact Billing</span>
            </a>
          </div>
        )}
      </div>

      {/* ================= TOP 4 KPI TILES (HYBRID FROSTED + LIGHT AURA + SPARKLINES) ================= */}
      <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-3.5">
        {/* KPI 1 */}
        <div className="relative overflow-hidden p-4 rounded-2xl bg-white/85 backdrop-blur-xl border border-sand-200 shadow-2xs hover:border-sand-300 hover:shadow-island transition-all group flex flex-col justify-between">
          <div
            className="absolute inset-0 opacity-[0.03] pointer-events-none"
            style={{
              backgroundImage: `url("data:image/svg+xml,%3Csvg viewBox='0 0 200 200' xmlns='http://www.w3.org/2000/svg'%3E%3Cfilter id='n'%3E%3CfeTurbulence type='fractalNoise' baseFrequency='0.8' numOctaves='3'/%3E%3C/filter%3E%3Crect width='100%25' height='100%25' filter='url(%23n)'/%3E%3C/svg%3E")`,
            }}
          />
          <div className="absolute -top-8 -right-8 w-20 h-20 bg-amber-400/20 rounded-full blur-2xl group-hover:scale-125 transition-transform" />

          <div className="relative z-10 flex items-center justify-between text-xs text-stone-600 font-medium">
            <span>Subscription Tier</span>
            <CreditCard className="w-4 h-4 text-stone-400" />
          </div>
          <div className="relative z-10 mt-2">
            <div className="text-2xl font-bold font-mono text-stone-900">{planName}</div>
            <div className="text-[10px] text-stone-400 font-mono mt-0.5">
              {org?.plan === "free" ? "Shared Managed Fleet" : "Dedicated Runners Enabled"}
            </div>
          </div>
          <div className="relative z-10 mt-2 flex items-end gap-1 h-3.5">
            {[40, 50, 60, 70, 75, 85, org?.plan === "pro" ? 100 : 40].map((h, i) => (
              <div key={i} className="flex-1 bg-amber-200 rounded-2xs" style={{ height: `${h}%` }} />
            ))}
          </div>
        </div>

        {/* KPI 2 */}
        <div className="relative overflow-hidden p-4 rounded-2xl bg-white/85 backdrop-blur-xl border border-sand-200 shadow-2xs hover:border-kiwi-300 hover:shadow-island transition-all group flex flex-col justify-between">
          <div
            className="absolute inset-0 opacity-[0.03] pointer-events-none"
            style={{
              backgroundImage: `url("data:image/svg+xml,%3Csvg viewBox='0 0 200 200' xmlns='http://www.w3.org/2000/svg'%3E%3Cfilter id='n'%3E%3CfeTurbulence type='fractalNoise' baseFrequency='0.8' numOctaves='3'/%3E%3C/filter%3E%3Crect width='100%25' height='100%25' filter='url(%23n)'/%3E%3C/svg%3E")`,
            }}
          />
          <div className="absolute -top-8 -right-8 w-20 h-20 bg-kiwi-400/20 rounded-full blur-2xl group-hover:scale-125 transition-transform" />

          <div className="relative z-10 flex items-center justify-between text-xs text-stone-600 font-medium">
            <span>Monthly Quota</span>
            <Gauge className="w-4 h-4 text-kiwi-600" />
          </div>
          <div className="relative z-10 mt-2">
            <div className="text-2xl font-bold font-mono text-stone-900">
              {usage?.agent_minutes_limit === 0 || org?.plan === "enterprise"
                ? "Unlimited"
                : usage?.agent_minutes_limit
                ? `${usage.agent_minutes_limit.toLocaleString()} min`
                : org?.plan === "free"
                ? "500 min"
                : "2,000 min"}
            </div>
            <div className="text-[10px] text-stone-400 font-mono mt-0.5">
              {org?.plan === "free" ? "Pooled workspace allowance" : "Per seat / pooled allowance"}
            </div>
          </div>
          <div className="relative z-10 mt-2 flex items-end gap-1 h-3.5">
            {[60, 70, 75, 80, 85, 90, 95].map((h, i) => (
              <div key={i} className="flex-1 bg-kiwi-200 rounded-2xs" style={{ height: `${h}%` }} />
            ))}
          </div>
        </div>

        {/* KPI 3 */}
        <div className="relative overflow-hidden p-4 rounded-2xl bg-white/85 backdrop-blur-xl border border-sand-200 shadow-2xs hover:border-indigo-300 hover:shadow-island transition-all group flex flex-col justify-between">
          <div
            className="absolute inset-0 opacity-[0.03] pointer-events-none"
            style={{
              backgroundImage: `url("data:image/svg+xml,%3Csvg viewBox='0 0 200 200' xmlns='http://www.w3.org/2000/svg'%3E%3Cfilter id='n'%3E%3CfeTurbulence type='fractalNoise' baseFrequency='0.8' numOctaves='3'/%3E%3C/filter%3E%3Crect width='100%25' height='100%25' filter='url(%23n)'/%3E%3C/svg%3E")`,
            }}
          />
          <div className="absolute -top-8 -right-8 w-20 h-20 bg-indigo-400/20 rounded-full blur-2xl group-hover:scale-125 transition-transform" />

          <div className="relative z-10 flex items-center justify-between text-xs text-stone-600 font-medium">
            <span>Concurrency Limit</span>
            <Layers className="w-4 h-4 text-indigo-500" />
          </div>
          <div className="relative z-10 mt-2">
            <div className="text-2xl font-bold font-mono text-stone-900">
              {usage?.max_concurrent_jobs != null
                ? `${usage.max_concurrent_jobs} ${usage.max_concurrent_jobs === 1 ? "Task" : "Tasks"}`
                : org?.plan === "free"
                ? "1 Task"
                : "20 Tasks"}
            </div>
            <div className="text-[10px] text-stone-400 font-mono mt-0.5">
              Parallel execution capacity
            </div>
          </div>
          <div className="relative z-10 mt-2 flex items-end gap-1 h-3.5">
            {[30, 45, 60, 75, 80, 90, 100].map((h, i) => (
              <div key={i} className="flex-1 bg-indigo-200 rounded-2xs" style={{ height: `${h}%` }} />
            ))}
          </div>
        </div>

        {/* KPI 4 */}
        <div className="relative overflow-hidden p-4 rounded-2xl bg-white/85 backdrop-blur-xl border border-sand-200 shadow-2xs hover:border-emerald-300 hover:shadow-island transition-all group flex flex-col justify-between">
          <div
            className="absolute inset-0 opacity-[0.03] pointer-events-none"
            style={{
              backgroundImage: `url("data:image/svg+xml,%3Csvg viewBox='0 0 200 200' xmlns='http://www.w3.org/2000/svg'%3E%3Cfilter id='n'%3E%3CfeTurbulence type='fractalNoise' baseFrequency='0.8' numOctaves='3'/%3E%3C/filter%3E%3Crect width='100%25' height='100%25' filter='url(%23n)'/%3E%3C/svg%3E")`,
            }}
          />
          <div className="absolute -top-8 -right-8 w-20 h-20 bg-emerald-400/20 rounded-full blur-2xl group-hover:scale-125 transition-transform" />

          <div className="relative z-10 flex items-center justify-between text-xs text-stone-600 font-medium">
            <span>Private Runners</span>
            <Server className="w-4 h-4 text-emerald-500" />
          </div>
          <div className="relative z-10 mt-2">
            <div className="text-2xl font-bold font-mono text-stone-900">
              {stats.daemonsOnline} <span className="text-sm font-normal text-stone-400">/ {stats.daemons}</span>
            </div>
            <div className="text-[10px] text-stone-400 font-mono mt-0.5">
              <Link href="/fleet" className="text-kiwi-700 font-semibold hover:underline">
                View Private Fleet &rarr;
              </Link>
            </div>
          </div>
          <div className="relative z-10 mt-2 flex items-end gap-1 h-3.5">
            {[40, 50, 60, 60, 80, 80, stats.daemonsOnline > 0 ? 100 : 50].map((h, i) => (
              <div key={i} className="flex-1 bg-emerald-200 rounded-2xs" style={{ height: `${h}%` }} />
            ))}
          </div>
        </div>
      </div>

      {/* ================= CURRENT PLAN USAGE CARD ================= */}
      <PlanUsage />

      {/* ================= PLAN COMPARISON & PRICING TIERS ================= */}
      <PlanComparison currentPlan={org?.plan} />

      {/* ================= ORGANIZATION BILLING PROFILE & INVOICES ================= */}
      <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
        {/* Organization Details Card */}
        <div className="bg-white border border-sand-200 rounded-2xl shadow-2xs p-5 sm:p-6 space-y-4">
          <div className="flex items-center justify-between">
            <h2 className="text-sm font-bold text-stone-900 uppercase tracking-wider flex items-center gap-2">
              <Building2 className="w-4 h-4 text-stone-600" />
              <span>Workspace Profile</span>
            </h2>

            <span className="text-[10px] font-mono font-bold text-emerald-700 bg-emerald-50 border border-emerald-200 px-2 py-0.5 rounded-full flex items-center gap-1">
              <ShieldCheck className="w-3 h-3 text-emerald-600" />
              <span>Verified Account</span>
            </span>
          </div>

          <div className="space-y-3 text-xs pt-1">
            <div className="flex items-center justify-between py-2 border-b border-sand-150">
              <span className="text-stone-500 font-medium">Workspace Name</span>
              <span className="font-bold text-stone-900">{org?.org_name || "—"}</span>
            </div>

            <div className="flex items-center justify-between py-2 border-b border-sand-150">
              <span className="text-stone-500 font-medium">Organization ID</span>
              <button
                onClick={copyOrgId}
                className="font-mono text-xs text-stone-700 hover:text-stone-950 flex items-center gap-1 bg-sand-50 hover:bg-sand-100 border border-sand-200 px-2 py-0.5 rounded-md transition-colors"
                title="Copy Org ID"
              >
                <span>{org?.org_id || "—"}</span>
                {copiedOrgId ? <Check className="w-3 h-3 text-emerald-600" /> : <Copy className="w-3 h-3 text-stone-400" />}
              </button>
            </div>

            <div className="flex items-center justify-between py-2 border-b border-sand-150">
              <span className="text-stone-500 font-medium">Primary Domain</span>
              <span className="font-mono text-stone-800">{org?.primary_domain || "runkiwi.dev"}</span>
            </div>

            <div className="flex items-center justify-between py-2">
              <span className="text-stone-500 font-medium">Team Management</span>
              <Link href="/team" className="text-kiwi-700 font-bold hover:underline">
                Manage Team Members &rarr;
              </Link>
            </div>
          </div>

          {isSuspended && (
            <div className="p-3.5 rounded-xl border border-rose-200 bg-rose-50 text-xs text-rose-800 space-y-1">
              <div className="font-bold flex items-center gap-1.5">
                <span>Organization Suspended</span>
              </div>
              <p className="text-rose-700">
                Task execution is paused. Please contact{" "}
                <a href="mailto:support@runkiwi.dev" className="underline font-semibold hover:text-rose-900">
                  support@runkiwi.dev
                </a>{" "}
                to reactivate compute limits.
              </p>
            </div>
          )}
        </div>

        {/* Invoices & Receipts Card */}
        <div className="bg-white border border-sand-200 rounded-2xl shadow-2xs p-5 sm:p-6 space-y-4 flex flex-col justify-between">
          <div className="space-y-3">
            <div className="flex items-center justify-between">
              <h2 className="text-sm font-bold text-stone-900 uppercase tracking-wider flex items-center gap-2">
                <Receipt className="w-4 h-4 text-stone-600" />
                <span>Invoices &amp; Receipts</span>
              </h2>

              <span className="text-[10px] font-mono text-stone-500 bg-sand-100 px-2 py-0.5 rounded-md border border-sand-200">
                Stripe Billing
              </span>
            </div>

            <p className="text-xs text-stone-500 leading-relaxed">
              Kiwi generates verifiable cryptographic receipts for every execution task and automated pull request. Invoices are dispatched monthly to your billing email.
            </p>

            <div className="p-3 rounded-xl bg-sand-50/70 border border-sand-200 text-xs space-y-2">
              <div className="flex items-center justify-between">
                <span className="text-stone-600">Payment Method:</span>
                <span className="font-mono text-stone-900 font-semibold">
                  {org?.plan === "free" ? "Free Community Tier" : "Invoiced (Stripe)"}
                </span>
              </div>
              <div className="flex items-center justify-between">
                <span className="text-stone-600">Billing Support:</span>
                <a
                  href="mailto:support@runkiwi.dev?subject=Invoice%20Request"
                  className="font-mono text-kiwi-700 hover:underline font-semibold"
                >
                  support@runkiwi.dev
                </a>
              </div>
            </div>
          </div>

          <div className="pt-2 border-t border-sand-150 flex items-center justify-between">
            <Link
              href="/records"
              className="text-xs font-semibold text-stone-600 hover:text-stone-900 flex items-center gap-1 transition-colors"
            >
              <span>View Audit Receipts Log</span>
              <ExternalLink className="w-3 h-3 text-stone-400" />
            </Link>

            <a
              href="mailto:support@runkiwi.dev?subject=Billing%20Support"
              className="px-3 py-1.5 rounded-xl bg-sand-100 hover:bg-sand-200 text-stone-800 font-semibold text-xs border border-sand-200 transition-colors"
            >
              Request Invoice PDF
            </a>
          </div>
        </div>
      </div>

      {/* ================= DEFAULT MODEL SOURCE SETTING ================= */}
      <div className="bg-white border border-sand-200 rounded-2xl shadow-2xs p-5 sm:p-6 flex flex-wrap items-center justify-between gap-4">
        <div className="space-y-0.5">
          <div className="flex items-center gap-2">
            <Coins className="w-4 h-4 text-amber-500" />
            <h3 className="text-sm font-bold text-stone-900">Default Model Source</h3>
          </div>
          <p className="text-xs text-stone-500 max-w-xl">
            Which model a task runs on when nothing else names one — a Slack trigger in an unbound channel, most
            commonly. Kiwi-funded needs no key of your own; your own key never touches Kiwi&apos;s allowance.
          </p>
        </div>

        <div className="flex items-center rounded-xl border border-sand-200 bg-sand-50 p-1 shrink-0">
          <button
            type="button"
            disabled={modelSourceSaving}
            onClick={() => handleSetModelSource("kiwi")}
            className={`px-3 py-1.5 rounded-lg text-xs font-bold transition-all disabled:opacity-40 disabled:cursor-not-allowed cursor-pointer ${
              modelSource === "kiwi" ? "bg-white text-stone-900 shadow-2xs border border-sand-200" : "text-stone-500 hover:text-stone-800"
            }`}
          >
            Kiwi-funded
          </button>
          <button
            type="button"
            disabled={modelSourceSaving}
            onClick={() => handleSetModelSource("byok")}
            className={`px-3 py-1.5 rounded-lg text-xs font-bold transition-all disabled:opacity-40 disabled:cursor-not-allowed cursor-pointer ${
              modelSource === "byok" ? "bg-white text-stone-900 shadow-2xs border border-sand-200" : "text-stone-500 hover:text-stone-800"
            }`}
          >
            Your own key
          </button>
        </div>
      </div>

      {/* ================= DESKTOP NOTIFICATIONS SETTING ================= */}
      <div className="bg-white border border-sand-200 rounded-2xl shadow-2xs p-5 sm:p-6 flex flex-wrap items-center justify-between gap-4">
        <div className="space-y-0.5">
          <div className="flex items-center gap-2">
            <Bell className="w-4 h-4 text-amber-500" />
            <h3 className="text-sm font-bold text-stone-900">Task Completion Notifications</h3>
          </div>
          <p className="text-xs text-stone-500 max-w-xl">
            Get notified when tasks and PR watchdogs finish running, even when Kiwi is in the background.
          </p>
        </div>

        <button
          type="button"
          role="switch"
          aria-checked={notifyOn}
          disabled={permission === "denied" || permission === "unsupported"}
          onClick={handleToggleNotify}
          className={`flex items-center gap-2 px-4 py-2 rounded-xl text-xs font-bold border transition-all shadow-2xs shrink-0 disabled:opacity-40 disabled:cursor-not-allowed cursor-pointer ${
            notifyOn
              ? "border-emerald-300 bg-emerald-50 text-emerald-800"
              : "border-sand-200 bg-white text-stone-600 hover:text-stone-900 hover:bg-sand-50"
          }`}
        >
          {notifyOn ? <Bell className="w-3.5 h-3.5 text-emerald-600 fill-current" /> : <BellOff className="w-3.5 h-3.5 text-stone-400" />}
          <span>{notifyOn ? "Notifications Active" : "Enable Notifications"}</span>
        </button>
      </div>
    </div>
  );
}
