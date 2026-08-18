-- telemetry_metrics rows are hand-provisioned (no dashboard CRUD exists for
-- this table yet), and both of these columns fail silently when typo'd: an
-- unknown provider is dropped by the poll-enqueue path's registry lookup, and
-- an unknown comparison_direction falls through to lower-is-better semantics,
-- which can invert a verdict. The provider list mirrors pkg/telemetry's
-- registry; the direction list mirrors pkg/store's Comparison* constants.
-- Safe as a plain ADD CONSTRAINT: telemetry_metrics is created in this same
-- unmerged branch (migrations/0039), so no deployed database has rows yet.
ALTER TABLE telemetry_metrics
    ADD CONSTRAINT telemetry_metrics_provider_check
        CHECK (provider IN ('prometheus', 'datadog')),
    ADD CONSTRAINT telemetry_metrics_comparison_direction_check
        CHECK (comparison_direction IN ('higher_is_better', 'lower_is_better'));
