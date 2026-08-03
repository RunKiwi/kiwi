-- Deliberately a no-op, for the same reason as 0020.
--
-- Two of these three columns predate this migration in every deployed database
-- (AutoMigrate created them), so dropping them here would remove state this
-- migration never added — and `created_at` in particular is load-bearing for
-- reading a run's timeline. The index is cheap enough to leave.
SELECT 1;
