"use client";

import { useEffect, useState, use } from "react";
import { useRouter } from "next/navigation";
import { client, type AdminOrg, type AdminUser, type AdminAuditLog, type AdminProviderConfig } from "@/lib/api";
import { Loader2, ArrowLeft, Users, Activity, Settings, Database, Plus } from "lucide-react";
import { LoadingState } from "@/components/LoadingState";
import Link from "next/link";

export default function AdminOrgPage({ params }: { params: Promise<{ orgId: string }> }) {
  const router = useRouter();
  const unwrappedParams = use(params);
  const orgId = unwrappedParams.orgId;

  const [org, setOrg] = useState<AdminOrg | null>(null);
  const [users, setUsers] = useState<AdminUser[]>([]);
  const [auditLogs, setAuditLogs] = useState<AdminAuditLog[]>([]);
  const [loading, setLoading] = useState(true);
  const [activeTab, setActiveTab] = useState<"users" | "audit" | "provider">("users");
  const [busy, setBusy] = useState<string | null>(null);

  // New user form
  const [newEmail, setNewEmail] = useState("");
  const [newName, setNewName] = useState("");
  const [newRole, setNewRole] = useState("member");
  
  // Provider form
  const [provName, setProvName] = useState("");
  const [provActor, setProvActor] = useState("");
  const [provCritic, setProvCritic] = useState("");
  const [provKey, setProvKey] = useState("");

  useEffect(() => {
    client.getUsage().then(u => {
      if (!u.is_super_admin) {
        router.push("/");
        return;
      }
      return Promise.all([
        client.listAdminOrgs().then(orgs => orgs.find(o => o.id === orgId) || null),
        client.listAdminOrgUsers(orgId),
        client.getAdminOrgAuditLogs(orgId),
        client.getAdminOrgProviderConfig(orgId).catch(() => null)
      ]).then(([o, usrs, logs, prov]) => {
        if (!o) {
          router.push("/admin");
          return;
        }
        setOrg(o);
        setUsers(usrs);
        setAuditLogs(logs);

        if (prov) {
          setProvName(prov.provider_name);
          setProvActor(prov.actor_model || "");
          setProvCritic(prov.critic_model || "");
        } else {
          setProvName("anthropic");
        }
        
        setLoading(false);
      });
    }).catch(() => {
      router.push("/");
    });
  }, [router, orgId]);

  const handleCreateUser = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!newEmail || !newName) return;
    
    setBusy("create_user");
    try {
      const u = await client.createAdminOrgUser(orgId, newEmail, newName, newRole);
      setUsers([u, ...users]);
      setNewEmail("");
      setNewName("");
      setNewRole("member");
      alert("User created successfully!");
    } catch (err) {
      alert("Error: " + (err instanceof Error ? err.message : String(err)));
    } finally {
      setBusy(null);
    }
  };

  const handleSaveProvider = async (e: React.FormEvent) => {
    e.preventDefault();
    
    setBusy("save_provider");
    try {
      const update: Partial<AdminProviderConfig> = {
        provider_name: provName,
        actor_model: provActor,
        critic_model: provCritic,
      };
      if (provKey) {
        update.api_key = provKey;
      }
      
      await client.setAdminOrgProviderConfig(orgId, update);
      setProvKey(""); // clear key field after save
      alert("Provider configuration updated successfully!");
    } catch (err) {
      alert("Error: " + (err instanceof Error ? err.message : String(err)));
    } finally {
      setBusy(null);
    }
  };

  if (loading || !org) {
    return <LoadingState label="Loading org details…" className="h-full" />;
  }

  return (
    <div className="p-8 max-w-7xl mx-auto h-full flex flex-col text-white">
      <div className="mb-8">
        <Link href="/admin" className="text-sm text-zinc-400 hover:text-white flex items-center gap-1 mb-4 w-fit">
          <ArrowLeft className="w-4 h-4" /> Back to Admin
        </Link>
        <div className="flex items-center justify-between">
          <div>
            <h1 className="text-3xl font-light tracking-tight mb-2 flex items-center gap-2">
              {org.name}
            </h1>
            <p className="text-zinc-400 font-mono text-sm">ID: {org.id} &bull; Plan: {org.plan} &bull; Status: {org.activation_state}</p>
          </div>
        </div>
      </div>

      <div className="flex gap-4 mb-6 border-b border-white/10 pb-4">
        <button 
          onClick={() => setActiveTab("users")}
          className={`flex items-center gap-2 px-4 py-2 rounded-lg text-sm font-medium transition-colors ${activeTab === 'users' ? 'bg-white/10 text-white' : 'text-zinc-400 hover:text-white hover:bg-white/5'}`}
        >
          <Users className="w-4 h-4" /> Users
        </button>
        <button 
          onClick={() => setActiveTab("provider")}
          className={`flex items-center gap-2 px-4 py-2 rounded-lg text-sm font-medium transition-colors ${activeTab === 'provider' ? 'bg-white/10 text-white' : 'text-zinc-400 hover:text-white hover:bg-white/5'}`}
        >
          <Database className="w-4 h-4" /> Provider Config
        </button>
        <button 
          onClick={() => setActiveTab("audit")}
          className={`flex items-center gap-2 px-4 py-2 rounded-lg text-sm font-medium transition-colors ${activeTab === 'audit' ? 'bg-white/10 text-white' : 'text-zinc-400 hover:text-white hover:bg-white/5'}`}
        >
          <Activity className="w-4 h-4" /> Audit Logs
        </button>
      </div>

      <div className="flex-1 overflow-auto">
        {activeTab === 'users' && (
          <div className="space-y-6">
            <div className="glass-panel p-6 border border-white/10 rounded-xl">
              <h2 className="text-lg font-medium mb-4 flex items-center gap-2">
                <Plus className="w-5 h-5" /> Add User
              </h2>
              <form onSubmit={handleCreateUser} className="flex gap-4 items-end">
                <div className="flex-1">
                  <label className="block text-xs text-zinc-400 mb-1">Name</label>
                  <input type="text" value={newName} onChange={e => setNewName(e.target.value)} required className="w-full bg-white/5 border border-white/10 rounded-lg px-3 py-2 text-sm focus:outline-none focus:border-indigo-500" placeholder="John Doe" />
                </div>
                <div className="flex-1">
                  <label className="block text-xs text-zinc-400 mb-1">Email</label>
                  <input type="email" value={newEmail} onChange={e => setNewEmail(e.target.value)} required className="w-full bg-white/5 border border-white/10 rounded-lg px-3 py-2 text-sm focus:outline-none focus:border-indigo-500" placeholder="john@example.com" />
                </div>
                <div className="w-32">
                  <label className="block text-xs text-zinc-400 mb-1">Role</label>
                  <select value={newRole} onChange={e => setNewRole(e.target.value)} className="w-full bg-[#1c1c1c] border border-white/10 rounded-lg px-3 py-2 text-sm focus:outline-none focus:border-indigo-500">
                    <option value="member">Member</option>
                    <option value="admin">Admin</option>
                  </select>
                </div>
                <button type="submit" disabled={!!busy} className="bg-indigo-600 hover:bg-indigo-700 text-white px-4 py-2 rounded-lg text-sm font-medium transition-colors h-[38px] flex items-center justify-center min-w-[100px]">
                  {busy === 'create_user' ? <Loader2 className="w-4 h-4 animate-spin" /> : 'Create User'}
                </button>
              </form>
            </div>

            <div className="glass-panel border border-white/10 rounded-xl overflow-hidden">
              <table className="w-full text-sm text-left">
                <thead className="bg-white/5 border-b border-white/10 text-xs font-medium text-zinc-400">
                  <tr>
                    <th className="px-4 py-3">Name</th>
                    <th className="px-4 py-3">Email</th>
                    <th className="px-4 py-3">Role</th>
                    <th className="px-4 py-3">Joined</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-white/5">
                  {users.map(user => (
                    <tr key={user.id} className="hover:bg-white/[0.02] transition-colors">
                      <td className="px-4 py-3 font-medium">{user.name}</td>
                      <td className="px-4 py-3 text-zinc-300">{user.email}</td>
                      <td className="px-4 py-3">
                        <span className={`inline-flex px-2 py-0.5 rounded text-xs ${user.role === 'admin' ? 'bg-indigo-500/10 text-indigo-400' : 'bg-white/10 text-zinc-300'}`}>
                          {user.role}
                        </span>
                      </td>
                      <td className="px-4 py-3 text-zinc-400">{new Date(user.created_at).toLocaleDateString()}</td>
                    </tr>
                  ))}
                  {users.length === 0 && (
                    <tr>
                      <td colSpan={4} className="px-4 py-8 text-center text-zinc-500">No users found.</td>
                    </tr>
                  )}
                </tbody>
              </table>
            </div>
          </div>
        )}

        {activeTab === 'provider' && (
          <div className="glass-panel p-6 border border-white/10 rounded-xl max-w-2xl">
            <h2 className="text-lg font-medium mb-4 flex items-center gap-2">
              <Settings className="w-5 h-5" /> LLM Provider Override
            </h2>
            <p className="text-sm text-zinc-400 mb-6">
              Configure custom LLM provider settings for this organization. This will override global defaults.
            </p>
            <form onSubmit={handleSaveProvider} className="space-y-4">
              <div>
                <label className="block text-sm font-medium text-zinc-300 mb-1">Provider Name</label>
                <select value={provName} onChange={e => setProvName(e.target.value)} className="w-full bg-[#1c1c1c] border border-white/10 rounded-lg px-3 py-2 text-sm focus:outline-none focus:border-indigo-500">
                  <option value="anthropic">Anthropic</option>
                  <option value="openai">OpenAI</option>
                  <option value="gemini">Gemini</option>
                </select>
              </div>
              <div>
                <label className="block text-sm font-medium text-zinc-300 mb-1">API Key</label>
                <input type="password" value={provKey} onChange={e => setProvKey(e.target.value)} className="w-full bg-white/5 border border-white/10 rounded-lg px-3 py-2 text-sm focus:outline-none focus:border-indigo-500" placeholder="Leave blank to keep existing key" />
                <p className="text-xs text-zinc-500 mt-1">Stored securely. Only enter a new key to update.</p>
              </div>
              <div className="grid grid-cols-2 gap-4">
                <div>
                  <label className="block text-sm font-medium text-zinc-300 mb-1">Actor Model</label>
                  <input type="text" value={provActor} onChange={e => setProvActor(e.target.value)} className="w-full bg-white/5 border border-white/10 rounded-lg px-3 py-2 text-sm focus:outline-none focus:border-indigo-500" placeholder="e.g. claude-3-5-sonnet-20241022" />
                </div>
                <div>
                  <label className="block text-sm font-medium text-zinc-300 mb-1">Critic Model</label>
                  <input type="text" value={provCritic} onChange={e => setProvCritic(e.target.value)} className="w-full bg-white/5 border border-white/10 rounded-lg px-3 py-2 text-sm focus:outline-none focus:border-indigo-500" placeholder="e.g. claude-3-5-haiku-20241022" />
                </div>
              </div>
              <div className="pt-4 border-t border-white/10 mt-6">
                <button type="submit" disabled={!!busy} className="bg-indigo-600 hover:bg-indigo-700 text-white px-6 py-2 rounded-lg text-sm font-medium transition-colors flex items-center justify-center min-w-[120px]">
                  {busy === 'save_provider' ? <Loader2 className="w-4 h-4 animate-spin" /> : 'Save Config'}
                </button>
              </div>
            </form>
          </div>
        )}

        {activeTab === 'audit' && (
          <div className="glass-panel border border-white/10 rounded-xl overflow-hidden">
            <table className="w-full text-sm text-left">
              <thead className="bg-white/5 border-b border-white/10 text-xs font-medium text-zinc-400">
                <tr>
                  <th className="px-4 py-3">Timestamp</th>
                  <th className="px-4 py-3">User</th>
                  <th className="px-4 py-3">Action</th>
                  <th className="px-4 py-3">Resource</th>
                  <th className="px-4 py-3">Details</th>
                  <th className="px-4 py-3">IP Address</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-white/5">
                {auditLogs.map(log => (
                  <tr key={log.id} className="hover:bg-white/[0.02] transition-colors">
                    <td className="px-4 py-3 text-zinc-400 whitespace-nowrap">{new Date(log.created_at).toLocaleString()}</td>
                    <td className="px-4 py-3">
                      <div className="font-medium">{log.user_email || 'System'}</div>
                      {log.user_id && <div className="text-[10px] text-zinc-500 font-mono">{log.user_id}</div>}
                    </td>
                    <td className="px-4 py-3">
                      <span className="inline-flex px-2 py-0.5 rounded text-xs bg-white/10 text-zinc-300 font-mono">
                        {log.action}
                      </span>
                    </td>
                    <td className="px-4 py-3">
                      <div className="font-mono text-zinc-300">{log.resource}</div>
                      <div className="text-[10px] text-zinc-500 font-mono truncate max-w-[120px]">{log.resource_id}</div>
                    </td>
                    <td className="px-4 py-3 text-zinc-300 truncate max-w-md">{log.details}</td>
                    <td className="px-4 py-3 text-zinc-500 font-mono text-xs">{log.client_ip || '-'}</td>
                  </tr>
                ))}
                {auditLogs.length === 0 && (
                  <tr>
                    <td colSpan={6} className="px-4 py-8 text-center text-zinc-500">No audit logs found.</td>
                  </tr>
                )}
              </tbody>
            </table>
          </div>
        )}
      </div>
    </div>
  );
}
