-- Restore the 10-minute Free cap.
--
-- Only the Free rows are reverted, and only from the exact value this migration
-- set. The paid-org repair in the up migration is deliberately NOT undone:
-- 1800 is what those rows should have held all along, so putting 600 back would
-- reintroduce a bug rather than restore a prior state.
--
-- Note this cannot distinguish an org that was always at 1200 from one this
-- migration moved there — 1200 is not a value any other code path writes today,
-- so the match is exact in practice.

UPDATE org_limits
SET task_timeout_seconds = 600
WHERE task_timeout_seconds = 1200
  AND org_id IN (SELECT id FROM organizations WHERE plan = 'free' OR plan = '');
