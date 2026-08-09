# Org-admin self-service admin actions

Date: 2026-08-09
Status: Approved (design), pending implementation plan

## Problem

`ee/auth`'s `/admin/orgs/{orgID}/...` API tree is the only surface for managing
an org: users, API keys, audit logs, LLM provider config, and (unused today)
domain-join / join-request approval. Every route is gated by
`isAdminAuthorized`, which recognizes only a global super-admin allowlist
(`KIWI_SUPER_ADMIN_EMAILS`) or the bootstrap `KIWI_SERVER_TOKEN` — it never
checks the org-scoped `User.Role == "admin"` a user gets from creating or
being promoted into an org. So an org's own admin (e.g. the first person to
sign up under a company domain) cannot view their own org's audit log,
approve a teammate's join request, or toggle whether their domain auto-joins
— every one of those requires a Kiwi operator to act on their behalf via a
raw authenticated API call. There's also no frontend UI for join requests or
domain-join at all, for anyone, super-admin included.

This spec scopes a self-service path: an org-scoped admin gets these actions
for their own org, without touching the actions that are genuinely
operator-only (billing/lifecycle).

## Scope

**Becomes self-service (org-admin, own org only):**
- Users: create, list
- API keys: create, list, revoke
- Audit logs: view
- Usage/spend stats: view — backend authorization only (see Frontend design
  note on why no new UI is needed for this one)
- Provider config (LLM provider/models/key override): view, edit — stays
  org-wide (one `OrgProviderConfig` row per org, as today); no per-user
  override is introduced
- Join requests: list, approve, deny
- Domain-join: toggle
- Organization name: rename (new endpoint — see Backend design)

**Stays super-admin-only:**
- Create organization
- List all organizations
- Activate / suspend organization
- Change plan
- Grant agent minutes
- `/admin/stats`

Billing/lifecycle and abuse-control actions are deliberately excluded —
these are decisions Kiwi (the operator) keeps, not the org.

## Backend design

### Authorization

Add one helper to `ee/auth/admin.go`, next to the existing
`isAdminAuthorized`:

```go
// authorizeOrgAccess grants access to super-admins (via isAdminAuthorized)
// or to an org-scoped admin acting on their own org.
func authorizeOrgAccess(r *http.Request, orgID string) bool {
	if isAdminAuthorized(r) {
		return true
	}
	claims := ClaimsFromContext(r.Context())
	return claims != nil && claims.IsAdmin() && claims.OrgID == orgID
}
```

In the `/admin/orgs/` route switch in `AdminRouter`, change the gate from
`isAdminAuthorized(r)` to `authorizeOrgAccess(r, orgID)` for exactly these
eight route groups (identified by the existing `parts` path-parsing):
- `users` (`len(parts) == 2 && parts[1] == "users"`)
- `users/{userID}/keys[/{keyID}]` (the two 4- and 5-part cases)
- `audit`
- `usage`
- `provider`
- `join_requests[/{reqID}/approve|deny]`
- `domain_join`
- `name` (new — see below)

Note: today `isAdminAuthorized` is checked once, at the top of the
`/admin/orgs/` handler, before the path is even parsed into `parts`. Making
this per-route-group means that blanket check has to move: the path parsing
(`path := strings.TrimPrefix(...)`; `parts := strings.Split(...)`) happens
first, unguarded, and each `case` in the switch calls the appropriate check
(`isAdminAuthorized(r)` or `authorizeOrgAccess(r, orgID)`) as its own first
line, using the `orgID := parts[0]` each case already establishes.

All other cases in that switch (and the top-level `/admin/orgs` and
`/admin/stats` handlers) keep `isAdminAuthorized(r)` unchanged.

### New endpoint: rename organization

`PUT /admin/orgs/{orgID}/name`, matching the existing `domain_join` route's
"replace a single field" convention (`case len(parts) == 2 && parts[1] ==
"name"`). New handler `handleUpdateOrgName(db, w, r, orgID)`:

```go
var body struct {
    Name string `json:"name"`
}
// decode; 400 if body.Name == "" after TrimSpace
err := db.Model(&Organization{}).Where("id = ?", orgID).Update("name", body.Name).Error
// UNIQUE constraint violation -> 409 "Organization name already exists"
//   (Organization.Name has a uniqueIndex — same conflict handling as
//   handleCreateOrg already does)
```

Audit-logged the same way as the other mutating handlers
(`LogAuditEvent(db, r, "UPDATE", "ORG_NAME", orgID, ...)`). Gated by
`authorizeOrgAccess` like the rest of this batch — renaming isn't a
billing/lifecycle decision, so there's no reason to reserve it for
super-admins.

### Required hardening (bundled into this change, not optional)

`handleCreateAPIKey`, `handleListAPIKeys`, and `handleRevokeAPIKey` currently
take `userID` (and `keyID`) from the URL and operate on them directly,
without checking that the target user (or the key's owning user) actually
belongs to the `orgID` earlier in the same path. Today this is harmless
because only a super-admin — who can already touch any org — can reach these
routes. Once `authorizeOrgAccess` lets an org-scoped admin in, it becomes a
cross-org escalation: an admin of org A could pass their own `orgID` in the
path (satisfying the gate) but supply a `userID`/`keyID` belonging to org B,
and mint or revoke an API key for a user in a different org.

Fix: in all three handlers, look up the target user (directly for
create/list, or via the key's `UserID` for revoke) and confirm
`user.OrgID == orgID` before proceeding; return `404` (matching the existing
"not found" style for missing rows) if it doesn't match.

Every other affected handler (`handleListUsers`, `handleOrgAuditLogsAdmin`,
`handleSaveProviderConfig`/`handleGetProviderConfig`,
`handleApproveJoinRequest`/`handleDenyJoinRequest`, `handleToggleDomainJoin`)
already scopes its query directly by `orgID`/`org_id`, so no equivalent gap
exists there.

### New endpoints

Everything except organization rename maps onto an existing handler — that
one action needs the new route above. Otherwise this change is
authorization plus the one hardening fix.

## Frontend design

No new UI for `GET /admin/orgs/{orgID}/usage`: it has zero frontend
consumers today (not even the super-admin org-detail page calls it), and the
self-service equivalent already exists — `/api/v1/spend`, which any org
member (including admins) already uses today via the existing `/spend` page
for their own org. Relaxing its authorization is included for consistency
and future use (e.g. scripted access), but building a redundant "Usage" tab
in `OrgManagementPanel` is out of scope here.

0. **`/auth/validate` gains two fields.** The self-service page can't call
   `listAdminOrgs` (stays super-admin-only — it lists every org). It's the
   only org-scoped admin route that already loads the full `Organization`
   row, so add `domain_join` and `primary_domain` to its JSON response
   (`ee/auth/admin.go`, the `/auth/validate` handler already has `org`
   loaded) — otherwise the self-service Access tab has no way to know the
   current domain-join state to seed its toggle.

1. **Extract `OrgManagementPanel`** (`frontend/src/components/OrgManagementPanel.tsx`)
   from the existing super-admin org-detail page
   (`frontend/src/app/(dashboard)/admin/orgs/[orgId]/page.tsx`). Takes
   `org: AdminOrg` and `onOrgUpdate: (org: AdminOrg) => void` as props rather
   than fetching org metadata itself — the two call sites obtain that
   metadata differently (super-admin via `listAdminOrgs` + find; self-service
   via the extended `/auth/validate`), and the panel shouldn't need to know
   which. Keeps the existing three tabs (Users, Provider Config, Audit Logs)
   and adds a fourth tab, **Access**: domain-join toggle plus the list of
   pending join requests with approve/deny actions. This tab is net-new for
   everyone — no join-request/domain-join UI exists today at all. The
   panel's header (currently a static `org.name` — see
   `admin/orgs/[orgId]/page.tsx:129`) becomes an inline-editable field: a
   pencil icon next to the name opens a text input, saved via the new
   `renameOrg` call, which calls `onOrgUpdate` with the response so the
   parent's state — and the header — reflect the new name. Available
   regardless of which tab is active, since it's not tab-specific content.

2. **New self-service page** at `/team`
   (`frontend/src/app/(dashboard)/team/page.tsx`). On mount, calls
   `client.validate()`; if the returned `role !== "admin"` and the user is
   not a super-admin, redirects to `/` (mirrors the existing guard pattern in
   `admin/page.tsx`, which does the equivalent for `is_super_admin`; real
   enforcement is server-side either way). Otherwise builds an `AdminOrg`
   object from the validate response (`{id: org_id, name: org_name, plan,
   activation_state, domain_join, primary_domain}`) and renders
   `<OrgManagementPanel org={org} onOrgUpdate={setOrg} />` — no org picker,
   an org-scoped admin only ever sees their own org.

3. **Super-admin page becomes a thin wrapper**: `admin/orgs/[orgId]/page.tsx`
   keeps its existing `listAdminOrgs`-based fetch for `org` and renders
   `<OrgManagementPanel org={org} onOrgUpdate={setOrg} />`, so the new Access
   tab is available there too.

4. **Nav item**: `frontend/src/app/(dashboard)/layout.tsx` gains an
   `isOrgAdmin` state, set from `client.validate().role === "admin"`
   (alongside the existing `isSuperAdmin` from `client.getUsage()`), and
   pushes a `"Team"` nav entry → `/team` when `isOrgAdmin || isSuperAdmin`.
   The existing `"Admin"` entry (→ `/admin`, the all-orgs browser) stays
   super-admin-only, unchanged.

5. **New `client.*` functions** in `frontend/src/lib/api.ts`:
   `listJoinRequests(orgId)`, `approveJoinRequest(orgId, reqId)`,
   `denyJoinRequest(orgId, reqId)`, `setDomainJoin(orgId, enabled)`,
   `renameOrg(orgId, name)`. `AdminOrg` gains `domain_join: boolean` and
   `primary_domain: string` (the backend `Organization` JSON already has
   both; the frontend type just didn't declare them). `ValidateResponse`
   gains `role: string`, `domain_join: boolean`, `primary_domain: string`.

## Data flow

`/team` loads → `client.validate()` returns `{org_id, role, org_name, plan,
activation_state, domain_join, primary_domain}` → guard check → an `AdminOrg`
is built from that response and passed to `OrgManagementPanel`, which fires
`client.listAdminOrgUsers`, `getAdminOrgAuditLogs`, `getAdminOrgProviderConfig`,
plus the new join-request/domain-join/rename calls, all against
`/admin/orgs/{org_id}/...` → backend `authorizeOrgAccess` passes because
`claims.OrgID == org_id`.

## Error handling

- A `member` (non-admin) hitting any of the eight routes, even for their own
  org → `403` (unchanged from today's behavior for everyone but a
  super-admin).
- An org-admin whose `orgID` in the URL doesn't match `claims.OrgID` → `403`
  via `authorizeOrgAccess`.
- An org-admin whose `orgID` matches but whose target `userID`/`keyID`
  belongs to a different org → `404` via the hardening fix above.
- Rename to a name already used by another org → `409`, matching
  `handleCreateOrg`'s existing UNIQUE-conflict handling.
- Rename with an empty/whitespace-only name → `400`.
- Existing per-handler validation (org exists, user exists, request still
  pending, etc.) is unchanged.

## Testing

- `ee/auth/admin_test.go`: extend with — org-admin succeeds on own org for
  each of the eight route groups; org-admin gets `403` acting on a different
  org; member gets `403` even on their own org; super-admin still passes on
  any org (regression).
- `ee/auth/admin_test.go`: new case proving the hardening fix — an org-admin
  for org A cannot create/list/revoke an API key for a user belonging to org
  B, even with org A's ID correctly in the URL path.
- `ee/auth/admin_test.go`: new cases for rename — success, duplicate-name
  conflict (`409`), empty-name rejection (`400`).
- Frontend: no existing test suite covers `admin/` today; verification is
  manual, via an org-admin session and a super-admin session, using the
  `run` skill.

## Out of scope

- Per-user provider config overrides (provider config stays org-wide, as
  today).
- Any change to billing/lifecycle actions (create org, plan, grants,
  activate/suspend) — these remain super-admin-only.
- A general-purpose RBAC/permissions system — rejected as YAGNI for a
  two-tier (member/admin) plus global-super-admin model.
