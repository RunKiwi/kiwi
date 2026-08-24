"use client";

import { useEffect, useRef, useState } from "react";
import { useRouter } from "next/navigation";
import {
  api,
  type AdminStats,
  type AdminOrg,
  type AdminUserSearchRow,
  type AdminFleetStats,
  providerLabel,
} from "@/lib/api";
import {
  ShieldAlert,
  Building2,
  Users,
  Server,
  Zap,
  Search,
  RefreshCw,
  Plus,
} from "lucide-react";
import { KiwiMicroButtonLoader } from "@/components/KiwiLoaders";
import { LoadingState } from "@/components/LoadingState";

export default function AdminPage() {
  const router = useRouter();
  const [activeTab, setActiveTab] = useState<"orgs" | "users" | "fleet">("orgs");
  const [stats, setStats] = useState<AdminStats | null>(null);
  const [orgs, setOrgs] = useState<AdminOrg[]>([]);
  const [users, setUsers] = useState<AdminUserSearchRow[]>([]);
  const [userTotal, setUserTotal] = useState(0);
  const [fleet, setFleet] = useState<AdminFleetStats | null>(null);
  const [loading, setLoading] = useState(true);
  const [userSearch, setUserSearch] = useState("");
  const [orgSearch, setOrgSearch] = useState("");
  
  const searchSeq = useRef(0);
  const searchTimer = useRef<ReturnType<typeof setTimeout> | null>(null);

  // Modals state
  const [planModalOrg, setPlanModalOrg] = useState<AdminOrg | null>(null);
  const [grantModalOrg, setGrantModalOrg] = useState<AdminOrg | null>(null);
  const [grantAmount, setGrantAmount] = useState<number>(1000);
  const [submitting, setSubmitting] = useState(false);
  const [creatingOrg, setCreatingOrg] = useState(false);

  const fetchData = async () => {
    try {
      const u = await api.getUsage();
      if (!u.is_super_admin) {
        router.push("/");
        return;
      }
      const [s, o, usrRes, fltRes] = await Promise.all([
        api.getAdminStats().catch(() => null),
        api.listAdminOrgs().catch(() => []),
        api.searchAdminUsers(userSearch).catch(() => ({ users: [], total: 0 })),
        api.getAdminFleetMetrics().catch(() => null),
      ]);
      if (s) setStats(s);
      setOrgs(o);
      setUsers(usrRes.users);
      setUserTotal(usrRes.total);
      setFleet(fltRes);
    } catch {
      router.push("/");
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect
    fetchData();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [router]);

  const handleSearchUsers = (q: string) => {
    setUserSearch(q);
    if (searchTimer.current) clearTimeout(searchTimer.current);
    const seq = ++searchSeq.current;
    searchTimer.current = setTimeout(async () => {
      try {
        const res = await api.searchAdminUsers(q);
        if (seq !== searchSeq.current) return;
        setUsers(res.users);
        if (!q.trim()) {
          setUserTotal(res.total);
        }
      } catch (e) {
        console.error(e);
      }
    }, 300);
  };

  const handleUpdatePlan = async (plan: string) => {
    if (!planModalOrg) return;
    setSubmitting(true);
    try {
      await api.setOrgPlan(planModalOrg.id, plan);
      setOrgs(orgs.map((o) => (o.id === planModalOrg.id ? { ...o, plan } : o)));
      setPlanModalOrg(null);
    } catch (e) {
      alert("Error: " + (e instanceof Error ? e.message : String(e)));
    } finally {
      setSubmitting(false);
    }
  };

  const handleGrantMinutes = async () => {
    if (!grantModalOrg || grantAmount <= 0) return;
    setSubmitting(true);
    try {
      await api.grantOrgMinutes(grantModalOrg.id, grantAmount);
      setGrantModalOrg(null);
      await fetchData();
    } catch (e) {
      alert("Error: " + (e instanceof Error ? e.message : String(e)));
    } finally {
      setSubmitting(false);
    }
  };

  const handleCreateOrg = async () => {
    const name = prompt("Enter new organization name:");
    if (!name) return;
    setCreatingOrg(true);
    try {
      const org = await api.createAdminOrg(name);
      setOrgs((prev) => [org, ...prev]);
      if (stats) setStats({ ...stats, total_orgs: stats.total_orgs + 1 });
    } catch (e) {
      alert("Error: " + (e instanceof Error ? e.message : String(e)));
    } finally {
      setCreatingOrg(false);
    }
  };

  const handleToggleActivation = async (org: AdminOrg) => {
    const action = org.activation_state === "active" ? "suspend" : "activate";
    if (!confirm(`Are you sure you want to ${action} ${org.name}?`)) return;
    try {
      if (action === "activate") {
        await api.activateOrg(org.id);
        setOrgs(orgs.map((o) => (o.id === org.id ? { ...o, activation_state: "active" } : o)));
      } else {
        await api.suspendOrg(org.id);
        setOrgs(orgs.map((o) => (o.id === org.id ? { ...o, activation_state: "suspended" } : o)));
      }
    } catch (e) {
      alert("Error: " + (e instanceof Error ? e.message : String(e)));
    }
  };

  const filteredOrs = orgs.filter(
    (o) =>
      o.name.toLowerCase().includes(orgSearch.toLowerCase()) ||
      o.id.toLowerCase().includes(orgSearch.toLowerCase())
  );

  if (loading) {
    return (
      <div className="flex items-center justify-center min-h-[60vh]">
        <LoadingState state="connecting" label="Loading Super Admin Governance Console..." />
      </div>
    );
  }

  return (
    <div className="p-0 sm:p-2 md:p-4 max-w-7xl mx-auto space-y-6">
      {/* Header */}
      <div className="flex items-center justify-between border-b border-sand-200 pb-4">
        <div>
          <div className="flex items-center gap-2">
            <span className="px-2 py-0.5 rounded-full text-[10px] font-mono font-bold bg-rose-100 text-rose-800 border border-rose-200 flex items-center gap-1">
              <ShieldAlert className="w-3 h-3 text-rose-600" />
              KIWI STAFF CONSOLE
            </span>
          </div>
          <h1 className="text-xl font-bold text-stone-900 mt-1">Super Admin Governance</h1>
          <p className="text-xs text-stone-500">Cross-tenant organization management, global user directory, and runner fleet telemetry.</p>
        </div>

        <button
          onClick={fetchData}
          className="p-2 rounded-xl border border-sand-200 bg-white hover:bg-sand-50 text-stone-600 text-xs font-semibold flex items-center gap-1.5 shadow-2xs"
        >
          <RefreshCw className="w-3.5 h-3.5" />
          <span>Refresh</span>
        </button>
      </div>

      {/* KPI Cards */}
      <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4">
        <div className="p-4 rounded-2xl border border-sand-200 bg-white shadow-2xs">
          <div className="text-[11px] font-medium text-stone-500">Total Organizations</div>
          <div className="text-2xl font-bold text-stone-900 font-mono mt-1">{stats?.total_orgs ?? orgs.length}</div>
          <div className="text-[10px] font-mono text-emerald-600 mt-1">
            {stats?.signups_last_7_days != null ? `+${stats.signups_last_7_days} signups (7d)` : "Active"}
          </div>
        </div>

        <div className="p-4 rounded-2xl border border-sand-200 bg-white shadow-2xs">
          <div className="text-[11px] font-medium text-stone-500">Global Compute</div>
          <div className="text-2xl font-bold text-stone-900 font-mono mt-1">{(stats?.total_agent_minutes ?? 0).toFixed(1)}m</div>
          <div className="text-[10px] font-mono text-stone-400 mt-1">Active Runner Pool</div>
        </div>

        <div className="p-4 rounded-2xl border border-sand-200 bg-white shadow-2xs">
          <div className="text-[11px] font-medium text-stone-500">Global Users</div>
          <div className="text-2xl font-bold text-stone-900 font-mono mt-1">{userTotal || users.length}</div>
          <div className="text-[10px] font-mono text-stone-400 mt-1">Cross-tenant roster</div>
        </div>

        <div className="p-4 rounded-2xl border border-sand-200 bg-white shadow-2xs">
          <div className="text-[11px] font-medium text-stone-500">Fleet Containers</div>
          <div className="text-2xl font-bold text-stone-900 font-mono mt-1">
            {fleet?.active_containers ?? 0} / {fleet?.max_capacity ?? 0}
          </div>
          <div className="text-[10px] font-mono text-emerald-600 mt-1">gVisor runtime active</div>
        </div>
      </div>

      {/* Sub Tabs */}
      <div className="flex items-center gap-2 border-b border-sand-200 pb-2">
        <button
          onClick={() => setActiveTab("orgs")}
          className={`px-3 py-1.5 rounded-xl text-xs font-bold flex items-center gap-2 transition-all ${
            activeTab === "orgs"
              ? "bg-stone-900 text-white shadow-2xs"
              : "text-stone-600 hover:bg-sand-150"
          }`}
        >
          <Building2 className="w-3.5 h-3.5" />
          <span>Organizations ({orgs.length})</span>
        </button>

        <button
          onClick={() => setActiveTab("users")}
          className={`px-3 py-1.5 rounded-xl text-xs font-bold flex items-center gap-2 transition-all ${
            activeTab === "users"
              ? "bg-stone-900 text-white shadow-2xs"
              : "text-stone-600 hover:bg-sand-150"
          }`}
        >
          <Users className="w-3.5 h-3.5" />
          <span>Global Users ({userTotal || users.length})</span>
        </button>

        <button
          onClick={() => setActiveTab("fleet")}
          className={`px-3 py-1.5 rounded-xl text-xs font-bold flex items-center gap-2 transition-all ${
            activeTab === "fleet"
              ? "bg-stone-900 text-white shadow-2xs"
              : "text-stone-600 hover:bg-sand-150"
          }`}
        >
          <Server className="w-3.5 h-3.5" />
          <span>Fleet & Providers</span>
        </button>
      </div>

      {/* Tab 1: Organizations Table */}
      {activeTab === "orgs" && (
        <div className="space-y-3">
          <div className="flex items-center justify-between gap-3">
            <div className="relative flex-1 max-w-md">
              <Search className="w-3.5 h-3.5 text-stone-400 absolute left-3 top-3" />
              <input
                type="text"
                value={orgSearch}
                onChange={(e) => setOrgSearch(e.target.value)}
                placeholder="Search organizations by name or id..."
                className="w-full pl-8 pr-3 py-2 rounded-xl border border-sand-200 bg-white text-xs focus:ring-1 focus:ring-stone-900"
              />
            </div>

            <button
              onClick={handleCreateOrg}
              disabled={creatingOrg}
              className="px-3 py-2 rounded-xl bg-stone-900 hover:bg-stone-800 text-white text-xs font-bold flex items-center gap-1.5 shadow-2xs disabled:opacity-40"
            >
              {creatingOrg ? <KiwiMicroButtonLoader /> : <Plus className="w-3.5 h-3.5" />}
              <span>Create Organization</span>
            </button>
          </div>

          <div className="rounded-2xl border border-sand-200 bg-white overflow-hidden shadow-2xs">
            <table className="w-full text-left text-xs font-sans">
              <thead className="bg-sand-50 border-b border-sand-200 text-stone-500 font-mono text-[10px] uppercase">
                <tr>
                  <th className="p-3">Organization</th>
                  <th className="p-3">Plan Tier</th>
                  <th className="p-3">Status</th>
                  <th className="p-3">Compute Mins</th>
                  <th className="p-3 text-right">Actions</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-sand-100">
                {filteredOrs.map((org) => (
                  <tr key={org.id} className="hover:bg-sand-50/60 transition-colors">
                    <td className="p-3 font-semibold text-stone-900">
                      <div>{org.name}</div>
                      <div className="text-[10px] font-mono text-stone-400">{org.id}</div>
                    </td>
                    <td className="p-3">
                      <span className="px-2 py-0.5 rounded-full font-mono text-[10px] font-bold bg-sand-100 text-stone-800 border border-sand-200 uppercase">
                        {org.plan}
                      </span>
                    </td>
                    <td className="p-3">
                      <span
                        className={`px-2 py-0.5 rounded-full font-mono text-[10px] font-bold ${
                          org.activation_state === "active"
                            ? "bg-emerald-50 text-emerald-800 border border-emerald-200"
                            : "bg-rose-50 text-rose-800 border border-rose-200"
                        }`}
                      >
                        {org.activation_state}
                      </span>
                    </td>
                    <td className="p-3 font-mono text-stone-600">
                      {org.agent_minutes_used ? org.agent_minutes_used.toFixed(1) : "0.0"}m
                    </td>
                    <td className="p-3 text-right space-x-2">
                      <button
                        onClick={() => setPlanModalOrg(org)}
                        className="px-2.5 py-1 rounded-lg border border-sand-200 bg-white hover:bg-sand-100 text-[11px] font-semibold text-stone-700 shadow-2xs"
                      >
                        Change Plan
                      </button>
                      <button
                        onClick={() => setGrantModalOrg(org)}
                        className="px-2.5 py-1 rounded-lg border border-amber-200 bg-amber-50 hover:bg-amber-100 text-[11px] font-semibold text-amber-900 shadow-2xs"
                      >
                        + Grant Mins
                      </button>
                      <button
                        onClick={() => handleToggleActivation(org)}
                        className={`px-2.5 py-1 rounded-lg text-[11px] font-semibold shadow-2xs ${
                          org.activation_state === "active"
                            ? "border border-rose-200 bg-rose-50 text-rose-700 hover:bg-rose-100"
                            : "border border-emerald-200 bg-emerald-50 text-emerald-700 hover:bg-emerald-100"
                        }`}
                      >
                        {org.activation_state === "active" ? "Suspend" : "Activate"}
                      </button>
                      <button
                        onClick={() => router.push(`/admin/orgs/${org.id}`)}
                        className="px-2.5 py-1 rounded-lg bg-stone-900 hover:bg-stone-800 text-white text-[11px] font-semibold shadow-2xs"
                      >
                        Manage →
                      </button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>
      )}

      {/* Tab 2: Global Users Table */}
      {activeTab === "users" && (
        <div className="space-y-3">
          <div className="relative max-w-md">
            <Search className="w-3.5 h-3.5 text-stone-400 absolute left-3 top-3" />
            <input
              type="text"
              value={userSearch}
              onChange={(e) => handleSearchUsers(e.target.value)}
              placeholder="Search users by email or name..."
              className="w-full pl-8 pr-3 py-2 rounded-xl border border-sand-200 bg-white text-xs focus:ring-1 focus:ring-stone-900"
            />
          </div>

          <div className="rounded-2xl border border-sand-200 bg-white overflow-hidden shadow-2xs">
            <table className="w-full text-left text-xs font-sans">
              <thead className="bg-sand-50 border-b border-sand-200 text-stone-500 font-mono text-[10px] uppercase">
                <tr>
                  <th className="p-3">User</th>
                  <th className="p-3">Organization</th>
                  <th className="p-3">Role</th>
                  <th className="p-3">Auth Provider</th>
                  <th className="p-3">Joined</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-sand-100">
                {users.map((u) => (
                  <tr key={u.id} className="hover:bg-sand-50/60 transition-colors">
                    <td className="p-3">
                      <div className="font-semibold text-stone-900">{u.name || "No name"}</div>
                      <div className="text-[11px] text-stone-500 font-mono">{u.email}</div>
                    </td>
                    <td className="p-3 text-stone-700">
                      <div>{u.org_name || u.org_id}</div>
                    </td>
                    <td className="p-3 font-mono text-[10px]">
                      <span className="px-2 py-0.5 rounded-full bg-sand-100 border border-sand-200 text-stone-800 uppercase font-bold">
                        {u.role}
                      </span>
                    </td>
                    <td className="p-3 text-stone-600 font-mono text-[11px]">
                      {u.auth_provider || "email/pwd"}
                    </td>
                    <td className="p-3 text-stone-400 font-mono text-[11px]">
                      {new Date(u.created_at).toLocaleDateString()}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>
      )}

      {/* Tab 3: Fleet & AI Providers */}
      {activeTab === "fleet" && (
        <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
          <div className="p-5 rounded-2xl border border-sand-200 bg-white shadow-2xs space-y-3">
            <h3 className="text-sm font-bold text-stone-900 flex items-center gap-2">
              <Server className="w-4 h-4 text-stone-700" />
              Runner Daemon Host Telemetry
            </h3>
            <div className="space-y-2 text-xs font-mono">
              <div className="flex justify-between p-2 rounded-xl bg-sand-50">
                <span className="text-stone-500">Host Pool:</span>
                <span className="font-bold text-stone-900">{fleet?.host_pool || "—"}</span>
              </div>
              <div className="flex justify-between p-2 rounded-xl bg-sand-50">
                <span className="text-stone-500">Active Container Leases:</span>
                <span className="font-bold text-stone-900">{fleet ? `${fleet.active_containers} / ${fleet.max_capacity}` : "—"}</span>
              </div>
              <div className="flex justify-between p-2 rounded-xl bg-sand-50">
                <span className="text-stone-500">Avg Cold Start Latency:</span>
                <span className="font-bold text-stone-900">{fleet?.avg_cold_start_ms != null ? `${fleet.avg_cold_start_ms.toFixed(0)}ms` : "—"}</span>
              </div>
              <div className="flex justify-between p-2 rounded-xl bg-sand-50">
                <span className="text-stone-500">Security IMDS Blocks:</span>
                <span className="font-bold text-emerald-600">{fleet?.imds_blocked_count != null ? `${fleet.imds_blocked_count} receipts` : "—"}</span>
              </div>
            </div>
          </div>

          <div className="p-5 rounded-2xl border border-sand-200 bg-white shadow-2xs space-y-3">
            <h3 className="text-sm font-bold text-stone-900 flex items-center gap-2">
              <Zap className="w-4 h-4 text-amber-500" />
              AI Provider Volume Matrix
            </h3>
            <div className="space-y-2 text-xs font-mono">
              {(() => {
                const rows = stats?.provider_usage ?? [];
                const totalTokens = rows.reduce((n, r) => n + r.tokens_in + r.tokens_out, 0);
                if (rows.length === 0) {
                  return <p className="text-stone-400 py-2">No provider usage recorded yet.</p>;
                }
                return rows.map((r) => {
                  const tokens = r.tokens_in + r.tokens_out;
                  const pct = totalTokens > 0 ? ((tokens / totalTokens) * 100).toFixed(1) : "0.0";
                  return (
                    <div key={r.provider} className="flex justify-between p-2 rounded-xl bg-sand-50">
                      <span className="text-stone-500">{providerLabel(r.provider)}:</span>
                      <span className="font-bold text-stone-900">{pct}% token volume</span>
                    </div>
                  );
                });
              })()}
            </div>
          </div>
        </div>
      )}

      {/* Plan Modal */}
      {planModalOrg && (
        <div className="fixed inset-0 bg-black/40 backdrop-blur-xs flex items-center justify-center p-4 z-50">
          <div className="bg-white rounded-2xl border border-sand-200 p-5 max-w-sm w-full space-y-4 shadow-xl">
            <h3 className="text-sm font-bold text-stone-900">Change Plan for {planModalOrg.name}</h3>
            <div className="space-y-1.5">
              {["free", "pro", "enterprise"].map((p) => (
                <button
                  key={p}
                  onClick={() => handleUpdatePlan(p)}
                  disabled={submitting}
                  className="w-full p-2.5 rounded-xl border border-sand-200 hover:border-stone-900 text-xs font-bold capitalize flex items-center justify-between text-stone-800 hover:bg-sand-50"
                >
                  <span>{p} Tier</span>
                  <span className="text-[10px] font-mono text-stone-400">Select</span>
                </button>
              ))}
            </div>
            <button
              onClick={() => setPlanModalOrg(null)}
              className="w-full py-1.5 rounded-xl bg-sand-100 text-stone-600 text-xs font-semibold"
            >
              Cancel
            </button>
          </div>
        </div>
      )}

      {/* Grant Minutes Modal */}
      {grantModalOrg && (
        <div className="fixed inset-0 bg-black/40 backdrop-blur-xs flex items-center justify-center p-4 z-50">
          <div className="bg-white rounded-2xl border border-sand-200 p-5 max-w-sm w-full space-y-4 shadow-xl">
            <h3 className="text-sm font-bold text-stone-900">Grant Compute Minutes to {grantModalOrg.name}</h3>
            <div className="grid grid-cols-3 gap-2">
              {[500, 2500, 10000].map((amt) => (
                <button
                  key={amt}
                  onClick={() => setGrantAmount(amt)}
                  className={`p-2 rounded-xl text-xs font-mono font-bold border transition-all ${
                    grantAmount === amt
                      ? "border-stone-900 bg-sand-200 text-stone-900"
                      : "border-sand-200 text-stone-600 hover:bg-sand-50"
                  }`}
                >
                  +{amt}m
                </button>
              ))}
            </div>
            <div className="flex gap-2">
              <button
                onClick={() => setGrantModalOrg(null)}
                className="flex-1 py-2 rounded-xl bg-sand-100 text-stone-600 text-xs font-semibold"
              >
                Cancel
              </button>
              <button
                onClick={handleGrantMinutes}
                disabled={submitting}
                className="flex-1 py-2 rounded-xl bg-stone-900 text-white text-xs font-bold flex items-center justify-center gap-1"
              >
                {submitting ? <KiwiMicroButtonLoader /> : null}
                <span>Grant Minutes</span>
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
