CREATE TABLE slack_installations (
    team_id TEXT PRIMARY KEY,
    org_id TEXT NOT NULL,
    team_name TEXT NOT NULL DEFAULT '',
    installed_by_user_id TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_slack_installations_org ON slack_installations (org_id);

CREATE TABLE slack_channel_bindings (
    id TEXT PRIMARY KEY,
    org_id TEXT NOT NULL,
    team_id TEXT NOT NULL,
    channel_id TEXT NOT NULL,
    repo_url TEXT NOT NULL,
    default_test_cmd TEXT NOT NULL DEFAULT '',
    default_ref TEXT NOT NULL DEFAULT '',
    created_by TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX idx_slack_channel_bindings_channel ON slack_channel_bindings (team_id, channel_id);
CREATE INDEX idx_slack_channel_bindings_org ON slack_channel_bindings (org_id);

CREATE TABLE slack_triggered_tasks (
    id TEXT PRIMARY KEY,
    org_id TEXT NOT NULL,
    team_id TEXT NOT NULL,
    channel_id TEXT NOT NULL,
    thread_ts TEXT NOT NULL,
    parent_task_id TEXT,
    queued_task_id TEXT NOT NULL DEFAULT '',
    status_message_ts TEXT NOT NULL DEFAULT '',
    last_status TEXT NOT NULL DEFAULT '',
    investigation_only BOOLEAN NOT NULL DEFAULT false,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_slack_triggered_tasks_thread ON slack_triggered_tasks (team_id, channel_id, thread_ts, created_at DESC);
