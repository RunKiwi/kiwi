"use client";

import { useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import { client, type AdminStats, type AdminOrg, type UsageResponse } from "@/lib/api";
import { Activity, Users, Database, ShieldAlert, Check, Plus, Loader2 } from "lucide-react";

export default function AdminPage() {
  const router = useRouter();
  const [usage, setUsage] = useState<UsageResponse | null>(null);
  const [stats, setStats] = useState<AdminStats | null>(null);
  const [orgs, setOrgs] = useState<AdminOrg[]>([]);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState<string | null>(null); // orgId + action

  useEffect(() => {
    client.getUsage().then(u => {
      if (!u.is_super_admin) {
        router.push("/");
        return;
      }
      setUsage(u);
      return Promise.all([
        client.getAdminStats(),
        client.listAdminOrgs(),
      ]).then(([s, o]) => {
        setStats(s);
        setOrgs(o);
        setLoading(false);
      });
    }).catch(() => {
      router.push("/");
    });
  }, [router]);

  const grantMinutes = async (orgId: string) => {
    const minStr = prompt("Grant agent minutes to this org:");
    if (!minStr) return;
    const mins = parseFloat(minStr);
    if (isNaN(mins) || mins <= 0) return;
    
    setBusy(`${orgId}-grant`);
    try {
      await client.grantOrgMinutes(orgId, mins);
      alert(`Granted ${mins} minutes`);
    } catch (e: any) {
      alert("Error: " + e.message);
    } finally {
      setBusy(null);
    }
  };

  const updatePlan = async (orgId: string, currentPlan: string) => {
    const plan = prompt("Enter new plan (free, pro, enterprise):", currentPlan);
    if (!plan || plan === currentPlan) return;
    
    setBusy(`${orgId}-plan`);
    try {
      await client.setOrgPlan(orgId, plan);
      setOrgs(orgs.map(o => o.id === orgId ? { ...o, plan } : o));
    } catch (e: any) {
      alert("Error: " + e.message);
    } finally {
      setBusy(null);
    }
  };

  const toggleActivation = async (orgId: string, currentState: string) => {
    const action = currentState === "active" ? "suspend" : "activate";
    if (!confirm(`Are you sure you want to ${action} this org?`)) return;
    
    setBusy(`${orgId}-${action}`);
    try {
      if (action === "activate") {
        await client.activateOrg(orgId);
        setOrgs(orgs.map(o => o.id === orgId ? { ...o, activation_state: "active" } : o));
      } else {
        await client.suspendOrg(orgId);
        setOrgs(orgs.map(o => o.id === orgId ? { ...o, activation_state: "suspended" } : o));
      }
    } catch (e: any) {
      alert("Error: " + e.message);
    } finally {
      setBusy(null);
    }
  };

  if (loading) {
    return (
      <div className="flex h-full items-center justify-center">
        <Loader2 className="w-8 h-8 animate-spin text-zinc-500" />
      </div>
    );
  }

  return (
    <div className="p-8 max-w-7xl mx-auto h-full flex flex-col text-white">
      <div className="mb-8">
        <h1 className="text-3xl font-light tracking-tight mb-2 flex items-center gap-2">
          <ShieldAlert className="w-8 h-8 text-red-500" />
          Super Admin
        </h1>
        <p className="text-zinc-400">Global system monitoring and organization management.</p>
      </div>

      {stats && (
        <div className="grid grid-cols-1 md:grid-cols-4 gap-4 mb-8">
          <div className="glass-panel p-5 border border-white/10 rounded-xl">
            <div className="text-xs font-bold text-zinc-500 uppercase tracking-widest mb-1">Total Orgs</div>
            <div className="text-2xl font-light">{stats.total_orgs}</div>
            <div className="text-xs text-zinc-400 mt-2">
              <span className="text-green-400">+{stats.signups_last_7_days}</span> last 7d, <span className="text-green-400">+{stats.signups_last_30_days}</span> last 30d
            </div>
          </div>
          <div className="glass-panel p-5 border border-white/10 rounded-xl">
            <div className="text-xs font-bold text-zinc-500 uppercase tracking-widest mb-1">Plans</div>
            {Object.entries(stats.orgs_by_plan).map(([plan, count]) => (
              <div key={plan} className="flex justify-between text-sm mt-1">
                <span className="text-zinc-300 capitalize">{plan}</span>
                <span className="font-medium">{count}</span>
              </div>
            ))}
          </div>
          <div className="glass-panel p-5 border border-white/10 rounded-xl">
            <div className="text-xs font-bold text-zinc-500 uppercase tracking-widest mb-1">Compute</div>
            <div className="text-2xl font-light">{stats.total_agent_minutes.toFixed(0)} <span className="text-sm text-zinc-400">min</span></div>
            <div className="text-xs text-zinc-400 mt-2">Global agent minutes consumed</div>
          </div>
          <div className="glass-panel p-5 border border-white/10 rounded-xl">
            <div className="text-xs font-bold text-zinc-500 uppercase tracking-widest mb-1">Task Queue</div>
            {Object.entries(stats.tasks_by_status).map(([status, count]) => (
              <div key={status} className="flex justify-between text-sm mt-1">
                <span className="text-zinc-300">{status}</span>
                <span className="font-medium">{count}</span>
              </div>
            ))}
          </div>
        </div>
      )}

      <h2 className="text-xs font-bold text-zinc-500 uppercase tracking-widest mb-3">Organizations</h2>
      <div className="glass-panel border border-white/10 rounded-xl overflow-hidden">
        <table className="w-full text-sm text-left">
          <thead className="bg-white/5 border-b border-white/10 text-xs font-medium text-zinc-400">
            <tr>
              <th className="px-4 py-3">Org</th>
              <th className="px-4 py-3">Plan</th>
              <th className="px-4 py-3">Status</th>
              <th className="px-4 py-3">Created</th>
              <th className="px-4 py-3 text-right">Actions</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-white/5">
            {orgs.map((org) => (
              <tr key={org.id} className="hover:bg-white/[0.02] transition-colors">
                <td className="px-4 py-3">
                  <div className="font-medium">{org.name}</div>
                  <div className="text-xs text-zinc-500 font-mono">{org.id}</div>
                </td>
                <td className="px-4 py-3 capitalize">{org.plan}</td>
                <td className="px-4 py-3">
                  <span className={`inline-flex items-center px-2 py-0.5 rounded text-xs font-medium ${
                    org.activation_state === 'active' ? 'bg-green-500/10 text-green-400' :
                    org.activation_state === 'suspended' ? 'bg-red-500/10 text-red-400' :
                    'bg-zinc-500/10 text-zinc-400'
                  }`}>
                    {org.activation_state}
                  </span>
                </td>
                <td className="px-4 py-3 text-zinc-400">
                  {new Date(org.created_at).toLocaleDateString()}
                </td>
                <td className="px-4 py-3 text-right space-x-2">
                  <button
                    onClick={() => updatePlan(org.id, org.plan)}
                    disabled={!!busy}
                    className="text-xs bg-white/5 hover:bg-white/10 border border-white/10 rounded px-2 py-1 transition-colors"
                  >
                    Change Plan
                  </button>
                  <button
                    onClick={() => grantMinutes(org.id)}
                    disabled={!!busy}
                    className="text-xs bg-white/5 hover:bg-white/10 border border-white/10 rounded px-2 py-1 transition-colors"
                  >
                    Grant Mins
                  </button>
                  <button
                    onClick={() => toggleActivation(org.id, org.activation_state)}
                    disabled={!!busy}
                    className={`text-xs border rounded px-2 py-1 transition-colors ${
                      org.activation_state === 'active' 
                        ? 'bg-red-500/10 hover:bg-red-500/20 border-red-500/20 text-red-400'
                        : 'bg-green-500/10 hover:bg-green-500/20 border-green-500/20 text-green-400'
                    }`}
                  >
                    {busy === `${org.id}-activate` || busy === `${org.id}-suspend` ? <Loader2 className="w-3 h-3 animate-spin inline" /> : org.activation_state === 'active' ? 'Suspend' : 'Activate'}
                  </button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );
}
