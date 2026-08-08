-- Per-org, per-tier, per-month token allowances for work run on Kiwi-owned keys.
--
-- tokens_granted = -1 means unlimited. 0 means NO allowance, which is a real
-- and common state: the Free plan grants no frontier tokens at all. This
-- deliberately differs from org_limits, where 0 means unlimited — reusing that
-- overloaded sentinel here would turn "zero frontier tokens" into "unlimited
-- frontier tokens", which is the expensive direction of the mistake.
CREATE TABLE IF NOT EXISTS org_token_grants (
    org_id         TEXT        NOT NULL,
    tier           TEXT        NOT NULL,
    -- UTC calendar month, YYYY-MM. UTC so the rollover instant is the same for
    -- every deployment and no org can collect two allowances at a boundary.
    period         TEXT        NOT NULL,
    tokens_granted BIGINT      NOT NULL DEFAULT 0,
    tokens_used    BIGINT      NOT NULL DEFAULT 0,
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (org_id, tier, period)
);

CREATE INDEX IF NOT EXISTS idx_org_token_grants_period ON org_token_grants (org_id, period);
