-- Dropping these would break any daemon still reporting progress against them,
-- and they hold no data worth protecting — a run's authoritative history lives
-- in task_events. Deliberately a no-op.
SELECT 1;
