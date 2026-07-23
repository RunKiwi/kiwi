CREATE EXTENSION IF NOT EXISTS vector;

CREATE TABLE IF NOT EXISTS job_learnings (
    id TEXT PRIMARY KEY,
    job_id TEXT NOT NULL,
    org_id TEXT NOT NULL,
    repo TEXT NOT NULL,
    task TEXT NOT NULL,
    summary TEXT NOT NULL,
    pr_url TEXT,
    outcome TEXT,
    embedding vector(768),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- One learning per job: the indexer upserts on job_id, so it must be unique.
CREATE UNIQUE INDEX IF NOT EXISTS job_learnings_job_id_key ON job_learnings (job_id);
CREATE INDEX IF NOT EXISTS job_learnings_org_id_idx ON job_learnings (org_id);
CREATE INDEX IF NOT EXISTS job_learnings_embedding_idx ON job_learnings USING hnsw (embedding vector_cosine_ops);
