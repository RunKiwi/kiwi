CREATE TABLE plan_submissions (
    org_id VARCHAR(255) NOT NULL,
    idempotency_key VARCHAR(255) NOT NULL,
    job_id VARCHAR(255) NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (org_id, idempotency_key)
);
