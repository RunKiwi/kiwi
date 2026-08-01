-- Durable state for the agentic session loop (pkg/session).
--
-- A session is a task-long conversation between an Architect that plans and
-- reviews and an Implementer that does the work in rounds. That is not a
-- disposable unit of work, and the lease queue is built for disposable units:
-- "a crashed daemon's work returns to the queue" only helps if the work can be
-- redone from durable inputs.
--
-- Two things make it durable, and neither is a provider transcript. The diff
-- lives in git, on the job branch, pushed as rounds complete. Everything else
-- lives here: where the session got to, what the Architect last asked for, and
-- an append-only log of what happened. The Architect's context is RECONSTRUCTED
-- from that log on every call rather than stored as provider messages — Kiwi
-- routes three providers, a retry may land on a different one, and a persisted
-- Anthropic message array would be useless to Gemini. It also means the same
-- rows that let a session resume are the ones that let pkg/ver explain it.
--
-- Guarded like 0020, and for the same reason: queued_tasks exists only through
-- AutoMigrate (see the schema drift note in CLAUDE.md §1), so the migrate role
-- may run against a database no serving process has touched. These tables do
-- not depend on it, but the FK from events to sessions does depend on sessions
-- existing, so the whole block is written to be safely re-runnable.

CREATE TABLE IF NOT EXISTS agent_sessions (
    id              TEXT PRIMARY KEY,
    org_id          TEXT NOT NULL,
    job_id          TEXT NOT NULL DEFAULT '',
    task_id         TEXT NOT NULL,
    repo_url        TEXT NOT NULL DEFAULT '',
    branch          TEXT NOT NULL DEFAULT '',
    -- base_sha is the commit the session started from and never changes. Every
    -- diff the reviewer sees is taken against it, so the review is always of the
    -- whole task rather than the latest round's slice.
    base_sha        TEXT NOT NULL DEFAULT '',
    -- head_sha is the last committed round. A resumed session resets to it and
    -- discards whatever the crashed round had half-done.
    head_sha        TEXT NOT NULL DEFAULT '',
    phase           TEXT NOT NULL DEFAULT 'planning',
    round           INT  NOT NULL DEFAULT 0,
    -- round_attempts counts how many times the CURRENT round has been started.
    -- A round that kills its daemon twice is a poison pill: bounded here rather
    -- than left to MaxLeaseAttempts, which would spend five leases learning it.
    round_attempts  INT  NOT NULL DEFAULT 0,
    max_rounds      INT  NOT NULL DEFAULT 4,
    rejections      INT  NOT NULL DEFAULT 0,
    architect_model TEXT NOT NULL DEFAULT '',
    worker_model    TEXT NOT NULL DEFAULT '',
    -- state carries the parts of the runner's position that have no column of
    -- their own: the current spec, the round history, the stall fingerprints.
    -- It is deliberately opaque to SQL — nothing queries inside it, and giving
    -- each field a column would freeze pkg/session's internals into the schema.
    state           JSONB,
    cost_usd        DOUBLE PRECISION NOT NULL DEFAULT 0,
    tokens_in       BIGINT NOT NULL DEFAULT 0,
    tokens_out      BIGINT NOT NULL DEFAULT 0,
    status          TEXT NOT NULL DEFAULT 'RUNNING',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- One session per task: the task is what the queue leases, so this is what
-- makes "resume the session this lease is for" a lookup rather than a guess.
CREATE UNIQUE INDEX IF NOT EXISTS idx_agent_sessions_task ON agent_sessions (task_id);
CREATE INDEX IF NOT EXISTS idx_agent_sessions_org_job ON agent_sessions (org_id, job_id);

CREATE TABLE IF NOT EXISTS agent_session_events (
    id         BIGSERIAL PRIMARY KEY,
    session_id TEXT NOT NULL REFERENCES agent_sessions(id) ON DELETE CASCADE,
    org_id     TEXT NOT NULL,
    round      INT  NOT NULL DEFAULT 0,
    seq        INT  NOT NULL,
    kind       TEXT NOT NULL,
    outcome    TEXT NOT NULL DEFAULT '',
    tool       TEXT NOT NULL DEFAULT '',
    -- detail is truncated by the daemon before it is sent. Tool output can carry
    -- secrets, so what lands here is a bounded tail, never a full transcript.
    detail      TEXT NOT NULL DEFAULT '',
    duration_ms BIGINT NOT NULL DEFAULT 0,
    tokens_in   BIGINT NOT NULL DEFAULT 0,
    tokens_out  BIGINT NOT NULL DEFAULT 0,
    cost_usd    DOUBLE PRECISION NOT NULL DEFAULT 0,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- (session, round, seq) is unique so a retried checkpoint POST cannot duplicate
-- a round's events. The daemon numbers them; the database enforces it.
CREATE UNIQUE INDEX IF NOT EXISTS idx_ase_seq ON agent_session_events (session_id, round, seq);
CREATE INDEX IF NOT EXISTS idx_ase_org ON agent_session_events (org_id, session_id);
