"use client";

import React, { useEffect, useState, useMemo } from "react";
import {
  client,
  type AdminOrg,
  type AdminUser,
  type AdminAuditLog,
  type AdminProviderConfig,
  type AdminOrgModelUsage,
  type AdminAPIKey,
  type AdminDashboardSession,
  type AdminJoinRequest,
  formatTokens,
  providerLabel,
} from "@/lib/api";
import { shortTime, exactTime, formatDuration } from "@/lib/datetime";
import {
  Users,
  UserPlus,
  ShieldCheck,
  KeyRound,
  History,
  BarChart3,
  Database,
  Activity,
  Search,
  Copy,
  Check,
  Pencil,
  Plus,
  X,
  Lock,
  Clock,
  Globe,
  AlertCircle,
  User,
  CheckCircle2,
} from "lucide-react";
import { KiwiMicroButtonLoader } from "@/components/KiwiLoaders";

function getAvatarInitials(email?: string, name?: string): string {
  if (email && email.trim()) {
    const localPart = email.split("@")[0].trim();
    if (localPart.length >= 2) {
      return localPart.slice(0, 2).toUpperCase();
    }
  }
  if (name && name.trim()) {
    const parts = name.trim().split(/[\s_.-]+/);
    if (parts.length >= 2 && parts[0] && parts[1]) {
      return (parts[0][0] + parts[1][0]).toUpperCase();
    }
    return name.slice(0, 2).toUpperCase();
  }
  return "KW";
}

const AVATAR_BG_COLORS = [
  "bg-emerald-700 text-emerald-100",
  "bg-indigo-700 text-indigo-100",
  "bg-amber-700 text-amber-100",
  "bg-purple-700 text-purple-100",
  "bg-sky-700 text-sky-100",
  "bg-rose-700 text-rose-100",
  "bg-stone-800 text-stone-100",
];

function getAvatarColor(identifier: string): string {
  let hash = 0;
  for (let i = 0; i < identifier.length; i++) {
    hash = identifier.charCodeAt(i) + ((hash << 5) - hash);
  }
  const index = Math.abs(hash) % AVATAR_BG_COLORS.length;
  return AVATAR_BG_COLORS[index];
}

export function OrgManagementPanel({
  org,
  onOrgUpdate,
}: {
  org: AdminOrg;
  onOrgUpdate: (org: AdminOrg) => void;
}) {
  const [users, setUsers] = useState<AdminUser[]>([]);
  const [auditLogs, setAuditLogs] = useState<AdminAuditLog[]>([]);
  const [modelUsage, setModelUsage] = useState<AdminOrgModelUsage | null>(null);
  const [joinRequests, setJoinRequests] = useState<AdminJoinRequest[]>([]);
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState(false);
  const [activeTab, setActiveTab] = useState<"users" | "usage" | "audit" | "provider" | "access">("users");
  const [busy, setBusy] = useState<string | null>(null);

  // Search & Filters
  const [userQuery, setUserQuery] = useState("");
  const [roleFilter, setRoleFilter] = useState<"all" | "admin" | "member">("all");
  const [copiedId, setCopiedId] = useState(false);

  // Invite User Modal
  const [showInviteModal, setShowInviteModal] = useState(false);
  const [newEmail, setNewEmail] = useState("");
  const [newName, setNewName] = useState("");
  const [newRole, setNewRole] = useState("member");
  const [inviteFeedback, setInviteFeedback] = useState<{ type: "success" | "error"; message: string } | null>(null);

  // Keys Modal
  const [activeKeysUser, setActiveKeysUser] = useState<AdminUser | null>(null);
  const [keysByUser, setKeysByUser] = useState<Record<string, AdminAPIKey[]>>({});
  const [keysLoading, setKeysLoading] = useState<string | null>(null);
  const [newKey, setNewKey] = useState<{ userId: string; plaintext: string } | null>(null);
  const [copiedKey, setCopiedKey] = useState(false);
  const [keyLabelInput, setKeyLabelInput] = useState("cli");
  const [showAddKeyInput, setShowAddKeyInput] = useState(false);

  // Sessions Modal
  const [activeSessionsUser, setActiveSessionsUser] = useState<AdminUser | null>(null);
  const [sessionsByUser, setSessionsByUser] = useState<Record<string, AdminDashboardSession[]>>({});
  const [sessionsLoading, setSessionsLoading] = useState<string | null>(null);

  // Provider config form
  const [provName, setProvName] = useState("");
  const [provActor, setProvActor] = useState("");
  const [provCritic, setProvCritic] = useState("");
  const [provKey, setProvKey] = useState("");
  const [provSuccess, setProvSuccess] = useState(false);

  // Rename org state
  const [renaming, setRenaming] = useState(false);
  const [nameDraft, setNameDraft] = useState(org.name);

  useEffect(() => {
    Promise.all([
      client.listAdminOrgUsers(org.id),
      client.getAdminOrgAuditLogs(org.id),
      client.getAdminOrgProviderConfig(org.id).catch(() => null),
      client.getAdminOrgModelUsage(org.id).catch(() => null),
      client.listJoinRequests(org.id).catch(() => []),
    ])
      .then(([usrs, logs, prov, usage, reqs]) => {
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
      })
      .catch(() => {
        setLoading(false);
        setLoadError(true);
      });
  }, [org.id]);

  const handleCopyOrgId = () => {
    navigator.clipboard.writeText(org.id).then(() => {
      setCopiedId(true);
      setTimeout(() => setCopiedId(false), 2000);
    });
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

  const handleCreateUser = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!newEmail.trim() || !newName.trim()) return;

    setBusy("create_user");
    setInviteFeedback(null);
    try {
      const u = await client.createAdminOrgUser(org.id, newEmail.trim(), newName.trim(), newRole);
      setUsers([u, ...users]);
      setInviteFeedback({ type: "success", message: `Added ${newName} (${newEmail}) to the team.` });
      setNewEmail("");
      setNewName("");
      setNewRole("member");
      setTimeout(() => {
        setShowInviteModal(false);
        setInviteFeedback(null);
      }, 1200);
    } catch (err) {
      setInviteFeedback({
        type: "error",
        message: err instanceof Error ? err.message : "Failed to add user",
      });
    } finally {
      setBusy(null);
    }
  };

  const openKeysModal = (user: AdminUser) => {
    setActiveKeysUser(user);
    setNewKey(null);
    setShowAddKeyInput(false);
    if (!keysByUser[user.id]) {
      setKeysLoading(user.id);
      client
        .listAdminUserAPIKeys(org.id, user.id)
        .then((keys) => setKeysByUser((prev) => ({ ...prev, [user.id]: keys })))
        .catch(() => setKeysByUser((prev) => ({ ...prev, [user.id]: [] })))
        .finally(() => setKeysLoading(null));
    }
  };

  const openSessionsModal = (user: AdminUser) => {
    setActiveSessionsUser(user);
    if (!sessionsByUser[user.id]) {
      setSessionsLoading(user.id);
      client
        .listAdminUserSessions(org.id, user.id)
        .then((sessions) => setSessionsByUser((prev) => ({ ...prev, [user.id]: sessions })))
        .catch(() => setSessionsByUser((prev) => ({ ...prev, [user.id]: [] })))
        .finally(() => setSessionsLoading(null));
    }
  };

  const handleGenerateKey = async (userId: string) => {
    setBusy(`genkey-${userId}`);
    try {
      const created = await client.createAdminUserAPIKey(org.id, userId, keyLabelInput || "cli");
      setNewKey({ userId, plaintext: created.key });
      setCopiedKey(false);
      setShowAddKeyInput(false);
      setKeyLabelInput("cli");
      setKeysByUser((prev) => ({
        ...prev,
        [userId]: [
          {
            id: created.key_id,
            user_id: userId,
            label: created.label,
            created_at: created.created_at,
            expires_at: created.expires_at ?? undefined,
          },
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
    if (!confirm("Revoke this API key? Any CLI session or automation using it will stop working immediately.")) return;

    setBusy(`revoke-${keyId}`);
    try {
      await client.revokeAdminUserAPIKey(org.id, userId, keyId);
      setKeysByUser((prev) => ({
        ...prev,
        [userId]: (prev[userId] ?? []).filter((k) => k.id !== keyId),
      }));
    } catch (err) {
      alert("Error: " + (err instanceof Error ? err.message : String(err)));
    } finally {
      setBusy(null);
    }
  };

  const copyKeyText = (text: string) => {
    navigator.clipboard.writeText(text).then(() => {
      setCopiedKey(true);
      setTimeout(() => setCopiedKey(false), 2000);
    });
  };

  const handleSaveProvider = async (e: React.FormEvent) => {
    e.preventDefault();
    setBusy("save_provider");
    setProvSuccess(false);
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
      setProvKey("");
      setProvSuccess(true);
      setTimeout(() => setProvSuccess(false), 3000);
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
      setJoinRequests(joinRequests.filter((r) => r.id !== reqId));
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
      setJoinRequests(joinRequests.filter((r) => r.id !== reqId));
    } catch (err) {
      alert("Error: " + (err instanceof Error ? err.message : String(err)));
    } finally {
      setBusy(null);
    }
  };

  const filteredUsers = useMemo(() => {
    const q = userQuery.trim().toLowerCase();
    return users.filter((u) => {
      const matchesQuery =
        !q ||
        u.name.toLowerCase().includes(q) ||
        u.email.toLowerCase().includes(q) ||
        u.id.toLowerCase().includes(q);
      const matchesRole =
        roleFilter === "all" ||
        (roleFilter === "admin" && u.role === "admin") ||
        (roleFilter === "member" && u.role !== "admin");
      return matchesQuery && matchesRole;
    });
  }, [users, userQuery, roleFilter]);

  const adminCount = users.filter((u) => u.role === "admin").length;
  const memberCount = users.filter((u) => u.role !== "admin").length;

  if (loading) {
    return (
      <div className="p-12 text-stone-500 flex flex-col items-center justify-center gap-3">
        <KiwiMicroButtonLoader />
        <span className="text-xs font-mono">Loading team workspace...</span>
      </div>
    );
  }

  if (loadError) {
    return (
      <div className="p-8 text-rose-600 bg-rose-50 border border-rose-200 rounded-2xl">
        Failed to load organization team details. Please check your permissions or network.
      </div>
    );
  }

  return (
    <div className="flex flex-col space-y-6 font-sans text-stone-900">
      {/* ================= HERO & HEADER SECTION ================= */}
      <div className="flex flex-wrap items-start justify-between gap-4 pb-2 border-b border-sand-200">
        <div>
          {renaming ? (
            <div className="flex items-center gap-2 mb-2">
              <input
                autoFocus
                type="text"
                value={nameDraft}
                onChange={(e) => setNameDraft(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === "Enter") handleSaveName();
                  if (e.key === "Escape") {
                    setRenaming(false);
                    setNameDraft(org.name);
                  }
                }}
                className="bg-white border border-sand-300 rounded-xl px-3 py-1 text-2xl font-bold tracking-tight focus:outline-none focus:border-stone-800 shadow-2xs"
              />
              <button
                onClick={handleSaveName}
                disabled={busy === "rename"}
                className="p-1.5 rounded-lg bg-emerald-50 text-emerald-700 hover:bg-emerald-100 border border-emerald-200 transition-colors"
                title="Save"
              >
                {busy === "rename" ? <KiwiMicroButtonLoader /> : <Check className="w-4 h-4" />}
              </button>
              <button
                onClick={() => {
                  setRenaming(false);
                  setNameDraft(org.name);
                }}
                className="p-1.5 rounded-lg bg-sand-100 text-stone-500 hover:text-stone-800 transition-colors"
                title="Cancel"
              >
                <X className="w-4 h-4" />
              </button>
            </div>
          ) : (
            <h1 className="text-2xl font-bold tracking-tight text-stone-900 flex items-center gap-2.5">
              <span>{org.name}</span>
              <button
                onClick={() => {
                  setNameDraft(org.name);
                  setRenaming(true);
                }}
                className="p-1 rounded-lg text-stone-400 hover:text-stone-700 hover:bg-sand-150 transition-colors"
                title="Rename Organization"
              >
                <Pencil className="w-3.5 h-3.5" />
              </button>
            </h1>
          )}

          {/* Org metadata chips */}
          <div className="flex items-center gap-2 mt-1.5 flex-wrap text-xs">
            <button
              onClick={handleCopyOrgId}
              className="font-mono text-[11px] text-stone-600 bg-white hover:bg-sand-100 px-2 py-0.5 rounded-md border border-sand-200 flex items-center gap-1 shadow-2xs transition-colors"
              title="Click to copy Org ID"
            >
              <span>#{org.id}</span>
              {copiedId ? <Check className="w-3 h-3 text-emerald-600" /> : <Copy className="w-3 h-3 text-stone-400" />}
            </button>

            <span className="font-mono text-[10px] font-bold uppercase bg-amber-50 text-amber-800 border border-amber-200 px-2 py-0.5 rounded-md">
              {org.plan} PLAN
            </span>

            <span className="font-mono text-[10px] font-bold uppercase bg-emerald-50 text-emerald-800 border border-emerald-200 px-2 py-0.5 rounded-md flex items-center gap-1">
              <span className="w-1.5 h-1.5 rounded-full bg-emerald-500" />
              {org.activation_state || "ACTIVE"}
            </span>

            {org.primary_domain && (
              <span className="font-mono text-[10px] text-stone-600 bg-sand-100 px-2 py-0.5 rounded-md border border-sand-200">
                @{org.primary_domain}
              </span>
            )}
          </div>
        </div>

        {/* Top Header Actions */}
        <div className="flex items-center gap-2.5">
          <button
            onClick={() => {
              setShowInviteModal(true);
              setInviteFeedback(null);
            }}
            className="px-4 py-2 rounded-xl bg-stone-900 hover:bg-stone-800 text-white font-semibold text-xs shadow-2xs flex items-center gap-1.5 transition-all active:scale-[0.98]"
          >
            <UserPlus className="w-3.5 h-3.5 text-kiwi-400" />
            <span>+ Invite Member</span>
          </button>
        </div>
      </div>

      {/* ================= 4 KPI METRIC STRIP ================= */}
      <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-3.5">
        <div className="p-4 rounded-2xl bg-white border border-sand-200 shadow-2xs flex flex-col justify-between">
          <div className="flex items-center justify-between text-xs text-stone-500 font-medium">
            <span>Total Team Roster</span>
            <Users className="w-4 h-4 text-stone-400" />
          </div>
          <div className="mt-2">
            <div className="text-2xl font-bold font-mono text-stone-900">{users.length}</div>
            <div className="text-[10px] text-stone-400 font-mono mt-0.5">
              {adminCount} admins • {memberCount} members
            </div>
          </div>
        </div>

        <div className="p-4 rounded-2xl bg-white border border-sand-200 shadow-2xs flex flex-col justify-between">
          <div className="flex items-center justify-between text-xs text-stone-500 font-medium">
            <span>Administrators</span>
            <ShieldCheck className="w-4 h-4 text-indigo-500" />
          </div>
          <div className="mt-2">
            <div className="text-2xl font-bold font-mono text-indigo-900">{adminCount}</div>
            <div className="text-[10px] text-indigo-600/80 font-mono mt-0.5">
              Role-Based Access Enforced
            </div>
          </div>
        </div>

        <div className="p-4 rounded-2xl bg-white border border-sand-200 shadow-2xs flex flex-col justify-between">
          <div className="flex items-center justify-between text-xs text-stone-500 font-medium">
            <span>Pending Join Requests</span>
            <Clock className="w-4 h-4 text-amber-500" />
          </div>
          <div className="mt-2">
            <div className="text-2xl font-bold font-mono text-stone-900">{joinRequests.length}</div>
            <div className="text-[10px] font-mono mt-0.5 text-stone-400">
              {joinRequests.length > 0 ? (
                <span className="text-amber-700 font-bold">Awaiting your approval</span>
              ) : (
                "No pending requests"
              )}
            </div>
          </div>
        </div>

        <div className="p-4 rounded-2xl bg-white border border-sand-200 shadow-2xs flex flex-col justify-between">
          <div className="flex items-center justify-between text-xs text-stone-500 font-medium">
            <span>Domain Auto-Join</span>
            <Globe className="w-4 h-4 text-emerald-500" />
          </div>
          <div className="mt-2 flex items-center justify-between">
            <div>
              <div className="text-sm font-bold font-mono text-stone-900">
                {org.domain_join ? "Active" : "Disabled"}
              </div>
              <div className="text-[10px] text-stone-400 font-mono mt-0.5 truncate max-w-[120px]">
                {org.primary_domain ? `@${org.primary_domain}` : "No domain set"}
              </div>
            </div>
            <button
              onClick={handleToggleDomainJoin}
              disabled={busy === "domain_join"}
              className={`px-2.5 py-1 rounded-lg text-[10px] font-mono font-bold border transition-colors ${
                org.domain_join
                  ? "bg-emerald-50 text-emerald-800 border-emerald-200 hover:bg-emerald-100"
                  : "bg-sand-100 text-stone-600 border-sand-200 hover:bg-sand-150"
              }`}
            >
              {busy === "domain_join" ? "Saving..." : org.domain_join ? "Turn Off" : "Turn On"}
            </button>
          </div>
        </div>
      </div>

      {/* ================= TAB NAVIGATION ================= */}
      <div className="flex items-center gap-1.5 border-b border-sand-200 pb-2 text-xs flex-wrap">
        <button
          onClick={() => setActiveTab("users")}
          className={`flex items-center gap-1.5 px-3 py-1.5 rounded-xl font-semibold transition-all ${
            activeTab === "users"
              ? "bg-stone-900 text-white shadow-2xs"
              : "text-stone-600 hover:bg-sand-150 hover:text-stone-900"
          }`}
        >
          <Users className="w-3.5 h-3.5" />
          <span>Team Roster</span>
          <span className={`text-[10px] px-1.5 py-0.2 rounded-full font-mono ${activeTab === "users" ? "bg-stone-700 text-stone-200" : "bg-sand-200 text-stone-600"}`}>
            {users.length}
          </span>
        </button>

        <button
          onClick={() => setActiveTab("access")}
          className={`flex items-center gap-1.5 px-3 py-1.5 rounded-xl font-semibold transition-all ${
            activeTab === "access"
              ? "bg-stone-900 text-white shadow-2xs"
              : "text-stone-600 hover:bg-sand-150 hover:text-stone-900"
          }`}
        >
          <ShieldCheck className="w-3.5 h-3.5" />
          <span>Access & Invites</span>
          {joinRequests.length > 0 && (
            <span className="text-[10px] px-1.5 py-0.2 rounded-full font-mono bg-amber-500 text-stone-900 font-bold">
              {joinRequests.length}
            </span>
          )}
        </button>

        <button
          onClick={() => setActiveTab("usage")}
          className={`flex items-center gap-1.5 px-3 py-1.5 rounded-xl font-semibold transition-all ${
            activeTab === "usage"
              ? "bg-stone-900 text-white shadow-2xs"
              : "text-stone-600 hover:bg-sand-150 hover:text-stone-900"
          }`}
        >
          <BarChart3 className="w-3.5 h-3.5" />
          <span>Compute & Models</span>
        </button>

        <button
          onClick={() => setActiveTab("audit")}
          className={`flex items-center gap-1.5 px-3 py-1.5 rounded-xl font-semibold transition-all ${
            activeTab === "audit"
              ? "bg-stone-900 text-white shadow-2xs"
              : "text-stone-600 hover:bg-sand-150 hover:text-stone-900"
          }`}
        >
          <Activity className="w-3.5 h-3.5" />
          <span>Audit Logs</span>
          <span className={`text-[10px] px-1.5 py-0.2 rounded-full font-mono ${activeTab === "audit" ? "bg-stone-700 text-stone-200" : "bg-sand-200 text-stone-600"}`}>
            {auditLogs.length}
          </span>
        </button>

        <button
          onClick={() => setActiveTab("provider")}
          className={`flex items-center gap-1.5 px-3 py-1.5 rounded-xl font-semibold transition-all ${
            activeTab === "provider"
              ? "bg-stone-900 text-white shadow-2xs"
              : "text-stone-600 hover:bg-sand-150 hover:text-stone-900"
          }`}
        >
          <Database className="w-3.5 h-3.5" />
          <span>BYOK Config</span>
        </button>
      </div>

      {/* ================= TAB 1: USERS / TEAM ROSTER ================= */}
      {activeTab === "users" && (
        <div className="space-y-4">
          {/* Search & Filter Toolbar */}
          <div className="flex flex-wrap items-center justify-between gap-3 bg-white p-3 rounded-2xl border border-sand-200 shadow-2xs text-xs">
            <div className="flex items-center gap-2.5 flex-1 min-w-[240px]">
              <div className="relative flex-1">
                <Search className="w-3.5 h-3.5 text-stone-400 absolute left-3 top-2.5 pointer-events-none" />
                <input
                  type="text"
                  value={userQuery}
                  onChange={(e) => setUserQuery(e.target.value)}
                  placeholder="Search members by name, email, or user ID..."
                  className="w-full bg-sand-50/70 border border-sand-200 rounded-xl pl-8 pr-3 py-1.5 text-xs placeholder:text-stone-400 focus:outline-none focus:border-stone-400 focus:bg-white transition-all font-mono"
                />
              </div>
            </div>

            <div className="flex items-center gap-2">
              <div className="flex items-center gap-1 bg-sand-100 p-0.5 rounded-xl border border-sand-200">
                <button
                  onClick={() => setRoleFilter("all")}
                  className={`px-2.5 py-1 rounded-lg text-xs font-semibold transition-all ${
                    roleFilter === "all" ? "bg-white text-stone-900 shadow-2xs" : "text-stone-600 hover:text-stone-900"
                  }`}
                >
                  All ({users.length})
                </button>
                <button
                  onClick={() => setRoleFilter("admin")}
                  className={`px-2.5 py-1 rounded-lg text-xs font-semibold transition-all ${
                    roleFilter === "admin" ? "bg-white text-stone-900 shadow-2xs" : "text-stone-600 hover:text-stone-900"
                  }`}
                >
                  Admins ({adminCount})
                </button>
                <button
                  onClick={() => setRoleFilter("member")}
                  className={`px-2.5 py-1 rounded-lg text-xs font-semibold transition-all ${
                    roleFilter === "member" ? "bg-white text-stone-900 shadow-2xs" : "text-stone-600 hover:text-stone-900"
                  }`}
                >
                  Members ({memberCount})
                </button>
              </div>

              <span className="text-stone-400 font-mono text-[11px] hidden sm:inline">
                Showing {filteredUsers.length} of {users.length}
              </span>
            </div>
          </div>

          {/* Members Table */}
          <div className="bg-white border border-sand-200 rounded-2xl shadow-2xs overflow-hidden">
            <div className="w-full overflow-x-auto no-scrollbar">
              <table className="w-full text-xs text-left border-collapse">
                <thead className="bg-sand-50/80 border-b border-sand-200 text-stone-500 font-medium">
                  <tr>
                    <th className="py-2.5 px-3.5 text-left">Team Member</th>
                    <th className="py-2.5 px-3 text-left">Role</th>
                    <th className="py-2.5 px-3 text-center hidden md:table-cell">Sign-ins</th>
                    <th className="py-2.5 px-3 text-left">Last Active</th>
                    <th className="py-2.5 px-3 text-left hidden sm:table-cell">Joined</th>
                    <th className="py-2.5 px-3.5 text-right">Credentials</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-sand-150">
                  {filteredUsers.map((u) => {
                    const initials = getAvatarInitials(u.email, u.name);
                    const colorClass = getAvatarColor(u.email || u.id);

                    return (
                      <tr key={u.id} className="hover:bg-sand-50/50 transition-colors">
                        {/* Member profile */}
                        <td className="py-2.5 px-3.5">
                          <div className="flex items-center gap-2.5">
                            <div className={`w-7 h-7 rounded-full font-bold flex items-center justify-center text-[11px] shrink-0 shadow-2xs ${colorClass}`}>
                              {initials}
                            </div>
                            <div className="min-w-0">
                              <div className="font-bold text-stone-900 truncate flex items-center gap-1.5 leading-tight">
                                <span>{u.name || "Unnamed Member"}</span>
                              </div>
                              <div className="text-[11px] text-stone-500 font-mono truncate leading-tight mt-0.5">{u.email}</div>
                            </div>
                          </div>
                        </td>

                        {/* Role */}
                        <td className="py-2.5 px-3">
                          {u.role === "admin" ? (
                            <span className="inline-flex items-center gap-1 px-2 py-0.5 rounded-full text-[10px] font-mono font-bold bg-indigo-50 text-indigo-800 border border-indigo-200">
                              <ShieldCheck className="w-3 h-3 text-indigo-600" />
                              <span>Admin</span>
                            </span>
                          ) : (
                            <span className="inline-flex items-center gap-1 px-2 py-0.5 rounded-full text-[10px] font-mono font-medium bg-sand-100 text-stone-700 border border-sand-200">
                              <User className="w-3 h-3 text-stone-500" />
                              <span>Member</span>
                            </span>
                          )}
                        </td>

                        {/* Sign-ins */}
                        <td className="py-2.5 px-3 text-center font-mono text-stone-700 hidden md:table-cell">
                          <span className="bg-sand-100 px-2 py-0.5 rounded-md border border-sand-200 font-bold text-[11px]">
                            {u.sign_in_count}
                          </span>
                        </td>

                        {/* Last active */}
                        <td className="py-2.5 px-3">
                          {u.last_seen_at ? (
                            <div className="flex items-center gap-1.5 text-stone-600" title={exactTime(u.last_seen_at)}>
                              <span className="w-1.5 h-1.5 rounded-full bg-emerald-500 shrink-0" />
                              <span className="font-mono text-[11px]">{shortTime(u.last_seen_at)}</span>
                            </div>
                          ) : (
                            <span className="text-stone-400 font-mono text-[11px]">Never</span>
                          )}
                        </td>

                        {/* Joined date */}
                        <td className="py-2.5 px-3 font-mono text-stone-500 text-[11px] hidden sm:table-cell">
                          {new Date(u.created_at).toLocaleDateString(undefined, {
                            month: "short",
                            day: "numeric",
                            year: "numeric",
                          })}
                        </td>

                        {/* Actions: Keys & Sessions */}
                        <td className="py-2.5 px-3.5 text-right">
                          <div className="flex items-center justify-end gap-1.5">
                            <button
                              onClick={() => openKeysModal(u)}
                              className="px-2 py-1 rounded-lg border border-sand-200 bg-white hover:bg-sand-100 text-stone-700 font-semibold text-[11px] shadow-2xs flex items-center gap-1 transition-all"
                              title="Manage CLI & API Keys"
                            >
                              <KeyRound className="w-3 h-3 text-stone-500" />
                              <span>Keys</span>
                            </button>

                            <button
                              onClick={() => openSessionsModal(u)}
                              className="px-2 py-1 rounded-lg border border-sand-200 bg-white hover:bg-sand-100 text-stone-700 font-semibold text-[11px] shadow-2xs flex items-center gap-1 transition-all"
                              title="View Dashboard Sessions"
                            >
                              <History className="w-3 h-3 text-stone-500" />
                              <span>Sessions</span>
                            </button>
                          </div>
                        </td>
                      </tr>
                    );
                  })}

                  {filteredUsers.length === 0 && (
                    <tr>
                      <td colSpan={6} className="py-12 text-center text-stone-400 font-mono">
                        No team members matching your search filter.
                      </td>
                    </tr>
                  )}
                </tbody>
              </table>
            </div>
          </div>
        </div>
      )}

      {/* ================= TAB 2: ACCESS & JOIN REQUESTS ================= */}
      {activeTab === "access" && (
        <div className="space-y-6">
          {/* Domain Auto-Join Banner Card */}
          <div className="p-5 rounded-2xl border border-sand-200 bg-white shadow-2xs space-y-3">
            <div className="flex items-start justify-between gap-4">
              <div className="space-y-1">
                <h3 className="text-sm font-bold text-stone-900 flex items-center gap-2">
                  <Globe className="w-4 h-4 text-emerald-600" />
                  <span>Domain Auto-Join Access</span>
                </h3>
                <p className="text-xs text-stone-500 max-w-2xl leading-relaxed">
                  {org.primary_domain
                    ? `When enabled, teammates signing in with a verified @${org.primary_domain} email address will automatically join ${org.name} without manual administrator sign-off.`
                    : "No primary domain configured for this workspace. Set up your domain in settings to unlock seamless SSO and email auto-join."}
                </p>
              </div>

              <button
                onClick={handleToggleDomainJoin}
                disabled={busy === "domain_join" || !org.primary_domain}
                className={`px-4 py-2 rounded-xl text-xs font-bold border shadow-2xs transition-all flex items-center gap-1.5 disabled:opacity-50 ${
                  org.domain_join
                    ? "bg-emerald-50 text-emerald-800 border-emerald-300 hover:bg-emerald-100"
                    : "bg-stone-900 text-white border-stone-900 hover:bg-stone-800"
                }`}
              >
                {busy === "domain_join" ? <KiwiMicroButtonLoader /> : null}
                <span>{org.domain_join ? "Disable Auto-Join" : "Enable Auto-Join"}</span>
              </button>
            </div>
          </div>

          {/* Pending Join Requests Table */}
          <div className="bg-white border border-sand-200 rounded-2xl shadow-2xs overflow-hidden">
            <div className="p-4 border-b border-sand-200 flex items-center justify-between">
              <div>
                <h3 className="text-xs font-bold text-stone-900 uppercase tracking-wider">
                  Pending Join Requests ({joinRequests.length})
                </h3>
                <p className="text-[11px] text-stone-500 mt-0.5">
                  Users who requested access to this organization and are waiting for admin approval.
                </p>
              </div>
            </div>

            <div className="w-full overflow-x-auto no-scrollbar">
              <table className="w-full text-xs text-left">
                <thead className="bg-sand-50/80 border-b border-sand-200 text-stone-500 font-medium">
                  <tr>
                    <th className="py-3 px-4">Applicant Email</th>
                    <th className="py-3 px-4">Requested On</th>
                    <th className="py-3 px-4 text-right">Decision</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-sand-150">
                  {joinRequests.map((req) => (
                    <tr key={req.id} className="hover:bg-sand-50/50 transition-colors">
                      <td className="py-3 px-4 font-medium text-stone-900 font-mono">{req.user_email}</td>
                      <td className="py-3 px-4 text-stone-500 font-mono">
                        {new Date(req.created_at).toLocaleDateString(undefined, {
                          month: "short",
                          day: "numeric",
                          year: "numeric",
                        })}
                      </td>
                      <td className="py-3 px-4 text-right">
                        <div className="flex items-center justify-end gap-2">
                          <button
                            onClick={() => handleApproveJoinRequest(req.id)}
                            disabled={!!busy}
                            className="px-3 py-1.5 rounded-lg bg-emerald-600 hover:bg-emerald-700 text-white font-bold text-xs shadow-2xs flex items-center gap-1 transition-all"
                          >
                            {busy === `approve-${req.id}` ? <KiwiMicroButtonLoader /> : <Check className="w-3.5 h-3.5" />}
                            <span>Approve</span>
                          </button>
                          <button
                            onClick={() => handleDenyJoinRequest(req.id)}
                            disabled={!!busy}
                            className="px-3 py-1.5 rounded-lg bg-white hover:bg-rose-50 border border-rose-200 text-rose-700 font-bold text-xs shadow-2xs flex items-center gap-1 transition-all"
                          >
                            {busy === `deny-${req.id}` ? <KiwiMicroButtonLoader /> : <X className="w-3.5 h-3.5" />}
                            <span>Decline</span>
                          </button>
                        </div>
                      </td>
                    </tr>
                  ))}

                  {joinRequests.length === 0 && (
                    <tr>
                      <td colSpan={3} className="py-10 text-center text-stone-400 font-mono">
                        No pending join requests at this time.
                      </td>
                    </tr>
                  )}
                </tbody>
              </table>
            </div>
          </div>
        </div>
      )}

      {/* ================= TAB 3: COMPUTE & USAGE ================= */}
      {activeTab === "usage" && (
        <div className="space-y-6">
          {modelUsage && Object.keys(modelUsage.tasks_by_status).length > 0 && (
            <div className="bg-white p-5 border border-sand-200 rounded-2xl shadow-2xs">
              <h3 className="text-xs font-bold text-stone-500 uppercase tracking-wider mb-3">
                Team Task Execution Yield
              </h3>
              <div className="flex flex-wrap gap-4">
                {Object.entries(modelUsage.tasks_by_status).map(([status, count]) => (
                  <div key={status} className="px-3.5 py-2 rounded-xl bg-sand-50 border border-sand-200 flex items-baseline gap-2">
                    <span className="text-xl font-bold font-mono text-stone-900">{count}</span>
                    <span className="text-[11px] font-mono uppercase text-stone-500 font-bold">{status}</span>
                  </div>
                ))}
              </div>
            </div>
          )}

          <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
            {/* By Provider */}
            <div className="bg-white border border-sand-200 rounded-2xl shadow-2xs overflow-hidden">
              <div className="p-4 border-b border-sand-200">
                <h3 className="text-xs font-bold text-stone-900 uppercase tracking-wider">Usage by LLM Provider</h3>
              </div>
              <div className="w-full overflow-x-auto no-scrollbar">
                <table className="w-full text-xs text-left">
                  <thead className="bg-sand-50 border-b border-sand-200 text-stone-500 font-medium">
                    <tr>
                      <th className="py-2.5 px-4">Provider</th>
                      <th className="py-2.5 px-4 text-right">Tasks</th>
                      <th className="py-2.5 px-4 text-right">Direct Cost</th>
                      <th className="py-2.5 px-4 text-right">Platform Funded</th>
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-sand-150">
                    {(modelUsage?.provider_usage ?? []).map((row) => (
                      <tr key={row.provider} className="hover:bg-sand-50/50 transition-colors">
                        <td className="py-2.5 px-4 font-bold text-stone-900">{providerLabel(row.provider)}</td>
                        <td className="py-2.5 px-4 text-right font-mono text-stone-700">{row.task_count}</td>
                        <td className="py-2.5 px-4 text-right font-mono font-bold text-stone-900">
                          ${row.cost_usd.toFixed(2)}
                        </td>
                        <td className="py-2.5 px-4 text-right font-mono text-stone-500">
                          ${row.kiwi_cost_usd.toFixed(2)}
                        </td>
                      </tr>
                    ))}
                    {(!modelUsage || modelUsage.provider_usage.length === 0) && (
                      <tr>
                        <td colSpan={4} className="py-8 text-center text-stone-400 font-mono">
                          No provider consumption recorded yet.
                        </td>
                      </tr>
                    )}
                  </tbody>
                </table>
              </div>
            </div>

            {/* By Model */}
            <div className="bg-white border border-sand-200 rounded-2xl shadow-2xs overflow-hidden">
              <div className="p-4 border-b border-sand-200">
                <h3 className="text-xs font-bold text-stone-900 uppercase tracking-wider">Usage by Model</h3>
              </div>
              <div className="w-full overflow-x-auto no-scrollbar">
                <table className="w-full text-xs text-left">
                  <thead className="bg-sand-50 border-b border-sand-200 text-stone-500 font-medium">
                    <tr>
                      <th className="py-2.5 px-4">Model</th>
                      <th className="py-2.5 px-4 text-right">Tasks</th>
                      <th className="py-2.5 px-4 text-right">Cost</th>
                      <th className="py-2.5 px-4 text-right">Token Flow</th>
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-sand-150">
                    {(modelUsage?.model_usage ?? []).map((row) => (
                      <tr key={row.model} className="hover:bg-sand-50/50 transition-colors">
                        <td className="py-2.5 px-4 font-mono font-bold text-stone-900 text-[11px]">{row.model}</td>
                        <td className="py-2.5 px-4 text-right font-mono text-stone-700">{row.task_count}</td>
                        <td className="py-2.5 px-4 text-right font-mono font-bold text-stone-900">
                          ${row.cost_usd.toFixed(2)}
                        </td>
                        <td className="py-2.5 px-4 text-right font-mono text-stone-500 text-[11px]">
                          {formatTokens(row.tokens_in)} in / {formatTokens(row.tokens_out)} out
                        </td>
                      </tr>
                    ))}
                    {(!modelUsage || modelUsage.model_usage.length === 0) && (
                      <tr>
                        <td colSpan={4} className="py-8 text-center text-stone-400 font-mono">
                          No model telemetry recorded yet.
                        </td>
                      </tr>
                    )}
                  </tbody>
                </table>
              </div>
            </div>
          </div>

          {/* By User */}
          <div className="bg-white border border-sand-200 rounded-2xl shadow-2xs overflow-hidden">
            <div className="p-4 border-b border-sand-200">
              <h3 className="text-xs font-bold text-stone-900 uppercase tracking-wider">Per-User Compute Burn</h3>
            </div>
            <div className="w-full overflow-x-auto no-scrollbar">
              <table className="w-full text-xs text-left">
                <thead className="bg-sand-50 border-b border-sand-200 text-stone-500 font-medium">
                  <tr>
                    <th className="py-2.5 px-4">Member</th>
                    <th className="py-2.5 px-4 text-right">Tasks</th>
                    <th className="py-2.5 px-4 text-right">Pass Rate</th>
                    <th className="py-2.5 px-4 text-right">Total Cost</th>
                    <th className="py-2.5 px-4 text-right">Tokens Processed</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-sand-150">
                  {(modelUsage?.per_user ?? []).map((row) => {
                    const passRate =
                      row.task_count > 0
                        ? Math.round((row.succeeded / row.task_count) * 100)
                        : 0;

                    return (
                      <tr key={row.user_id} className="hover:bg-sand-50/50 transition-colors">
                        <td className="py-2.5 px-4">
                          <div className="font-bold text-stone-900">{row.email || row.user_id}</div>
                          <div className="text-[10px] text-stone-400 font-mono">{row.user_id}</div>
                        </td>
                        <td className="py-2.5 px-4 text-right font-mono text-stone-700">{row.task_count}</td>
                        <td className="py-2.5 px-4 text-right">
                          <span className="font-mono font-bold text-emerald-700 bg-emerald-50 px-2 py-0.5 rounded border border-emerald-200">
                            {passRate}% ({row.succeeded}/{row.task_count})
                          </span>
                        </td>
                        <td className="py-2.5 px-4 text-right font-mono font-bold text-stone-900">
                          ${row.cost_usd.toFixed(2)}
                        </td>
                        <td className="py-2.5 px-4 text-right font-mono text-stone-500 text-[11px]">
                          {formatTokens(row.tokens_in + row.tokens_out)}
                        </td>
                      </tr>
                    );
                  })}
                  {(!modelUsage || modelUsage.per_user.length === 0) && (
                    <tr>
                      <td colSpan={5} className="py-8 text-center text-stone-400 font-mono">
                        No member activity recorded yet.
                      </td>
                    </tr>
                  )}
                </tbody>
              </table>
            </div>
          </div>
        </div>
      )}

      {/* ================= TAB 4: SECURITY AUDIT TRAIL ================= */}
      {activeTab === "audit" && (
        <div className="bg-white border border-sand-200 rounded-2xl shadow-2xs overflow-hidden">
          <div className="p-4 border-b border-sand-200 flex items-center justify-between">
            <div>
              <h3 className="text-xs font-bold text-stone-900 uppercase tracking-wider">
                Workspace Security Audit Trail ({auditLogs.length})
              </h3>
              <p className="text-[11px] text-stone-500 mt-0.5">
                Immutable record of administrative, role change, and access token operations.
              </p>
            </div>
          </div>

          <div className="w-full overflow-x-auto no-scrollbar">
            <table className="w-full text-xs text-left">
              <thead className="bg-sand-50/80 border-b border-sand-200 text-stone-500 font-medium">
                <tr>
                  <th className="py-3 px-4">Timestamp</th>
                  <th className="py-3 px-4">Actor</th>
                  <th className="py-3 px-4">Action</th>
                  <th className="py-3 px-4">Target Resource</th>
                  <th className="py-3 px-4">Audit Details</th>
                  <th className="py-3 px-4">IP Origin</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-sand-150">
                {auditLogs.map((log) => (
                  <tr key={log.id} className="hover:bg-sand-50/50 transition-colors">
                    <td className="py-3 px-4 text-stone-500 font-mono whitespace-nowrap">
                      {new Date(log.created_at).toLocaleString(undefined, {
                        month: "short",
                        day: "numeric",
                        hour: "numeric",
                        minute: "2-digit",
                      })}
                    </td>
                    <td className="py-3 px-4">
                      <div className="font-bold text-stone-900">{log.user_email || "System Daemon"}</div>
                      {log.user_id && <div className="text-[10px] text-stone-400 font-mono">{log.user_id}</div>}
                    </td>
                    <td className="py-3 px-4">
                      <span className="inline-flex px-2 py-0.5 rounded text-[10px] bg-sand-100 text-stone-800 font-mono font-bold border border-sand-200">
                        {log.action}
                      </span>
                    </td>
                    <td className="py-3 px-4">
                      <div className="font-mono text-stone-700">{log.resource}</div>
                      <div className="text-[10px] text-stone-400 font-mono truncate max-w-[140px]">
                        {log.resource_id}
                      </div>
                    </td>
                    <td className="py-3 px-4 text-stone-700 max-w-sm truncate">{log.details}</td>
                    <td className="py-3 px-4 text-stone-400 font-mono text-[11px]">{log.client_ip || "—"}</td>
                  </tr>
                ))}
                {auditLogs.length === 0 && (
                  <tr>
                    <td colSpan={6} className="py-12 text-center text-stone-400 font-mono">
                      No security audit events recorded yet.
                    </td>
                  </tr>
                )}
              </tbody>
            </table>
          </div>
        </div>
      )}

      {/* ================= TAB 5: BYOK PROVIDERS ================= */}
      {activeTab === "provider" && (
        <div className="bg-white border border-sand-200 rounded-2xl shadow-2xs p-6 max-w-2xl space-y-6">
          <div>
            <h3 className="text-sm font-bold text-stone-900 flex items-center gap-2">
              <Database className="w-4 h-4 text-kiwi-700" />
              <span>BYOK LLM Provider Override</span>
            </h3>
            <p className="text-xs text-stone-500 mt-1 leading-relaxed">
              Configure custom API keys and model routing for this workspace. When configured, tasks executed for this organization use your own cloud LLM billing accounts.
            </p>
          </div>

          {provSuccess && (
            <div className="p-3 rounded-xl bg-emerald-50 border border-emerald-200 text-emerald-800 text-xs flex items-center gap-2">
              <CheckCircle2 className="w-4 h-4 text-emerald-600 shrink-0" />
              <span>LLM Provider configuration saved successfully.</span>
            </div>
          )}

          <form onSubmit={handleSaveProvider} className="space-y-4 text-xs">
            <div>
              <label className="block font-bold text-stone-700 mb-1.5">Primary Provider</label>
              <select
                value={provName}
                onChange={(e) => setProvName(e.target.value)}
                className="w-full bg-sand-50/80 border border-sand-200 rounded-xl px-3 py-2 text-xs font-semibold text-stone-900 focus:outline-none focus:border-stone-800"
              >
                <option value="anthropic">Anthropic (Claude 3.5 Sonnet / Haiku)</option>
                <option value="openai">OpenAI (GPT-4o / o1)</option>
                <option value="gemini">Google Gemini (Gemini 2.0 Flash / Pro)</option>
              </select>
            </div>

            <div>
              <label className="block font-bold text-stone-700 mb-1.5">API Key Credential</label>
              <input
                type="password"
                value={provKey}
                onChange={(e) => setProvKey(e.target.value)}
                placeholder="Leave blank to keep existing encrypted credential"
                className="w-full bg-sand-50/80 border border-sand-200 rounded-xl px-3 py-2 text-xs font-mono placeholder:text-stone-400 focus:outline-none focus:border-stone-800"
              />
              <p className="text-[10px] text-stone-400 mt-1 flex items-center gap-1">
                <Lock className="w-3 h-3 text-stone-400" />
                Encrypted at rest with KMS envelope encryption.
              </p>
            </div>

            <div className="grid grid-cols-1 sm:grid-cols-2 gap-3 pt-1">
              <div>
                <label className="block font-bold text-stone-700 mb-1.5">Actor / Worker Model</label>
                <input
                  type="text"
                  value={provActor}
                  onChange={(e) => setProvActor(e.target.value)}
                  placeholder="e.g. claude-3-5-sonnet-20241022"
                  className="w-full bg-sand-50/80 border border-sand-200 rounded-xl px-3 py-2 text-xs font-mono placeholder:text-stone-400 focus:outline-none focus:border-stone-800"
                />
              </div>

              <div>
                <label className="block font-bold text-stone-700 mb-1.5">Critic / Architect Model</label>
                <input
                  type="text"
                  value={provCritic}
                  onChange={(e) => setProvCritic(e.target.value)}
                  placeholder="e.g. claude-3-5-haiku-20241022"
                  className="w-full bg-sand-50/80 border border-sand-200 rounded-xl px-3 py-2 text-xs font-mono placeholder:text-stone-400 focus:outline-none focus:border-stone-800"
                />
              </div>
            </div>

            <div className="pt-3 border-t border-sand-200 flex justify-end">
              <button
                type="submit"
                disabled={busy === "save_provider"}
                className="px-5 py-2 rounded-xl bg-stone-900 hover:bg-stone-800 text-white font-bold text-xs shadow-2xs flex items-center gap-1.5 transition-all"
              >
                {busy === "save_provider" ? <KiwiMicroButtonLoader /> : <Check className="w-3.5 h-3.5 text-kiwi-400" />}
                <span>Save Provider Settings</span>
              </button>
            </div>
          </form>
        </div>
      )}

      {/* ================= MODAL 1: INVITE MEMBER MODAL ================= */}
      {showInviteModal && (
        <div className="fixed inset-0 bg-stone-900/40 backdrop-blur-xs flex items-center justify-center p-4 z-50 animate-fadeIn">
          <div className="bg-white border border-sand-200 rounded-2xl max-w-md w-full p-6 shadow-popover space-y-4">
            <div className="flex items-center justify-between pb-3 border-b border-sand-200">
              <div className="flex items-center gap-2">
                <UserPlus className="w-4 h-4 text-kiwi-700" />
                <h3 className="text-sm font-bold text-stone-900">Invite Team Member</h3>
              </div>
              <button
                onClick={() => {
                  setShowInviteModal(false);
                  setInviteFeedback(null);
                }}
                className="p-1 text-stone-400 hover:text-stone-700 rounded-lg"
              >
                <X className="w-4 h-4" />
              </button>
            </div>

            {inviteFeedback && (
              <div
                className={`p-3 rounded-xl text-xs flex items-center gap-2 ${
                  inviteFeedback.type === "success"
                    ? "bg-emerald-50 text-emerald-800 border border-emerald-200"
                    : "bg-rose-50 text-rose-800 border border-rose-200"
                }`}
              >
                {inviteFeedback.type === "success" ? (
                  <CheckCircle2 className="w-4 h-4 text-emerald-600 shrink-0" />
                ) : (
                  <AlertCircle className="w-4 h-4 text-rose-600 shrink-0" />
                )}
                <span>{inviteFeedback.message}</span>
              </div>
            )}

            <form onSubmit={handleCreateUser} className="space-y-3.5 text-xs">
              <div>
                <label className="block font-bold text-stone-700 mb-1">Full Name</label>
                <input
                  type="text"
                  value={newName}
                  onChange={(e) => setNewName(e.target.value)}
                  placeholder="Alex Mercer"
                  required
                  className="w-full bg-sand-50 border border-sand-200 rounded-xl px-3 py-2 text-xs focus:outline-none focus:border-stone-800"
                />
              </div>

              <div>
                <label className="block font-bold text-stone-700 mb-1">Work Email</label>
                <input
                  type="email"
                  value={newEmail}
                  onChange={(e) => setNewEmail(e.target.value)}
                  placeholder="alex@example.com"
                  required
                  className="w-full bg-sand-50 border border-sand-200 rounded-xl px-3 py-2 text-xs focus:outline-none focus:border-stone-800 font-mono"
                />
              </div>

              <div>
                <label className="block font-bold text-stone-700 mb-1.5">Workspace Role</label>
                <div className="grid grid-cols-2 gap-2">
                  <button
                    type="button"
                    onClick={() => setNewRole("member")}
                    className={`p-3 rounded-xl border text-left transition-all ${
                      newRole === "member"
                        ? "bg-sand-100 border-stone-800 text-stone-900 shadow-2xs"
                        : "bg-white border-sand-200 text-stone-600 hover:bg-sand-50"
                    }`}
                  >
                    <div className="font-bold flex items-center gap-1">
                      <User className="w-3.5 h-3.5" />
                      <span>Member</span>
                    </div>
                    <div className="text-[10px] text-stone-500 mt-1">Submit tasks, review PRs, view logs.</div>
                  </button>

                  <button
                    type="button"
                    onClick={() => setNewRole("admin")}
                    className={`p-3 rounded-xl border text-left transition-all ${
                      newRole === "admin"
                        ? "bg-indigo-50/70 border-indigo-600 text-indigo-950 shadow-2xs"
                        : "bg-white border-sand-200 text-stone-600 hover:bg-sand-50"
                    }`}
                  >
                    <div className="font-bold flex items-center gap-1 text-indigo-900">
                      <ShieldCheck className="w-3.5 h-3.5 text-indigo-600" />
                      <span>Admin</span>
                    </div>
                    <div className="text-[10px] text-indigo-800/70 mt-1">Manage roster, billing, BYOK keys.</div>
                  </button>
                </div>
              </div>

              <div className="pt-3 border-t border-sand-200 flex justify-end gap-2">
                <button
                  type="button"
                  onClick={() => setShowInviteModal(false)}
                  className="px-3.5 py-2 rounded-xl text-stone-600 hover:bg-sand-100 font-medium text-xs transition-colors"
                >
                  Cancel
                </button>
                <button
                  type="submit"
                  disabled={busy === "create_user" || !newEmail.trim() || !newName.trim()}
                  className="px-4 py-2 rounded-xl bg-stone-900 hover:bg-stone-800 text-white font-bold text-xs shadow-2xs flex items-center gap-1.5 transition-all disabled:opacity-50"
                >
                  {busy === "create_user" ? <KiwiMicroButtonLoader /> : <UserPlus className="w-3.5 h-3.5 text-kiwi-400" />}
                  <span>Add to Team</span>
                </button>
              </div>
            </form>
          </div>
        </div>
      )}

      {/* ================= MODAL 2: USER API KEYS MODAL ================= */}
      {activeKeysUser && (
        <div className="fixed inset-0 bg-stone-900/40 backdrop-blur-xs flex items-center justify-center p-4 z-50 animate-fadeIn">
          <div className="bg-white border border-sand-200 rounded-2xl max-w-lg w-full p-6 shadow-popover space-y-4">
            <div className="flex items-center justify-between pb-3 border-b border-sand-200">
              <div className="flex items-center gap-2 min-w-0">
                <KeyRound className="w-4 h-4 text-amber-600 shrink-0" />
                <h3 className="text-sm font-bold text-stone-900 truncate">
                  API & CLI Keys for {activeKeysUser.name || activeKeysUser.email}
                </h3>
              </div>
              <button onClick={() => setActiveKeysUser(null)} className="p-1 text-stone-400 hover:text-stone-700 rounded-lg">
                <X className="w-4 h-4" />
              </button>
            </div>

            {/* Generated key alert */}
            {newKey && newKey.userId === activeKeysUser.id && (
              <div className="p-3.5 rounded-xl border border-amber-500/30 bg-amber-500/10 space-y-2 text-xs">
                <p className="text-amber-900 font-bold flex items-center gap-1.5">
                  <AlertCircle className="w-4 h-4 text-amber-600 shrink-0" />
                  <span>Key generated — copy it now. It will not be shown again.</span>
                </p>
                <div className="flex items-center gap-2">
                  <code className="flex-1 text-xs font-mono text-stone-900 bg-white border border-sand-300 px-2.5 py-1.5 rounded-lg break-all select-all">
                    {newKey.plaintext}
                  </code>
                  <button
                    onClick={() => copyKeyText(newKey.plaintext)}
                    className="px-3 py-1.5 rounded-lg bg-stone-900 text-white font-bold text-xs flex items-center gap-1 shrink-0"
                  >
                    {copiedKey ? <Check className="w-3 h-3 text-kiwi-400" /> : <Copy className="w-3 h-3" />}
                    <span>{copiedKey ? "Copied" : "Copy"}</span>
                  </button>
                </div>
              </div>
            )}

            {/* Create new key toggle / input */}
            <div className="flex items-center justify-between">
              <span className="text-xs font-bold text-stone-500 uppercase tracking-wider">
                Active Tokens ({(keysByUser[activeKeysUser.id] ?? []).length})
              </span>
              {!showAddKeyInput ? (
                <button
                  onClick={() => setShowAddKeyInput(true)}
                  className="px-2.5 py-1 rounded-lg bg-sand-100 hover:bg-sand-200 text-stone-800 font-bold text-xs border border-sand-200 flex items-center gap-1 transition-all"
                >
                  <Plus className="w-3.5 h-3.5" />
                  <span>New Key</span>
                </button>
              ) : (
                <div className="flex items-center gap-2">
                  <input
                    type="text"
                    value={keyLabelInput}
                    onChange={(e) => setKeyLabelInput(e.target.value)}
                    placeholder="Key label (e.g. cli-laptop)"
                    className="bg-sand-50 border border-sand-300 rounded-lg px-2 py-1 text-xs font-mono focus:outline-none focus:border-stone-800"
                  />
                  <button
                    onClick={() => handleGenerateKey(activeKeysUser.id)}
                    disabled={busy === `genkey-${activeKeysUser.id}`}
                    className="px-2.5 py-1 rounded-lg bg-stone-900 text-white font-bold text-xs shadow-2xs"
                  >
                    {busy === `genkey-${activeKeysUser.id}` ? <KiwiMicroButtonLoader /> : "Create"}
                  </button>
                  <button
                    onClick={() => setShowAddKeyInput(false)}
                    className="p-1 text-stone-400 hover:text-stone-700"
                  >
                    <X className="w-3.5 h-3.5" />
                  </button>
                </div>
              )}
            </div>

            {/* Keys Table */}
            <div className="bg-sand-50/50 border border-sand-200 rounded-xl overflow-hidden text-xs">
              {keysLoading === activeKeysUser.id ? (
                <div className="p-6 text-center text-stone-400 font-mono">Loading API keys...</div>
              ) : (
                <table className="w-full text-left">
                  <thead className="border-b border-sand-200 text-stone-500 font-medium bg-sand-100/50">
                    <tr>
                      <th className="py-2 px-3">Label</th>
                      <th className="py-2 px-3">Created</th>
                      <th className="py-2 px-3">Expires</th>
                      <th className="py-2 px-3 text-right">Action</th>
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-sand-150">
                    {(keysByUser[activeKeysUser.id] ?? []).map((k) => (
                      <tr key={k.id} className="hover:bg-white transition-colors">
                        <td className="py-2 px-3 font-mono font-bold text-stone-800">{k.label || "default"}</td>
                        <td className="py-2 px-3 font-mono text-stone-500 text-[11px]">
                          {new Date(k.created_at).toLocaleDateString()}
                        </td>
                        <td className="py-2 px-3 font-mono text-stone-500 text-[11px]">
                          {k.expires_at ? new Date(k.expires_at).toLocaleDateString() : "Never"}
                        </td>
                        <td className="py-2 px-3 text-right">
                          <button
                            onClick={() => handleRevokeKey(activeKeysUser.id, k.id)}
                            disabled={busy === `revoke-${k.id}`}
                            className="text-rose-600 hover:text-rose-800 font-bold hover:underline"
                          >
                            {busy === `revoke-${k.id}` ? <KiwiMicroButtonLoader /> : "Revoke"}
                          </button>
                        </td>
                      </tr>
                    ))}
                    {(keysByUser[activeKeysUser.id] ?? []).length === 0 && (
                      <tr>
                        <td colSpan={4} className="py-6 text-center text-stone-400 font-mono">
                          No active API keys for this user.
                        </td>
                      </tr>
                    )}
                  </tbody>
                </table>
              )}
            </div>

            <div className="pt-2 flex justify-end">
              <button
                onClick={() => setActiveKeysUser(null)}
                className="px-4 py-1.5 rounded-xl bg-sand-100 hover:bg-sand-200 text-stone-800 font-semibold text-xs transition-colors"
              >
                Close
              </button>
            </div>
          </div>
        </div>
      )}

      {/* ================= MODAL 3: USER SESSIONS MODAL ================= */}
      {activeSessionsUser && (
        <div className="fixed inset-0 bg-stone-900/40 backdrop-blur-xs flex items-center justify-center p-4 z-50 animate-fadeIn">
          <div className="bg-white border border-sand-200 rounded-2xl max-w-lg w-full p-6 shadow-popover space-y-4">
            <div className="flex items-center justify-between pb-3 border-b border-sand-200">
              <div className="flex items-center gap-2 min-w-0">
                <History className="w-4 h-4 text-indigo-600 shrink-0" />
                <h3 className="text-sm font-bold text-stone-900 truncate">
                  Dashboard Sessions for {activeSessionsUser.name || activeSessionsUser.email}
                </h3>
              </div>
              <button onClick={() => setActiveSessionsUser(null)} className="p-1 text-stone-400 hover:text-stone-700 rounded-lg">
                <X className="w-4 h-4" />
              </button>
            </div>

            {/* Sessions Table */}
            <div className="bg-sand-50/50 border border-sand-200 rounded-xl overflow-hidden text-xs">
              {sessionsLoading === activeSessionsUser.id ? (
                <div className="p-6 text-center text-stone-400 font-mono">Loading dashboard sessions...</div>
              ) : (
                <table className="w-full text-left">
                  <thead className="border-b border-sand-200 text-stone-500 font-medium bg-sand-100/50">
                    <tr>
                      <th className="py-2 px-3">Session Start</th>
                      <th className="py-2 px-3">Last Activity</th>
                      <th className="py-2 px-3 text-right">Duration</th>
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-sand-150">
                    {(sessionsByUser[activeSessionsUser.id] ?? []).map((s) => (
                      <tr key={s.id} className="hover:bg-white transition-colors">
                        <td className="py-2 px-3 font-mono text-stone-800 text-[11px]" title={exactTime(s.started_at)}>
                          {shortTime(s.started_at)}
                        </td>
                        <td className="py-2 px-3 font-mono text-stone-600 text-[11px]" title={exactTime(s.last_activity_at)}>
                          {shortTime(s.last_activity_at)}
                        </td>
                        <td className="py-2 px-3 text-right font-mono text-stone-700 text-[11px]">
                          {formatDuration(s.duration_seconds * 1000)}
                        </td>
                      </tr>
                    ))}
                    {(sessionsByUser[activeSessionsUser.id] ?? []).length === 0 && (
                      <tr>
                        <td colSpan={3} className="py-6 text-center text-stone-400 font-mono">
                          No active or recorded sessions for this user.
                        </td>
                      </tr>
                    )}
                  </tbody>
                </table>
              )}
            </div>

            <div className="pt-2 flex justify-end">
              <button
                onClick={() => setActiveSessionsUser(null)}
                className="px-4 py-1.5 rounded-xl bg-sand-100 hover:bg-sand-200 text-stone-800 font-semibold text-xs transition-colors"
              >
                Close
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
