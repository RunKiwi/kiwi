-- migrations/0037_postmerge_monitors.up.sql
--
-- Post-Merge Verification (Phase 1a): tracks a merged, Kiwi-authored PR from
-- merge through a fixed monitoring window, closed out by GitHub-native
-- signals alone (revert PR, failed check run, or a clean window) — Phase 1a
-- has no telemetry integration (see kiwi-internal/specs/2026-08-15-postmerge-
-- verification-design.md). One row per (org_id, job_id): a job merges once.
CREATE TABLE IF NOT EXISTS postmerge_monitors (
    id                   TEXT PRIMARY KEY,
    org_id               TEXT NOT NULL,
    job_id               TEXT NOT NULL,
    repo                 TEXT NOT NULL,
    pr_number            INTEGER NOT NULL,
    merge_commit_sha     TEXT NOT NULL,
    status               TEXT NOT NULL DEFAULT 'MONITORING',
    verdict_evidence     TEXT NOT NULL DEFAULT '',
    remediation_task_id  TEXT,
    deployed_at          TIMESTAMPTZ NOT NULL,
    window_ends_at       TIMESTAMPTZ NOT NULL,
    finalized_at         TIMESTAMPTZ,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_postmerge_monitors_org_job
    ON postmerge_monitors (org_id, job_id);
CREATE INDEX IF NOT EXISTS idx_postmerge_monitors_merge_commit
    ON postmerge_monitors (merge_commit_sha);
CREATE INDEX IF NOT EXISTS idx_postmerge_monitors_status_window
    ON postmerge_monitors (status, window_ends_at);
