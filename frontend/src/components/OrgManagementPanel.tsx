"use client";

import { useEffect, useState, Fragment } from "react";
import { client, type AdminOrg, type AdminUser, type AdminAuditLog, type AdminProviderConfig, type AdminOrgModelUsage, type AdminAPIKey, formatTokens, providerLabel } from "@/lib/api";
import { Loader2, Users, Activity, Settings, Database, Plus, BarChart3, KeyRound, Pencil, Check, X } from "lucide-react";

export function OrgManagementPanel({ org, onOrgUpdate }: { org: AdminOrg; onOrgUpdate: (org: AdminOrg) => void }) {
  const [users, setUsers] = useState<AdminUser[]>([]);
  const [auditLogs, setAuditLogs] = useState<AdminAuditLog[]>([]);
  const [modelUsage, setModelUsage] = useState<AdminOrgModelUsage | null>(null);
  const [loading, setLoading] = useState(true);
  const [activeTab, setActiveTab] = useState<"users" | "usage" | "audit" | "provider">("users");
  const [busy, setBusy] = useState<string | null>(null);

  // New user form
  const [newEmail, setNewEmail] = useState("");
  const [newName, setNewName] = useState("");
  const [newRole, setNewRole] = useState("member");

  // API keys, expanded per user
  const [expandedUserId, setExpandedUserId] = useState<string | null>(null);
  const [keysByUser, setKeysByUser] = useState<Record<string, AdminAPIKey[]>>({});
  const [keysLoading, setKeysLoading] = useState<string | null>(null);
  const [newKey, setNewKey] = useState<{ userId: string; plaintext: string } | null>(null);
  const [copied, setCopied] = useState(false);

  // Provider form
  const [provName, setProvName] = useState("");
  const [provActor, setProvActor] = useState("");
  const [provCritic, setProvCritic] = useState("");
  const [provKey, setProvKey] = useState("");

  // Rename
  const [renaming, setRenaming] = useState(false);
  const [nameDraft, setNameDraft] = useState(org.name);

  useEffect(() => {
    Promise.all([
      client.listAdminOrgUsers(org.id),
      client.getAdminOrgAuditLogs(org.id),
      client.getAdminOrgProviderConfig(org.id).catch(() => null),
      client.getAdminOrgModelUsage(org.id).catch(() => null),
    ]).then(([usrs, logs, prov, usage]) => {
      setUsers(usrs);
      setAuditLogs(logs);
      setModelUsage(usage);

      if (prov) {
        setProvName(prov.provider_name);
        setProvActor(prov.actor_model || "");
        setProvCritic(prov.critic_model || "");
      } else {
        setProvName("anthropic");
      }

      setLoading(false);
    });
  }, [org.id]);

  useEffect(() => {
    setNameDraft(org.name);
  }, [org.name]);

  const handleCreateUser = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!newEmail || !newName) return;

    setBusy("create_user");
    try {
      const u = await client.createAdminOrgUser(org.id, newEmail, newName, newRole);
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

  const toggleKeys = (userId: string) => {
    setNewKey(null);
    if (expandedUserId === userId) {
      setExpandedUserId(null);
      return;
    }
    setExpandedUserId(userId);
    if (!keysByUser[userId]) {
      setKeysLoading(userId);
      client.listAdminUserAPIKeys(org.id, userId)
        .then(keys => setKeysByUser(prev => ({ ...prev, [userId]: keys })))
        .catch(() => setKeysByUser(prev => ({ ...prev, [userId]: [] })))
        .finally(() => setKeysLoading(null));
    }
  };

  const handleGenerateKey = async (userId: string) => {
    const label = prompt("Label for this key (e.g. \"cli\"):", "cli");
    if (label === null) return;

    setBusy(`genkey-${userId}`);
    try {
      const created = await client.createAdminUserAPIKey(org.id, userId, label || "default");
      setNewKey({ userId, plaintext: created.key });
      setCopied(false);
      setKeysByUser(prev => ({
        ...prev,
        [userId]: [
          { id: created.key_id, user_id: userId, label: created.label, created_at: created.created_at, expires_at: created.expires_at ?? undefined },
          ...(prev[userId] ?? []),
        ],
      }));
    } catch (err) {
      alert("Error: " + (err instanceof Error ? err.message : String(err)));
    } finally {
      setBusy(null);
    }
  };

  const handleRevokeKey = async (userId: string, keyId: string) => {
    if (!confirm("Revoke this key? Anything using it will stop working immediately.")) return;

    setBusy(`revoke-${keyId}`);
    try {
      await client.revokeAdminUserAPIKey(org.id, userId, keyId);
      setKeysByUser(prev => ({ ...prev, [userId]: (prev[userId] ?? []).filter(k => k.id !== keyId) }));
    } catch (err) {
      alert("Error: " + (err instanceof Error ? err.message : String(err)));
    } finally {
      setBusy(null);
    }
  };

  const copyKey = (text: string) => {
    navigator.clipboard.writeText(text).then(() => {
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    });
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

      await client.setAdminOrgProviderConfig(org.id, update);
      setProvKey(""); // clear key field after save
      alert("Provider configuration updated successfully!");
    } catch (err) {
      alert("Error: " + (err instanceof Error ? err.message : String(err)));
    } finally {
      setBusy(null);
    }
  };

  const handleSaveName = async () => {
    const trimmed = nameDraft.trim();
    if (!trimmed || trimmed === org.name) {
      setRenaming(false);
      setNameDraft(org.name);
      return;
    }
    setBusy("rename");
    try {
      const updated = await client.renameOrg(org.id, trimmed);
      onOrgUpdate(updated);
      setRenaming(false);
    } catch (err) {
      alert("Error: " + (err instanceof Error ? err.message : String(err)));
    } finally {
      setBusy(null);
    }
  };

  if (loading) {
    return <div className="p-8 text-zinc-400 flex items-center gap-2"><Loader2 className="w-4 h-4 animate-spin" /> Loading org details…</div>;
  }

  return (
    <div className="flex flex-col h-full text-white">
      <div className="mb-8">
        <div className="flex items-center justify-between">
          <div>
            {renaming ? (
              <div className="flex items-center gap-2 mb-2">
                <input
                  autoFocus
                  type="text"
                  value={nameDraft}
                  onChange={e => setNameDraft(e.target.value)}
                  onKeyDown={e => { if (e.key === "Enter") handleSaveName(); if (e.key === "Escape") { setRenaming(false); setNameDraft(org.name); } }}
                  className="bg-white/5 border border-white/10 rounded-lg px-3 py-1 text-2xl font-light tracking-tight focus:outline-none focus:border-indigo-500"
                />
                <button onClick={handleSaveName} disabled={busy === "rename"} className="text-green-400 hover:text-green-300">
                  {busy === "rename" ? <Loader2 className="w-5 h-5 animate-spin" /> : <Check className="w-5 h-5" />}
                </button>
                <button onClick={() => { setRenaming(false); setNameDraft(org.name); }} className="text-zinc-400 hover:text-white">
                  <X className="w-5 h-5" />
                </button>
              </div>
            ) : (
              <h1 className="text-3xl font-light tracking-tight mb-2 flex items-center gap-2">
                {org.name}
                <button onClick={() => setRenaming(true)} className="text-zinc-500 hover:text-white transition-colors" title="Rename organization">
                  <Pencil className="w-4 h-4" />
                </button>
              </h1>
            )}
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
          onClick={() => setActiveTab("usage")}
          className={`flex items-center gap-2 px-4 py-2 rounded-lg text-sm font-medium transition-colors ${activeTab === 'usage' ? 'bg-white/10 text-white' : 'text-zinc-400 hover:text-white hover:bg-white/5'}`}
        >
          <BarChart3 className="w-4 h-4" /> Usage
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
              <div className="overflow-x-auto">
              <table className="w-full text-sm text-left">
                <thead className="bg-white/5 border-b border-white/10 text-xs font-medium text-zinc-400">
                  <tr>
                    <th className="px-4 py-3">Name</th>
                    <th className="px-4 py-3">Email</th>
                    <th className="px-4 py-3">Role</th>
                    <th className="px-4 py-3">Joined</th>
                    <th className="px-4 py-3 text-right">API Keys</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-white/5">
                  {users.map(user => (
                    <Fragment key={user.id}>
                      <tr className="hover:bg-white/[0.02] transition-colors">
                        <td className="px-4 py-3 font-medium">{user.name}</td>
                        <td className="px-4 py-3 text-zinc-300">{user.email}</td>
                        <td className="px-4 py-3">
                          <span className={`inline-flex px-2 py-0.5 rounded text-xs ${user.role === 'admin' ? 'bg-indigo-500/10 text-indigo-400' : 'bg-white/10 text-zinc-300'}`}>
                            {user.role}
                          </span>
                        </td>
                        <td className="px-4 py-3 text-zinc-400">{new Date(user.created_at).toLocaleDateString()}</td>
                        <td className="px-4 py-3 text-right">
                          <button
                            onClick={() => toggleKeys(user.id)}
                            className="inline-flex items-center gap-1 text-xs bg-white/5 hover:bg-white/10 border border-white/10 rounded px-2 py-1 transition-colors"
                          >
                            <KeyRound className="w-3 h-3" /> Keys
                          </button>
                        </td>
                      </tr>
                      {expandedUserId === user.id && (
                        <tr>
                          <td colSpan={5} className="px-4 py-4 bg-black/20">
                            <div className="flex items-center justify-between mb-3">
                              <h3 className="text-xs font-bold text-zinc-500 uppercase tracking-widest">API Keys for {user.email}</h3>
                              <button
                                onClick={() => handleGenerateKey(user.id)}
                                disabled={busy === `genkey-${user.id}`}
                                className="flex items-center gap-1 text-xs bg-indigo-500/10 hover:bg-indigo-500/20 text-indigo-400 border border-indigo-500/20 rounded px-2 py-1 transition-colors"
                              >
                                {busy === `genkey-${user.id}` ? <Loader2 className="w-3 h-3 animate-spin" /> : <Plus className="w-3 h-3" />}
                                Generate Key
                              </button>
                            </div>

                            {newKey && newKey.userId === user.id && (
                              <div className="mb-3 p-3 rounded-lg border border-amber-500/30 bg-amber-500/5">
                                <p className="text-xs text-amber-400 mb-2">
                                  Shown once — copy it now. It is not stored in plaintext and cannot be retrieved again, only revoked.
                                </p>
                                <div className="flex items-center gap-2">
                                  <code className="flex-1 text-xs font-mono text-white break-all bg-black/30 px-2 py-1.5 rounded">
                                    {newKey.plaintext}
                                  </code>
                                  <button
                                    onClick={() => copyKey(newKey.plaintext)}
                                    className="text-xs bg-white/10 hover:bg-white/20 rounded px-2 py-1.5 shrink-0 transition-colors"
                                  >
                                    {copied ? "Copied!" : "Copy"}
                                  </button>
                                </div>
                              </div>
                            )}

                            {keysLoading === user.id ? (
                              <div className="text-xs text-zinc-500">Loading keys…</div>
                            ) : (
                              <div className="overflow-x-auto">
                              <table className="w-full text-xs text-left">
                                <thead className="text-zinc-500">
                                  <tr>
                                    <th className="py-1 pr-4 font-medium">Label</th>
                                    <th className="py-1 pr-4 font-medium">Created</th>
                                    <th className="py-1 pr-4 font-medium">Expires</th>
                                    <th className="py-1 text-right font-medium">Action</th>
                                  </tr>
                                </thead>
                                <tbody className="divide-y divide-white/5">
                                  {(keysByUser[user.id] ?? []).map(key => (
                                    <tr key={key.id}>
                                      <td className="py-1.5 pr-4">{key.label || "default"}</td>
                                      <td className="py-1.5 pr-4 text-zinc-400">{new Date(key.created_at).toLocaleDateString()}</td>
                                      <td className="py-1.5 pr-4 text-zinc-400">{key.expires_at ? new Date(key.expires_at).toLocaleDateString() : "Never"}</td>
                                      <td className="py-1.5 text-right">
                                        <button
                                          onClick={() => handleRevokeKey(user.id, key.id)}
                                          disabled={busy === `revoke-${key.id}`}
                                          className="text-red-400 hover:text-red-300 transition-colors"
                                        >
                                          {busy === `revoke-${key.id}` ? <Loader2 className="w-3 h-3 animate-spin inline" /> : "Revoke"}
                                        </button>
                                      </td>
                                    </tr>
                                  ))}
                                  {(keysByUser[user.id] ?? []).length === 0 && (
                                    <tr>
                                      <td colSpan={4} className="py-2 text-zinc-500">No active keys.</td>
                                    </tr>
                                  )}
                                </tbody>
                              </table>
                              </div>
                            )}
                          </td>
                        </tr>
                      )}
                    </Fragment>
                  ))}
                  {users.length === 0 && (
                    <tr>
                      <td colSpan={5} className="px-4 py-8 text-center text-zinc-500">No users found.</td>
                    </tr>
                  )}
                </tbody>
              </table>
              </div>
            </div>
          </div>
        )}

        {activeTab === 'usage' && (
          <div className="space-y-6">
            {modelUsage && Object.keys(modelUsage.tasks_by_status).length > 0 && (
              <div className="glass-panel p-5 border border-white/10 rounded-xl">
                <h2 className="text-xs font-bold text-zinc-500 uppercase tracking-widest mb-3">Task Queue</h2>
                <div className="flex gap-6">
                  {Object.entries(modelUsage.tasks_by_status).map(([status, count]) => (
                    <div key={status} className="flex items-baseline gap-2">
                      <span className="text-2xl font-light">{count}</span>
                      <span className="text-xs text-zinc-400">{status}</span>
                    </div>
                  ))}
                </div>
              </div>
            )}

            <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
              <div className="glass-panel border border-white/10 rounded-xl overflow-hidden">
                <h2 className="text-xs font-bold text-zinc-500 uppercase tracking-widest px-4 pt-4 pb-3">
                  Usage by Provider
                </h2>
                <div className="overflow-x-auto">
                <table className="w-full text-sm text-left">
                  <thead className="bg-white/5 border-b border-white/10 text-xs font-medium text-zinc-400">
                    <tr>
                      <th className="px-4 py-2">Provider</th>
                      <th className="px-4 py-2 text-right">Tasks</th>
                      <th className="px-4 py-2 text-right">Cost</th>
                      <th className="px-4 py-2 text-right">Kiwi-funded</th>
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-white/5">
                    {(modelUsage?.provider_usage ?? []).map((row) => (
                      <tr key={row.provider} className="hover:bg-white/[0.02] transition-colors">
                        <td className="px-4 py-2 font-medium">{providerLabel(row.provider)}</td>
                        <td className="px-4 py-2 text-right text-zinc-300">{row.task_count}</td>
                        <td className="px-4 py-2 text-right">${row.cost_usd.toFixed(2)}</td>
                        <td className="px-4 py-2 text-right text-zinc-400">${row.kiwi_cost_usd.toFixed(2)}</td>
                      </tr>
                    ))}
                    {(!modelUsage || modelUsage.provider_usage.length === 0) && (
                      <tr>
                        <td colSpan={4} className="px-4 py-8 text-center text-zinc-500">No usage recorded yet.</td>
                      </tr>
                    )}
                  </tbody>
                </table>
                </div>
              </div>
              <div className="glass-panel border border-white/10 rounded-xl overflow-hidden">
                <h2 className="text-xs font-bold text-zinc-500 uppercase tracking-widest px-4 pt-4 pb-3">
                  Usage by Model
                </h2>
                <div className="overflow-x-auto">
                <table className="w-full text-sm text-left">
                  <thead className="bg-white/5 border-b border-white/10 text-xs font-medium text-zinc-400">
                    <tr>
                      <th className="px-4 py-2">Model</th>
                      <th className="px-4 py-2 text-right">Tasks</th>
                      <th className="px-4 py-2 text-right">Cost</th>
                      <th className="px-4 py-2 text-right">Tokens</th>
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-white/5">
                    {(modelUsage?.model_usage ?? []).map((row) => (
                      <tr key={row.model} className="hover:bg-white/[0.02] transition-colors">
                        <td className="px-4 py-2 font-medium font-mono text-xs">{row.model}</td>
                        <td className="px-4 py-2 text-right text-zinc-300">{row.task_count}</td>
                        <td className="px-4 py-2 text-right">${row.cost_usd.toFixed(2)}</td>
                        <td className="px-4 py-2 text-right text-zinc-400">
                          {formatTokens(row.tokens_in)} in / {formatTokens(row.tokens_out)} out
                        </td>
                      </tr>
                    ))}
                    {(!modelUsage || modelUsage.model_usage.length === 0) && (
                      <tr>
                        <td colSpan={4} className="px-4 py-8 text-center text-zinc-500">No usage recorded yet.</td>
                      </tr>
                    )}
                  </tbody>
                </table>
                </div>
              </div>
            </div>

            <div className="glass-panel border border-white/10 rounded-xl overflow-hidden">
              <h2 className="text-xs font-bold text-zinc-500 uppercase tracking-widest px-4 pt-4 pb-3">
                Usage by User
              </h2>
              <div className="overflow-x-auto">
              <table className="w-full text-sm text-left">
                <thead className="bg-white/5 border-b border-white/10 text-xs font-medium text-zinc-400">
                  <tr>
                    <th className="px-4 py-2">User</th>
                    <th className="px-4 py-2 text-right">Tasks</th>
                    <th className="px-4 py-2 text-right">Succeeded</th>
                    <th className="px-4 py-2 text-right">Failed</th>
                    <th className="px-4 py-2 text-right">Cost</th>
                    <th className="px-4 py-2 text-right">Kiwi-funded</th>
                    <th className="px-4 py-2 text-right">Tokens</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-white/5">
                  {(modelUsage?.per_user ?? []).map((row) => (
                    <tr key={row.user_id} className="hover:bg-white/[0.02] transition-colors">
                      <td className="px-4 py-2">
                        <div className="font-medium">{row.email || row.user_id}</div>
                        <div className="text-[10px] text-zinc-500 font-mono">{row.user_id}</div>
                      </td>
                      <td className="px-4 py-2 text-right text-zinc-300">{row.task_count}</td>
                      <td className="px-4 py-2 text-right text-green-400">{row.succeeded}</td>
                      <td className="px-4 py-2 text-right text-red-400">{row.failed}</td>
                      <td className="px-4 py-2 text-right">${row.cost_usd.toFixed(2)}</td>
                      <td className="px-4 py-2 text-right text-zinc-400">${row.kiwi_cost_usd.toFixed(2)}</td>
                      <td className="px-4 py-2 text-right text-zinc-400">
                        {formatTokens(row.tokens_in)} in / {formatTokens(row.tokens_out)} out
                      </td>
                    </tr>
                  ))}
                  {(!modelUsage || modelUsage.per_user.length === 0) && (
                    <tr>
                      <td colSpan={7} className="px-4 py-8 text-center text-zinc-500">No usage recorded yet.</td>
                    </tr>
                  )}
                </tbody>
              </table>
              </div>
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
            <div className="overflow-x-auto">
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
          </div>
        )}
      </div>
    </div>
  );
}
