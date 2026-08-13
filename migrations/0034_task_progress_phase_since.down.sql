-- Dropping this would break any daemon still reporting progress against it,
-- and it holds no data worth protecting — a run's authoritative history
-- lives in task_events. Deliberately a no-op, matching 0020's down migration.
SELECT 1;
