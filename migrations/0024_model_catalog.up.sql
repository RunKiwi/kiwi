-- The model catalog is the authority for model -> provider routing, replacing
-- prefix inference. org_id = '' holds a provider's public list; a non-empty
-- org_id holds models discovered against that org's own key (fine-tunes,
-- tier-gated previews) which other orgs cannot call.
--
-- '' rather than NULL so the primary key works without COALESCE and so a
-- duplicate is a real conflict rather than Postgres treating NULLs as distinct.
CREATE TABLE IF NOT EXISTS model_catalog (
    org_id            TEXT        NOT NULL DEFAULT '',
    model_id          TEXT        NOT NULL,
    provider          TEXT        NOT NULL,
    display_name      TEXT        NOT NULL DEFAULT '',
    -- Nullable: an unknown price is not a free price. A NULL here makes the
    -- model tier 'unknown', which is never funded by Kiwi.
    input_cost_per_m  NUMERIC,
    output_cost_per_m NUMERIC,
    context_length    INTEGER,
    supports_tools    BOOLEAN,
    modality          TEXT        NOT NULL DEFAULT '',
    tier              TEXT        NOT NULL DEFAULT 'unknown',
    kiwi_provided     BOOLEAN     NOT NULL DEFAULT FALSE,
    selectable        BOOLEAN     NOT NULL DEFAULT FALSE,
    source            TEXT        NOT NULL DEFAULT 'discovered',
    first_seen_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_seen_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    -- Set when a successful refresh no longer lists the model. Rows are never
    -- deleted: historical spend and execution records join to them.
    missing_since     TIMESTAMPTZ,
    PRIMARY KEY (org_id, model_id)
);

CREATE INDEX IF NOT EXISTS idx_model_catalog_provider ON model_catalog (provider);
CREATE INDEX IF NOT EXISTS idx_model_catalog_selectable ON model_catalog (org_id, selectable);
