-- Raise the Free per-task dollar cap from $0.50 to $2.00.
--
-- The code default lives in FreeLimits (ee/auth/limits.go) and applies only
-- to orgs created after it changes. Every existing Free org has an
-- org_limits row written at signup with 0.50 (ee/auth/domains.go,
-- ee/auth/admin.go, ee/auth/oauth.go all call tx.Create(FreeLimits(...))),
-- and GetOrgLimits's read-time repair only fires on <= 0, so a row holding
-- 0.50 keeps 0.50 forever. Without this UPDATE the change reaches nobody who
-- has already signed up — the same gap migration 0023 closed for
-- task_timeout_seconds.
--
-- 0.50 is unambiguous: it is written by FreeLimits and by nothing else
-- (DefaultLimits and pkg/store's own fallback both use 5.00), so matching on
-- the value identifies exactly the rows that took the Free profile.
--
-- No paid-org repair needed here, unlike 0023: UpdateOrgPlanAndLimits
-- (ee/auth/billing_webhook.go) already writes max_budget_per_job on every
-- plan change, so an org that upgraded from Free was never left stuck at the
-- old Free value the way task_timeout_seconds was.

UPDATE org_limits
SET max_budget_per_job = 2.00
WHERE max_budget_per_job = 0.50
  AND org_id IN (SELECT id FROM organizations WHERE plan = 'free' OR plan = '');
