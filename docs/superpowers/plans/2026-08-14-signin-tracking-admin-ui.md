# Sign-in Tracking Admin UI Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Surface the sign-in/dashboard-activity data from PR #354 (currently API-only) in the existing superadmin Users table, with a session-history drill-down.

**Architecture:** Two files change: `frontend/src/lib/api.ts` gets three new fields on `AdminUser` plus a new `AdminDashboardSession` type and `listAdminUserSessions` client method (both mirroring existing sibling patterns exactly); `frontend/src/components/OrgManagementPanel.tsx` gets two new table columns and a second expandable per-user panel ("Sessions"), mirroring the existing "Keys" panel's state/fetch/render pattern exactly.

**Tech Stack:** Next.js/React/TypeScript, Tailwind (no new dependencies — see design spec's Non-goals).

## Global Constraints

- No new third-party dependency. Reuse `frontend/src/lib/datetime.ts`'s `shortTime`, `exactTime`, `formatDuration` — already built for exactly this, already the convention in this codebase's list views.
- No backend changes — `sign_in_count`/`last_sign_in_at`/`last_seen_at` already ride along in `GET /admin/orgs/{orgID}/users`'s existing JSON response; `GET /admin/orgs/{orgID}/users/{userID}/sessions` already exists (PR #354).
- No new automated test files — this frontend has no component-test convention (`frontend/src/components/` contains zero `.test.tsx` files; the only tests are pure-function tests under `lib/`/`hooks/`), and `api.ts` itself has no existing test file despite ~30 client methods. Verification is `npm run lint`, `npm run build` (TypeScript compiles), and manual dev-server check.
- `last_seen_at` only in the table — no `last_sign_in_at` column (see spec's Non-goals: `last_seen_at` is the more current signal; the table is already six columns wide).

---

## Task 1: API client plumbing

**Files:**
- Modify: `frontend/src/lib/api.ts:435-442` (extend `AdminUser`)
- Modify: `frontend/src/lib/api.ts:472-473` (add `AdminDashboardSession`, after `AdminAPIKey`)
- Modify: `frontend/src/lib/api.ts:598` (add `listAdminUserSessions`, after `listAdminUserAPIKeys`)

**Interfaces:**
- Produces: `AdminUser.sign_in_count: number`, `AdminUser.last_sign_in_at: string | null`, `AdminUser.last_seen_at: string | null`; `interface AdminDashboardSession { id: string; user_id: string; org_id: string; started_at: string; last_activity_at: string; duration_seconds: number }`; `client.listAdminUserSessions(orgId: string, userId: string): Promise<AdminDashboardSession[]>` — all consumed by Task 2.

- [ ] **Step 1: Extend `AdminUser` with the three new fields**

In `frontend/src/lib/api.ts`, `AdminUser` currently reads (lines 435-442):

```ts
export interface AdminUser {
  id: string;
  email: string;
  name: string;
  org_id: string;
  role: string;
  created_at: string;
}
```

Change to:

```ts
export interface AdminUser {
  id: string;
  email: string;
  name: string;
  org_id: string;
  role: string;
  created_at: string;
  sign_in_count: number;
  last_sign_in_at: string | null;
  last_seen_at: string | null;
}
```

These are always present in the JSON (the Go struct has no `omitempty` on any of the three), so none are optional (`?`) — `last_sign_in_at`/`last_seen_at` are `null` rather than absent when unset.

- [ ] **Step 2: Add the `AdminDashboardSession` type**

In `frontend/src/lib/api.ts`, `AdminAPIKey` and `AdminAPIKeyCreated` currently read (lines 465-481):

```ts
export interface AdminAPIKey {
  id: string;
  user_id: string;
  label: string;
  created_at: string;
  expires_at?: string;
  revoked_at?: string;
}

export interface AdminAPIKeyCreated {
  key_id: string;
  key: string;
  label: string;
  user_id: string;
  created_at: string;
  expires_at: string | null;
}
```

Add a new type directly after `AdminAPIKeyCreated` (before `AdminUserUsageRow`):

```ts
export interface AdminDashboardSession {
  id: string;
  user_id: string;
  org_id: string;
  started_at: string;
  last_activity_at: string;
  duration_seconds: number;
}
```

- [ ] **Step 3: Add the `listAdminUserSessions` client method**

In `frontend/src/lib/api.ts`, the admin methods block currently has (line 598):

```ts
  listAdminUserAPIKeys: (orgId: string, userId: string) => fetchApi<AdminAPIKey[]>(`/admin/orgs/${orgId}/users/${userId}/keys`),
```

Add a new method directly after it:

```ts
  listAdminUserAPIKeys: (orgId: string, userId: string) => fetchApi<AdminAPIKey[]>(`/admin/orgs/${orgId}/users/${userId}/keys`),
  listAdminUserSessions: (orgId: string, userId: string) => fetchApi<AdminDashboardSession[]>(`/admin/orgs/${orgId}/users/${userId}/sessions`),
```

- [ ] **Step 4: Verify the file compiles and lints**

Run (from `frontend/`): `npx tsc --noEmit` and `npm run lint`
Expected: both clean. `AdminUser`'s three new fields and `AdminDashboardSession` are unused at this point (Task 2 consumes them) — TypeScript does not flag unused exported types/interface fields, only unused local variables/imports, so this is expected to pass clean, not a false negative to explain away.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/lib/api.ts
git commit -m "feat(admin-ui): add sign-in/session API types and client method"
```

---

## Task 2: Users table columns and Sessions drill-down

**Files:**
- Modify: `frontend/src/components/OrgManagementPanel.tsx`

**Interfaces:**
- Consumes: `AdminUser.sign_in_count`/`last_sign_in_at`/`last_seen_at`, `AdminDashboardSession`, `client.listAdminUserSessions` (Task 1); `shortTime(iso: string, now?: Date): string`, `exactTime(iso: string): string`, `formatDuration(ms: number): string` (existing, `frontend/src/lib/datetime.ts`).
- Produces: nothing consumed elsewhere — this is the terminal task.

- [ ] **Step 1: Import the datetime helpers**

In `frontend/src/components/OrgManagementPanel.tsx`, the import block currently reads (lines 1-5):

```tsx
"use client";

import { useEffect, useState, Fragment } from "react";
import { client, type AdminOrg, type AdminUser, type AdminAuditLog, type AdminProviderConfig, type AdminOrgModelUsage, type AdminAPIKey, type AdminJoinRequest, formatTokens, providerLabel } from "@/lib/api";
import { Loader2, Users, Activity, Settings, Database, Plus, BarChart3, KeyRound, Pencil, Check, X, ShieldCheck } from "lucide-react";
```

Change to:

```tsx
"use client";

import { useEffect, useState, Fragment } from "react";
import { client, type AdminOrg, type AdminUser, type AdminAuditLog, type AdminProviderConfig, type AdminOrgModelUsage, type AdminAPIKey, type AdminDashboardSession, type AdminJoinRequest, formatTokens, providerLabel } from "@/lib/api";
import { shortTime, exactTime, formatDuration } from "@/lib/datetime";
import { Loader2, Users, Activity, Settings, Database, Plus, BarChart3, KeyRound, Pencil, Check, X, ShieldCheck, History } from "lucide-react";
```

(`History` is added to the `lucide-react` import for the new "Sessions" button's icon, matching how "Keys" uses `KeyRound`.)

- [ ] **Step 2: Add Sessions expand state, mirroring the existing Keys state exactly**

In `frontend/src/components/OrgManagementPanel.tsx`, the Keys state currently reads (lines 22-27):

```tsx
  // API keys, expanded per user
  const [expandedUserId, setExpandedUserId] = useState<string | null>(null);
  const [keysByUser, setKeysByUser] = useState<Record<string, AdminAPIKey[]>>({});
  const [keysLoading, setKeysLoading] = useState<string | null>(null);
  const [newKey, setNewKey] = useState<{ userId: string; plaintext: string } | null>(null);
  const [copied, setCopied] = useState(false);
```

Add new state directly after it:

```tsx
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
```

- [ ] **Step 3: Add `toggleSessions`, mirroring `toggleKeys` exactly**

In `frontend/src/components/OrgManagementPanel.tsx`, `toggleKeys` currently reads (lines 86-97):

```tsx
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
```

Add a new function directly after it:

```tsx
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
```

(No `setNewKey(null)`-equivalent line: that call in `toggleKeys` clears the "just generated a key, show it once" banner state, which has no Sessions equivalent — session history has nothing analogous to a freshly-minted secret to display once.)

- [ ] **Step 4: Add the two new table header columns**

In `frontend/src/components/OrgManagementPanel.tsx`, the table header currently reads (lines 332-340):

```tsx
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
```

Change to:

```tsx
              <table className="w-full text-sm text-left">
                <thead className="bg-white/5 border-b border-white/10 text-xs font-medium text-zinc-400">
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
```

- [ ] **Step 5: Add the two new data cells to each user row**

In `frontend/src/components/OrgManagementPanel.tsx`, the user row currently reads (lines 345-362):

```tsx
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
```

Change to:

```tsx
                      <tr className="hover:bg-white/[0.02] transition-colors">
                        <td className="px-4 py-3 font-medium">{user.name}</td>
                        <td className="px-4 py-3 text-zinc-300">{user.email}</td>
                        <td className="px-4 py-3">
                          <span className={`inline-flex px-2 py-0.5 rounded text-xs ${user.role === 'admin' ? 'bg-indigo-500/10 text-indigo-400' : 'bg-white/10 text-zinc-300'}`}>
                            {user.role}
                          </span>
                        </td>
                        <td className="px-4 py-3 text-zinc-400">{new Date(user.created_at).toLocaleDateString()}</td>
                        <td className="px-4 py-3 text-zinc-300">{user.sign_in_count}</td>
                        <td className="px-4 py-3 text-zinc-400">
                          {user.last_seen_at ? (
                            <span title={exactTime(user.last_seen_at)}>{shortTime(user.last_seen_at)}</span>
                          ) : (
                            <span className="text-zinc-600">Never</span>
                          )}
                        </td>
                        <td className="px-4 py-3 text-right">
                          <div className="flex items-center justify-end gap-2">
                            <button
                              onClick={() => toggleKeys(user.id)}
                              className="inline-flex items-center gap-1 text-xs bg-white/5 hover:bg-white/10 border border-white/10 rounded px-2 py-1 transition-colors"
                            >
                              <KeyRound className="w-3 h-3" /> Keys
                            </button>
                            <button
                              onClick={() => toggleSessions(user.id)}
                              className="inline-flex items-center gap-1 text-xs bg-white/5 hover:bg-white/10 border border-white/10 rounded px-2 py-1 transition-colors"
                            >
                              <History className="w-3 h-3" /> Sessions
                            </button>
                          </div>
                        </td>
                      </tr>
```

- [ ] **Step 6: Add the Sessions expanded panel, mirroring the Keys panel exactly**

In `frontend/src/components/OrgManagementPanel.tsx`, the Keys expanded panel currently ends and the row-mapping closes like this (lines 363-440 — shown here from the closing of the Keys `<tr>` conditional through the end of the `users.map`):

```tsx
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
```

Two changes here:

1. The Keys panel's `colSpan={5}` must become `colSpan={7}` (the table is now 7 columns: Name/Email/Role/Joined/Sign-ins/Last seen/Details, not 5).
2. A new, independent Sessions panel `<tr>` is added directly after the Keys panel's closing `)}` and before `</Fragment>`. The bottom `"No users found."` row's `colSpan={5}` must also become `colSpan={7}`.

Full replacement:

```tsx
                      {expandedUserId === user.id && (
                        <tr>
                          <td colSpan={7} className="px-4 py-4 bg-black/20">
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
                      {expandedSessionsUserId === user.id && (
                        <tr>
                          <td colSpan={7} className="px-4 py-4 bg-black/20">
                            <h3 className="text-xs font-bold text-zinc-500 uppercase tracking-widest mb-3">Dashboard Sessions for {user.email}</h3>

                            {sessionsLoading === user.id ? (
                              <div className="text-xs text-zinc-500">Loading sessions…</div>
                            ) : (
                              <div className="overflow-x-auto">
                              <table className="w-full text-xs text-left">
                                <thead className="text-zinc-500">
                                  <tr>
                                    <th className="py-1 pr-4 font-medium">Started</th>
                                    <th className="py-1 pr-4 font-medium">Last Activity</th>
                                    <th className="py-1 font-medium">Duration</th>
                                  </tr>
                                </thead>
                                <tbody className="divide-y divide-white/5">
                                  {(sessionsByUser[user.id] ?? []).map(session => (
                                    <tr key={session.id}>
                                      <td className="py-1.5 pr-4 text-zinc-300" title={exactTime(session.started_at)}>{shortTime(session.started_at)}</td>
                                      <td className="py-1.5 pr-4 text-zinc-300" title={exactTime(session.last_activity_at)}>{shortTime(session.last_activity_at)}</td>
                                      <td className="py-1.5 text-zinc-300">{formatDuration(session.duration_seconds * 1000)}</td>
                                    </tr>
                                  ))}
                                  {(sessionsByUser[user.id] ?? []).length === 0 && (
                                    <tr>
                                      <td colSpan={3} className="py-2 text-zinc-500">No dashboard sessions recorded yet.</td>
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
                      <td colSpan={7} className="px-4 py-8 text-center text-zinc-500">No users found.</td>
                    </tr>
                  )}
                </tbody>
              </table>
              </div>
            </div>
```

- [ ] **Step 7: Verify the file compiles and lints**

Run (from `frontend/`): `npx tsc --noEmit` and `npm run lint`
Expected: both clean.

- [ ] **Step 8: Run the full frontend test suite for regressions**

Run (from `frontend/`): `npm test`
Expected: PASS, same 150 tests as the baseline (this task doesn't touch anything under `lib/`/`hooks/` that has test coverage — this run is a regression guard, not new coverage).

- [ ] **Step 9: Manual verification**

Start the dev server (`npm run dev` from `frontend/`) and confirm, against a running Control Plane with at least one org that has a user with `sign_in_count > 0` and one with `sign_in_count === 0`:
1. The Users table shows "Sign-ins" and "Last seen" columns with correct values (a `0`/"Never" user renders "Never" in muted gray; a user with activity shows a `shortTime`-formatted value, and hovering it shows the exact timestamp via the browser's native title tooltip).
2. Clicking "Sessions" expands a panel below that row showing session history (or "No dashboard sessions recorded yet." if none), with correctly formatted durations (e.g. "5m", "1h 4m").
3. Keys and Sessions panels toggle independently — opening one does not close the other, and each closes independently when its own button is clicked again.
4. The existing Keys functionality (view, generate, revoke) still works unchanged.

If a full Control Plane + superadmin login isn't available in this environment, say so explicitly rather than claiming this step passed — do not report Task 2 complete without either running this check or explicitly noting it was skipped and why.

- [ ] **Step 10: Commit**

```bash
git add frontend/src/components/OrgManagementPanel.tsx
git commit -m "feat(admin-ui): show sign-in counts, last-seen, and session history in the Users table"
```
