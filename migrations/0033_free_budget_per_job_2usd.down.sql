-- Restore the $0.50 Free cap.
--
-- Only Free rows are reverted, and only from the exact value this migration
-- set. There is no paid-org counterpart to undo, since the up migration did
-- not touch paid orgs.
--
-- Note this cannot distinguish an org that was always at 2.00 from one this
-- migration moved there — 2.00 is not a value any other code path writes
-- today, so the match is exact in practice.

UPDATE org_limits
SET max_budget_per_job = 0.50
WHERE max_budget_per_job = 2.00
  AND org_id IN (SELECT id FROM organizations WHERE plan = 'free' OR plan = '');
