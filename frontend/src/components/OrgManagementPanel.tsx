"use client";

import { useEffect, useState, Fragment } from "react";
import { client, type AdminOrg, type AdminUser, type AdminAuditLog, type AdminProviderConfig, type AdminOrgModelUsage, type AdminAPIKey, type AdminDashboardSession, type AdminJoinRequest, formatTokens, providerLabel } from "@/lib/api";
import { shortTime, exactTime, formatDuration } from "@/lib/datetime";
import { Loader2, Users, Activity, Settings, Database, Plus, BarChart3, KeyRound, Pencil, Check, X, ShieldCheck, History } from "lucide-react";

export function OrgManagementPanel({ org, onOrgUpdate }: { org: AdminOrg; onOrgUpdate: (org: AdminOrg) => void }) {
  const [users, setUsers] = useState<AdminUser[]>([]);
  const [auditLogs, setAuditLogs] = useState<AdminAuditLog[]>([]);
  const [modelUsage, setModelUsage] = useState<AdminOrgModelUsage | null>(null);
  const [joinRequests, setJoinRequests] = useState<AdminJoinRequest[]>([]);
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState(false);
  const [activeTab, setActiveTab] = useState<"users" | "usage" | "audit" | "provider" | "access">("users");
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

  // Dashboard sessions, expanded per user — independent of the Keys panel
  // above (a superadmin may want either, both, or neither open for a given
  // user; they answer unrelated questions about the same row).
  const [expandedSessionsUserId, setExpandedSessionsUserId] = useState<string | null>(null);
  const [sessionsByUser, setSessionsByUser] = useState<Record<string, AdminDashboardSession[]>>({});
  const [sessionsLoading, setSessionsLoading] = useState<string | null>(null);

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
      client.listJoinRequests(org.id).catch(() => []),
    ]).then(([usrs, logs, prov, usage, reqs]) => {
      setUsers(usrs);
      setAuditLogs(logs);
      setModelUsage(usage);
      setJoinRequests(reqs);

      if (prov) {
        setProvName(prov.provider_name);
        setProvActor(prov.actor_model || "");
        setProvCritic(prov.critic_model || "");
      } else {
        setProvName("anthropic");
      }

      setLoading(false);
    }).catch(() => {
      setLoading(false);
      setLoadError(true);
    });
  }, [org.id]);

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

  const toggleSessions = (userId: string) => {
    if (expandedSessionsUserId === userId) {
      setExpandedSessionsUserId(null);
      return;
    }
    setExpandedSessionsUserId(userId);
    if (!sessionsByUser[userId]) {
      setSessionsLoading(userId);
      client.listAdminUserSessions(org.id, userId)
        .then(sessions => setSessionsByUser(prev => ({ ...prev, [userId]: sessions })))
        .catch(() => setSessionsByUser(prev => ({ ...prev, [userId]: [] })))
        .finally(() => setSessionsLoading(null));
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

  const handleToggleDomainJoin = async () => {
    setBusy("domain_join");
    try {
      const updated = await client.setDomainJoin(org.id, !org.domain_join);
      onOrgUpdate(updated);
    } catch (err) {
      alert("Error: " + (err instanceof Error ? err.message : String(err)));
    } finally {
      setBusy(null);
    }
  };

  const handleApproveJoinRequest = async (reqId: string) => {
    setBusy(`approve-${reqId}`);
    try {
      await client.approveJoinRequest(org.id, reqId);
      setJoinRequests(joinRequests.filter(r => r.id !== reqId));
    } catch (err) {
      alert("Error: " + (err instanceof Error ? err.message : String(err)));
    } finally {
      setBusy(null);
    }
  };

  const handleDenyJoinRequest = async (reqId: string) => {
    setBusy(`deny-${reqId}`);
    try {
      await client.denyJoinRequest(org.id, reqId);
      setJoinRequests(joinRequests.filter(r => r.id !== reqId));
    } catch (err) {
      alert("Error: " + (err instanceof Error ? err.message : String(err)));
    } finally {
      setBusy(null);
    }
  };

  if (loading) {
    return <div className="p-8 text-stone-500 flex items-center gap-2"><Loader2 className="w-4 h-4 animate-spin" /> Loading org details…</div>;
  }

  if (loadError) {
    return <div className="p-8 text-rose-600">Failed to load org details.</div>;
  }

  return (
    <div className="flex flex-col h-full text-stone-900">
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
                  className="bg-sand-50 border border-sand-200 rounded-lg px-3 py-1 text-2xl font-light tracking-tight focus:outline-none focus:border-stone-400"
                />
                <button onClick={handleSaveName} disabled={busy === "rename"} className="text-emerald-600 hover:text-emerald-700">
                  {busy === "rename" ? <Loader2 className="w-5 h-5 animate-spin" /> : <Check className="w-5 h-5" />}
                </button>
                <button onClick={() => { setRenaming(false); setNameDraft(org.name); }} className="text-stone-500 hover:text-stone-900">
                  <X className="w-5 h-5" />
                </button>
              </div>
            ) : (
              <h1 className="text-3xl font-light tracking-tight mb-2 flex items-center gap-2">
                {org.name}
                <button onClick={() => { setNameDraft(org.name); setRenaming(true); }} className="text-stone-400 hover:text-stone-900 transition-colors" title="Rename organization">
                  <Pencil className="w-4 h-4" />
                </button>
              </h1>
            )}
            <p className="text-stone-500 font-mono text-sm">ID: {org.id} &bull; Plan: {org.plan} &bull; Status: {org.activation_state}</p>
          </div>
        </div>
      </div>

      <div className="flex gap-4 mb-6 border-b border-sand-200 pb-4">
        <button
          onClick={() => setActiveTab("users")}
          className={`flex items-center gap-2 px-4 py-2 rounded-lg text-sm font-medium transition-colors ${activeTab === 'users' ? 'bg-sand-100 text-stone-900' : 'text-stone-500 hover:text-stone-900 hover:bg-sand-50'}`}
        >
          <Users className="w-4 h-4" /> Users
        </button>
        <button
          onClick={() => setActiveTab("usage")}
          className={`flex items-center gap-2 px-4 py-2 rounded-lg text-sm font-medium transition-colors ${activeTab === 'usage' ? 'bg-sand-100 text-stone-900' : 'text-stone-500 hover:text-stone-900 hover:bg-sand-50'}`}
        >
          <BarChart3 className="w-4 h-4" /> Usage
        </button>
        <button
          onClick={() => setActiveTab("provider")}
          className={`flex items-center gap-2 px-4 py-2 rounded-lg text-sm font-medium transition-colors ${activeTab === 'provider' ? 'bg-sand-100 text-stone-900' : 'text-stone-500 hover:text-stone-900 hover:bg-sand-50'}`}
        >
          <Database className="w-4 h-4" /> Provider Config
        </button>
        <button
          onClick={() => setActiveTab("audit")}
          className={`flex items-center gap-2 px-4 py-2 rounded-lg text-sm font-medium transition-colors ${activeTab === 'audit' ? 'bg-sand-100 text-stone-900' : 'text-stone-500 hover:text-stone-900 hover:bg-sand-50'}`}
        >
          <Activity className="w-4 h-4" /> Audit Logs
        </button>
        <button
          onClick={() => setActiveTab("access")}
          className={`flex items-center gap-2 px-4 py-2 rounded-lg text-sm font-medium transition-colors ${activeTab === 'access' ? 'bg-sand-100 text-stone-900' : 'text-stone-500 hover:text-stone-900 hover:bg-sand-50'}`}
        >
          <ShieldCheck className="w-4 h-4" /> Access
        </button>
      </div>

      <div className="flex-1 overflow-auto">
        {activeTab === 'users' && (
          <div className="space-y-6">
            <div className="bg-white shadow-2xs p-6 border border-sand-200 rounded-xl">
              <h2 className="text-lg font-medium mb-4 flex items-center gap-2">
                <Plus className="w-5 h-5" /> Add User
              </h2>
              <form onSubmit={handleCreateUser} className="flex gap-4 items-end">
                <div className="flex-1">
                  <label className="block text-xs text-stone-500 mb-1">Name</label>
                  <input type="text" value={newName} onChange={e => setNewName(e.target.value)} required className="w-full bg-sand-50 border border-sand-200 rounded-lg px-3 py-2 text-sm focus:outline-none focus:border-stone-400" placeholder="John Doe" />
                </div>
                <div className="flex-1">
                  <label className="block text-xs text-stone-500 mb-1">Email</label>
                  <input type="email" value={newEmail} onChange={e => setNewEmail(e.target.value)} required className="w-full bg-sand-50 border border-sand-200 rounded-lg px-3 py-2 text-sm focus:outline-none focus:border-stone-400" placeholder="john@example.com" />
                </div>
                <div className="w-32">
                  <label className="block text-xs text-stone-500 mb-1">Role</label>
                  <select value={newRole} onChange={e => setNewRole(e.target.value)} className="w-full bg-sand-50 border border-sand-200 rounded-lg px-3 py-2 text-sm focus:outline-none focus:border-stone-400">
                    <option value="member">Member</option>
                    <option value="admin">Admin</option>
                  </select>
                </div>
                <button type="submit" disabled={!!busy} className="bg-stone-900 hover:bg-stone-800 text-white px-4 py-2 rounded-xl text-sm font-semibold transition-colors h-[38px] flex items-center justify-center min-w-[100px]">
                  {busy === 'create_user' ? <Loader2 className="w-4 h-4 animate-spin" /> : 'Create User'}
                </button>
              </form>
            </div>

            <div className="bg-white shadow-2xs border border-sand-200 rounded-xl overflow-hidden">
              <div className="overflow-x-auto">
              <table className="w-full text-sm text-left">
                <thead className="bg-sand-50 border-b border-sand-200 text-xs font-medium text-stone-500">
                  <tr>
                    <th className="px-4 py-3">Name</th>
                    <th className="px-4 py-3">Email</th>
                    <th className="px-4 py-3">Role</th>
                    <th className="px-4 py-3">Joined</th>
                    <th className="px-4 py-3">Sign-ins</th>
                    <th className="px-4 py-3">Last seen</th>
                    <th className="px-4 py-3 text-right">Details</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-sand-150">
                  {users.map(user => (
                    <Fragment key={user.id}>
                      <tr className="hover:bg-sand-50/60 transition-colors">
                        <td className="px-4 py-3 font-medium">{user.name}</td>
                        <td className="px-4 py-3 text-stone-700">{user.email}</td>
                        <td className="px-4 py-3">
                          <span className={`inline-flex px-2 py-0.5 rounded text-xs ${user.role === 'admin' ? 'bg-indigo-50 text-indigo-700 border border-indigo-200' : 'bg-sand-100 text-stone-700 border border-sand-200'}`}>
                            {user.role}
                          </span>
                        </td>
                        <td className="px-4 py-3 text-stone-500">{new Date(user.created_at).toLocaleDateString()}</td>
                        <td className="px-4 py-3 text-stone-700">{user.sign_in_count}</td>
                        <td className="px-4 py-3 text-stone-500">
                          {user.last_seen_at ? (
                            <span title={exactTime(user.last_seen_at)}>{shortTime(user.last_seen_at)}</span>
                          ) : (
                            <span className="text-stone-400">Never</span>
                          )}
                        </td>
                        <td className="px-4 py-3 text-right">
                          <div className="flex items-center justify-end gap-2">
                            <button
                              onClick={() => toggleKeys(user.id)}
                              className="inline-flex items-center gap-1 text-xs bg-sand-50 hover:bg-sand-100 border border-sand-200 rounded px-2 py-1 transition-colors"
                            >
                              <KeyRound className="w-3 h-3" /> Keys
                            </button>
                            <button
                              onClick={() => toggleSessions(user.id)}
                              className="inline-flex items-center gap-1 text-xs bg-sand-50 hover:bg-sand-100 border border-sand-200 rounded px-2 py-1 transition-colors"
                            >
                              <History className="w-3 h-3" /> Sessions
                            </button>
                          </div>
                        </td>
                      </tr>
                      {expandedUserId === user.id && (
                        <tr>
                          <td colSpan={7} className="px-4 py-4 bg-sand-50">
                            <div className="flex items-center justify-between mb-3">
                              <h3 className="text-xs font-bold text-stone-400 uppercase tracking-widest">API Keys for {user.email}</h3>
                              <button
                                onClick={() => handleGenerateKey(user.id)}
                                disabled={busy === `genkey-${user.id}`}
                                className="flex items-center gap-1 text-xs bg-indigo-50 hover:bg-indigo-100 text-indigo-700 border border-indigo-200 rounded-lg px-2 py-1 transition-colors"
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
                                  <code className="flex-1 text-xs font-mono text-stone-900 break-all bg-stone-900 px-2 py-1.5 rounded">
                                    {newKey.plaintext}
                                  </code>
                                  <button
                                    onClick={() => copyKey(newKey.plaintext)}
                                    className="text-xs bg-sand-100 hover:bg-sand-200 rounded px-2 py-1.5 shrink-0 transition-colors"
                                  >
                                    {copied ? "Copied!" : "Copy"}
                                  </button>
                                </div>
                              </div>
                            )}

                            {keysLoading === user.id ? (
                              <div className="text-xs text-stone-400">Loading keys…</div>
                            ) : (
                              <div className="overflow-x-auto">
                              <table className="w-full text-xs text-left">
                                <thead className="text-stone-400">
                                  <tr>
                                    <th className="py-1 pr-4 font-medium">Label</th>
                                    <th className="py-1 pr-4 font-medium">Created</th>
                                    <th className="py-1 pr-4 font-medium">Expires</th>
                                    <th className="py-1 text-right font-medium">Action</th>
                                  </tr>
                                </thead>
                                <tbody className="divide-y divide-sand-150">
                                  {(keysByUser[user.id] ?? []).map(key => (
                                    <tr key={key.id}>
                                      <td className="py-1.5 pr-4">{key.label || "default"}</td>
                                      <td className="py-1.5 pr-4 text-stone-500">{new Date(key.created_at).toLocaleDateString()}</td>
                                      <td className="py-1.5 pr-4 text-stone-500">{key.expires_at ? new Date(key.expires_at).toLocaleDateString() : "Never"}</td>
                                      <td className="py-1.5 text-right">
                                        <button
                                          onClick={() => handleRevokeKey(user.id, key.id)}
                                          disabled={busy === `revoke-${key.id}`}
                                          className="text-rose-600 hover:text-rose-700 transition-colors"
                                        >
                                          {busy === `revoke-${key.id}` ? <Loader2 className="w-3 h-3 animate-spin inline" /> : "Revoke"}
                                        </button>
                                      </td>
                                    </tr>
                                  ))}
                                  {(keysByUser[user.id] ?? []).length === 0 && (
                                    <tr>
                                      <td colSpan={4} className="py-2 text-stone-400">No active keys.</td>
                                    </tr>
                                  )}
                                </tbody>
                              </table>
                              </div>
                            )}
                          </td>
                        </tr>
                      )}
                      {expandedSessionsUserId === user.id && (
                        <tr>
                          <td colSpan={7} className="px-4 py-4 bg-sand-50">
                            <h3 className="text-xs font-bold text-stone-400 uppercase tracking-widest mb-3">Dashboard Sessions for {user.email}</h3>

                            {sessionsLoading === user.id ? (
                              <div className="text-xs text-stone-400">Loading sessions…</div>
                            ) : (
                              <div className="overflow-x-auto">
                              <table className="w-full text-xs text-left">
                                <thead className="text-stone-400">
                                  <tr>
                                    <th className="py-1 pr-4 font-medium">Started</th>
                                    <th className="py-1 pr-4 font-medium">Last Activity</th>
                                    <th className="py-1 font-medium">Duration</th>
                                  </tr>
                                </thead>
                                <tbody className="divide-y divide-sand-150">
                                  {(sessionsByUser[user.id] ?? []).map(session => (
                                    <tr key={session.id}>
                                      <td className="py-1.5 pr-4 text-stone-700" title={exactTime(session.started_at)}>{shortTime(session.started_at)}</td>
                                      <td className="py-1.5 pr-4 text-stone-700" title={exactTime(session.last_activity_at)}>{shortTime(session.last_activity_at)}</td>
                                      <td className="py-1.5 text-stone-700">{formatDuration(session.duration_seconds * 1000)}</td>
                                    </tr>
                                  ))}
                                  {(sessionsByUser[user.id] ?? []).length === 0 && (
                                    <tr>
                                      <td colSpan={3} className="py-2 text-stone-400">No dashboard sessions recorded yet.</td>
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
                      <td colSpan={7} className="px-4 py-8 text-center text-stone-400">No users found.</td>
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
              <div className="bg-white shadow-2xs p-5 border border-sand-200 rounded-xl">
                <h2 className="text-xs font-bold text-stone-400 uppercase tracking-widest mb-3">Task Queue</h2>
                <div className="flex gap-6">
                  {Object.entries(modelUsage.tasks_by_status).map(([status, count]) => (
                    <div key={status} className="flex items-baseline gap-2">
                      <span className="text-2xl font-light">{count}</span>
                      <span className="text-xs text-stone-500">{status}</span>
                    </div>
                  ))}
                </div>
              </div>
            )}

            <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
              <div className="bg-white shadow-2xs border border-sand-200 rounded-xl overflow-hidden">
                <h2 className="text-xs font-bold text-stone-400 uppercase tracking-widest px-4 pt-4 pb-3">
                  Usage by Provider
                </h2>
                <div className="overflow-x-auto">
                <table className="w-full text-sm text-left">
                  <thead className="bg-sand-50 border-b border-sand-200 text-xs font-medium text-stone-500">
                    <tr>
                      <th className="px-4 py-2">Provider</th>
                      <th className="px-4 py-2 text-right">Tasks</th>
                      <th className="px-4 py-2 text-right">Cost</th>
                      <th className="px-4 py-2 text-right">Kiwi-funded</th>
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-sand-150">
                    {(modelUsage?.provider_usage ?? []).map((row) => (
                      <tr key={row.provider} className="hover:bg-sand-50/60 transition-colors">
                        <td className="px-4 py-2 font-medium">{providerLabel(row.provider)}</td>
                        <td className="px-4 py-2 text-right text-stone-700">{row.task_count}</td>
                        <td className="px-4 py-2 text-right">${row.cost_usd.toFixed(2)}</td>
                        <td className="px-4 py-2 text-right text-stone-500">${row.kiwi_cost_usd.toFixed(2)}</td>
                      </tr>
                    ))}
                    {(!modelUsage || modelUsage.provider_usage.length === 0) && (
                      <tr>
                        <td colSpan={4} className="px-4 py-8 text-center text-stone-400">No usage recorded yet.</td>
                      </tr>
                    )}
                  </tbody>
                </table>
                </div>
              </div>
              <div className="bg-white shadow-2xs border border-sand-200 rounded-xl overflow-hidden">
                <h2 className="text-xs font-bold text-stone-400 uppercase tracking-widest px-4 pt-4 pb-3">
                  Usage by Model
                </h2>
                <div className="overflow-x-auto">
                <table className="w-full text-sm text-left">
                  <thead className="bg-sand-50 border-b border-sand-200 text-xs font-medium text-stone-500">
                    <tr>
                      <th className="px-4 py-2">Model</th>
                      <th className="px-4 py-2 text-right">Tasks</th>
                      <th className="px-4 py-2 text-right">Cost</th>
                      <th className="px-4 py-2 text-right">Tokens</th>
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-sand-150">
                    {(modelUsage?.model_usage ?? []).map((row) => (
                      <tr key={row.model} className="hover:bg-sand-50/60 transition-colors">
                        <td className="px-4 py-2 font-medium font-mono text-xs">{row.model}</td>
                        <td className="px-4 py-2 text-right text-stone-700">{row.task_count}</td>
                        <td className="px-4 py-2 text-right">${row.cost_usd.toFixed(2)}</td>
                        <td className="px-4 py-2 text-right text-stone-500">
                          {formatTokens(row.tokens_in)} in / {formatTokens(row.tokens_out)} out
                        </td>
                      </tr>
                    ))}
                    {(!modelUsage || modelUsage.model_usage.length === 0) && (
                      <tr>
                        <td colSpan={4} className="px-4 py-8 text-center text-stone-400">No usage recorded yet.</td>
                      </tr>
                    )}
                  </tbody>
                </table>
                </div>
              </div>
            </div>

            <div className="bg-white shadow-2xs border border-sand-200 rounded-xl overflow-hidden">
              <h2 className="text-xs font-bold text-stone-400 uppercase tracking-widest px-4 pt-4 pb-3">
                Usage by User
              </h2>
              <div className="overflow-x-auto">
              <table className="w-full text-sm text-left">
                <thead className="bg-sand-50 border-b border-sand-200 text-xs font-medium text-stone-500">
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
                <tbody className="divide-y divide-sand-150">
                  {(modelUsage?.per_user ?? []).map((row) => (
                    <tr key={row.user_id} className="hover:bg-sand-50/60 transition-colors">
                      <td className="px-4 py-2">
                        <div className="font-medium">{row.email || row.user_id}</div>
                        <div className="text-[10px] text-stone-400 font-mono">{row.user_id}</div>
                      </td>
                      <td className="px-4 py-2 text-right text-stone-700">{row.task_count}</td>
                      <td className="px-4 py-2 text-right text-emerald-600">{row.succeeded}</td>
                      <td className="px-4 py-2 text-right text-rose-600">{row.failed}</td>
                      <td className="px-4 py-2 text-right">${row.cost_usd.toFixed(2)}</td>
                      <td className="px-4 py-2 text-right text-stone-500">${row.kiwi_cost_usd.toFixed(2)}</td>
                      <td className="px-4 py-2 text-right text-stone-500">
                        {formatTokens(row.tokens_in)} in / {formatTokens(row.tokens_out)} out
                      </td>
                    </tr>
                  ))}
                  {(!modelUsage || modelUsage.per_user.length === 0) && (
                    <tr>
                      <td colSpan={7} className="px-4 py-8 text-center text-stone-400">No usage recorded yet.</td>
                    </tr>
                  )}
                </tbody>
              </table>
              </div>
            </div>
          </div>
        )}

        {activeTab === 'provider' && (
          <div className="bg-white shadow-2xs p-6 border border-sand-200 rounded-xl max-w-2xl">
            <h2 className="text-lg font-medium mb-4 flex items-center gap-2">
              <Settings className="w-5 h-5" /> LLM Provider Override
            </h2>
            <p className="text-sm text-stone-500 mb-6">
              Configure custom LLM provider settings for this organization. This will override global defaults.
            </p>
            <form onSubmit={handleSaveProvider} className="space-y-4">
              <div>
                <label className="block text-sm font-medium text-stone-700 mb-1">Provider Name</label>
                <select value={provName} onChange={e => setProvName(e.target.value)} className="w-full bg-sand-50 border border-sand-200 rounded-lg px-3 py-2 text-sm focus:outline-none focus:border-stone-400">
                  <option value="anthropic">Anthropic</option>
                  <option value="openai">OpenAI</option>
                  <option value="gemini">Gemini</option>
                </select>
              </div>
              <div>
                <label className="block text-sm font-medium text-stone-700 mb-1">API Key</label>
                <input type="password" value={provKey} onChange={e => setProvKey(e.target.value)} className="w-full bg-sand-50 border border-sand-200 rounded-lg px-3 py-2 text-sm focus:outline-none focus:border-stone-400" placeholder="Leave blank to keep existing key" />
                <p className="text-xs text-stone-400 mt-1">Stored securely. Only enter a new key to update.</p>
              </div>
              <div className="grid grid-cols-2 gap-4">
                <div>
                  <label className="block text-sm font-medium text-stone-700 mb-1">Actor Model</label>
                  <input type="text" value={provActor} onChange={e => setProvActor(e.target.value)} className="w-full bg-sand-50 border border-sand-200 rounded-lg px-3 py-2 text-sm focus:outline-none focus:border-stone-400" placeholder="e.g. claude-3-5-sonnet-20241022" />
                </div>
                <div>
                  <label className="block text-sm font-medium text-stone-700 mb-1">Critic Model</label>
                  <input type="text" value={provCritic} onChange={e => setProvCritic(e.target.value)} className="w-full bg-sand-50 border border-sand-200 rounded-lg px-3 py-2 text-sm focus:outline-none focus:border-stone-400" placeholder="e.g. claude-3-5-haiku-20241022" />
                </div>
              </div>
              <div className="pt-4 border-t border-sand-200 mt-6">
                <button type="submit" disabled={!!busy} className="bg-stone-900 hover:bg-stone-800 text-white px-6 py-2 rounded-xl text-sm font-semibold transition-colors flex items-center justify-center min-w-[120px]">
                  {busy === 'save_provider' ? <Loader2 className="w-4 h-4 animate-spin" /> : 'Save Config'}
                </button>
              </div>
            </form>
          </div>
        )}

        {activeTab === 'audit' && (
          <div className="bg-white shadow-2xs border border-sand-200 rounded-xl overflow-hidden">
            <div className="overflow-x-auto">
            <table className="w-full text-sm text-left">
              <thead className="bg-sand-50 border-b border-sand-200 text-xs font-medium text-stone-500">
                <tr>
                  <th className="px-4 py-3">Timestamp</th>
                  <th className="px-4 py-3">User</th>
                  <th className="px-4 py-3">Action</th>
                  <th className="px-4 py-3">Resource</th>
                  <th className="px-4 py-3">Details</th>
                  <th className="px-4 py-3">IP Address</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-sand-150">
                {auditLogs.map(log => (
                  <tr key={log.id} className="hover:bg-sand-50/60 transition-colors">
                    <td className="px-4 py-3 text-stone-500 whitespace-nowrap">{new Date(log.created_at).toLocaleString()}</td>
                    <td className="px-4 py-3">
                      <div className="font-medium">{log.user_email || 'System'}</div>
                      {log.user_id && <div className="text-[10px] text-stone-400 font-mono">{log.user_id}</div>}
                    </td>
                    <td className="px-4 py-3">
                      <span className="inline-flex px-2 py-0.5 rounded text-xs bg-sand-100 text-stone-700 font-mono">
                        {log.action}
                      </span>
                    </td>
                    <td className="px-4 py-3">
                      <div className="font-mono text-stone-700">{log.resource}</div>
                      <div className="text-[10px] text-stone-400 font-mono truncate max-w-[120px]">{log.resource_id}</div>
                    </td>
                    <td className="px-4 py-3 text-stone-700 truncate max-w-md">{log.details}</td>
                    <td className="px-4 py-3 text-stone-400 font-mono text-xs">{log.client_ip || '-'}</td>
                  </tr>
                ))}
                {auditLogs.length === 0 && (
                  <tr>
                    <td colSpan={6} className="px-4 py-8 text-center text-stone-400">No audit logs found.</td>
                  </tr>
                )}
              </tbody>
            </table>
            </div>
          </div>
        )}

        {activeTab === 'access' && (
          <div className="space-y-6">
            <div className="bg-white shadow-2xs p-6 border border-sand-200 rounded-xl max-w-2xl">
              <h2 className="text-lg font-medium mb-2 flex items-center gap-2">
                <ShieldCheck className="w-5 h-5" /> Domain join
              </h2>
              <p className="text-sm text-stone-500 mb-4">
                {org.primary_domain
                  ? `When on, anyone signing up with an @${org.primary_domain} email joins this org immediately, without approval.`
                  : "This org has no primary domain set — domain join has no effect until one is configured."}
              </p>
              <button
                type="button"
                role="switch"
                aria-checked={org.domain_join}
                disabled={busy === "domain_join"}
                onClick={handleToggleDomainJoin}
                className={`flex items-center gap-2 px-4 py-2 rounded-xl text-xs font-semibold border transition-all disabled:opacity-40 disabled:cursor-not-allowed ${
                  org.domain_join
                    ? "border-emerald-300 bg-emerald-50 text-emerald-700"
                    : "border-sand-200 bg-sand-50 text-stone-500 hover:text-stone-900"
                }`}
              >
                {busy === "domain_join" ? <Loader2 className="w-4 h-4 animate-spin" /> : null}
                <span>{org.domain_join ? "On" : "Off"}</span>
              </button>
            </div>

            <div className="bg-white shadow-2xs border border-sand-200 rounded-xl overflow-hidden">
              <div className="px-6 py-4 border-b border-sand-200">
                <h2 className="text-lg font-medium">Pending join requests</h2>
              </div>
              <div className="overflow-x-auto">
              <table className="w-full text-sm text-left">
                <thead className="bg-sand-50 border-b border-sand-200 text-xs font-medium text-stone-500">
                  <tr>
                    <th className="px-4 py-3">Email</th>
                    <th className="px-4 py-3">Requested</th>
                    <th className="px-4 py-3 text-right">Actions</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-sand-150">
                  {joinRequests.map(req => (
                    <tr key={req.id} className="hover:bg-sand-50/60 transition-colors">
                      <td className="px-4 py-3 font-medium">{req.user_email}</td>
                      <td className="px-4 py-3 text-stone-500">{new Date(req.created_at).toLocaleDateString()}</td>
                      <td className="px-4 py-3 text-right space-x-2">
                        <button
                          onClick={() => handleApproveJoinRequest(req.id)}
                          disabled={!!busy}
                          className="text-xs bg-emerald-50 hover:bg-emerald-100 border border-emerald-200 text-emerald-700 rounded-lg px-2 py-1 transition-colors"
                        >
                          {busy === `approve-${req.id}` ? <Loader2 className="w-3 h-3 animate-spin inline" /> : 'Approve'}
                        </button>
                        <button
                          onClick={() => handleDenyJoinRequest(req.id)}
                          disabled={!!busy}
                          className="text-xs bg-rose-50 hover:bg-rose-100 border border-rose-200 text-rose-700 rounded-lg px-2 py-1 transition-colors"
                        >
                          {busy === `deny-${req.id}` ? <Loader2 className="w-3 h-3 animate-spin inline" /> : 'Deny'}
                        </button>
                      </td>
                    </tr>
                  ))}
                  {joinRequests.length === 0 && (
                    <tr>
                      <td colSpan={3} className="px-4 py-8 text-center text-stone-400">No pending join requests.</td>
                    </tr>
                  )}
                </tbody>
              </table>
              </div>
            </div>
          </div>
        )}
      </div>
    </div>
  );
}
