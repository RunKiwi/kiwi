-- Raise the Free per-task wall-clock cap from 10 minutes to 20.
--
-- The code default lives in FreeLimits (pkg/auth/limits.go) and applies only to
-- orgs created after it changes. Every existing Free org has an org_limits row
-- written at signup with 600, and the enforcing read does NOT go through
-- auth.GetOrgLimits: the Control Plane queries the table directly when it hands
-- out work (handleDaemonHeartbeat in pkg/orchestrator/daemon_api.go) and stamps
-- the value onto the worker spec. Its read-time repair only fires on <= 0, so a
-- row holding 600 keeps 600 forever. Without this UPDATE the change reaches
-- nobody who has already signed up.
--
-- 600 is unambiguous: it is written by FreeLimits and by nothing else
-- (DefaultLimits is 1800), so matching on the value identifies exactly the rows
-- that took the Free profile.

UPDATE org_limits
SET task_timeout_seconds = 1200
WHERE task_timeout_seconds = 600
  AND org_id IN (SELECT id FROM organizations WHERE plan = 'free' OR plan = '');

-- The same value on a PAID org is a bug being repaired, not a cap being raised.
--
-- UpdateOrgPlanAndLimits (pkg/auth/billing_webhook.go) writes concurrency and
-- budget on a plan change but never task_timeout_seconds, so an org that signed
-- up Free and upgraded kept the Free cap indefinitely. Those rows should hold
-- what DefaultLimits says for a paid plan, which is 1800 — not the new Free
-- value. Fixing the data here does not fix the webhook; see the backlog.

UPDATE org_limits
SET task_timeout_seconds = 1800
WHERE task_timeout_seconds = 600
  AND org_id IN (SELECT id FROM organizations WHERE plan NOT IN ('free', ''));
