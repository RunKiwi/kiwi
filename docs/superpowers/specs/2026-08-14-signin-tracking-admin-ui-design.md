# Sign-in tracking in the superadmin UI

Status: approved for implementation
Date: 2026-08-14

## Problem

PR #354 added sign-in/dashboard-activity tracking (`sign_in_count`,
`last_sign_in_at`, `last_seen_at` on `users`; sessionized `dashboard_sessions`
history) but shipped API-only, by design — the brainstorm for that PR
explicitly scoped out a UI. The data is real but invisible: nobody can see it
without hand-crafting an authenticated `curl` against `/admin/orgs/{orgID}/users`.
This closes that gap in the one place it was always meant to surface: the
existing superadmin `/admin` UI.

## Goals

- Make `sign_in_count` and `last_seen_at` visible at a glance in the existing
  per-org Users table (`frontend/src/components/OrgManagementPanel.tsx`).
- Let a superadmin drill into a user's session history (start, last activity,
  computed duration) from that same table, on demand.
- Reuse existing formatting/plumbing conventions rather than introducing new
  ones — this is a small, additive change to an existing screen, not a new
  surface.

## Non-goals

- **No top-level `/admin` (org-list) changes.** Confirmed with the user:
  this lives entirely inside the per-org Users tab, not as a cross-org
  "most recently active" view on the org-list page.
- **No `last_sign_in_at` column.** `last_seen_at` is the more current,
  more useful-at-a-glance signal (per the original design's own reasoning —
  one OAuth login's cookie/API key can span days of `last_seen_at` updates),
  and the table is already six columns wide. `last_sign_in_at` stays
  reachable via the API for anyone who needs the distinction.
- **No new third-party dependency.** Checked `frontend/package.json`:
  no date-formatting or table/grid library is currently installed. The
  existing `frontend/src/lib/datetime.ts` (`shortTime`, `exactTime`,
  `formatDuration`, added in PR #352) already does exactly what this needs,
  is already the convention in this codebase's list views, and adding a
  library like `date-fns` here would mean *not* reusing that — the
  opposite of less code. The table itself stays a hand-rolled `<table>` +
  Tailwind, matching the rest of this file; nothing here is complex enough
  to justify a data-grid library.

## Design

### Table columns

Two new columns in the Users table, inserted between the existing "Joined"
and action columns:

- **Sign-ins** — `user.sign_in_count` rendered as a plain number.
- **Last seen** — `shortTime(user.last_seen_at)` if set (with the exact
  timestamp as a `title` attribute via `exactTime()`, so hovering shows the
  precise moment), or `"Never"` in muted gray (`text-zinc-600`, matching the
  existing muted-state convention already used for "No active keys.") if
  `last_seen_at` is `null`.

### Details column (renamed from "API Keys")

The existing right-aligned action column — currently a single "Keys" button
— is renamed **"Details"** and gets a second button, **"Sessions"**, styled
identically to the existing "Keys" button. Both buttons toggle independent
expand state:

```ts
// Existing (unchanged):
const [expandedUserId, setExpandedUserId] = useState<string | null>(null);
const [keysByUser, setKeysByUser] = useState<Record<string, AdminAPIKey[]>>({});
const [keysLoading, setKeysLoading] = useState<string | null>(null);

// New, structurally identical, independent of the above:
const [expandedSessionsUserId, setExpandedSessionsUserId] = useState<string | null>(null);
const [sessionsByUser, setSessionsByUser] = useState<Record<string, AdminDashboardSession[]>>({});
const [sessionsLoading, setSessionsLoading] = useState<string | null>(null);
```

Independent rather than sharing one "which panel is open" slot: Keys and
Sessions answer unrelated questions about the same user, and the existing
Keys toggle already behaves as a fully self-contained unit (`toggleKeys`
touches only `expandedUserId`/`keysByUser`/`keysLoading`/`newKey`). Giving
Sessions its own identically-shaped state means a superadmin can have Keys
open, Sessions open, both, or neither for a given user — and it means
copying `toggleKeys`'s logic almost verbatim for `toggleSessions`, rather
than threading a second concern through the existing toggle.

### Sessions panel

Clicking "Sessions" lazily fetches (once per user, then cached, exactly like
`toggleKeys`) `client.listAdminUserSessions(org.id, user.id)` and renders a
sub-table in a new expanded `<tr>`, styled identically to the existing Keys
sub-table:

| Started | Last Activity | Duration |
|---|---|---|

- Started / Last Activity: `shortTime(session.started_at)` /
  `shortTime(session.last_activity_at)`.
- Duration: `formatDuration(session.duration_seconds * 1000)` — the API
  returns seconds as a float, `formatDuration` takes milliseconds.

No action column — session history is read-only, nothing to revoke or edit.
Empty state (no rows): `"No dashboard sessions recorded yet."`, styled like
the existing `"No active keys."` empty-state row (`text-zinc-500`,
`colSpan={3}` — three columns here, not the Keys sub-table's four).

### Implementation detail: the main table's own empty-state colSpan

The Users table's `"No users found."` row is currently `colSpan={5}`
(Name/Email/Role/Joined/API Keys). With Sign-ins and Last seen added, the
table is 7 columns wide — that row's `colSpan` must become `7`, or the
empty state renders misaligned. Easy to miss since it's a few dozen lines
below the header row being edited.

### API client plumbing (`frontend/src/lib/api.ts`)

`AdminUser` gains the three fields that already ride along in the existing
endpoint's JSON (no backend change needed):

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

A new type and client method, mirroring `AdminAPIKey` /
`listAdminUserAPIKeys` exactly:

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
```ts
listAdminUserSessions: (orgId: string, userId: string) =>
  fetchApi<AdminDashboardSession[]>(`/admin/orgs/${orgId}/users/${userId}/sessions`),
```

### Testing

This codebase's frontend has no existing test suite pattern for React
components in `components/` (no `.test.tsx` files alongside them — the only
frontend test found, `progressTime.test.ts`, covers a pure utility function,
not a component). Consistent with that existing convention, this change adds
no new test files; verification is manual, per the `run` skill: start the
dev server, log in as a superadmin, open an org's Users tab, and confirm the
new columns render correctly for a user with activity and one without
(`last_seen_at: null` → "Never"), and that the Sessions panel opens, fetches,
and displays session rows (or the empty state) correctly.
