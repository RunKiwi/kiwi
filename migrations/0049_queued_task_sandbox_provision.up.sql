-- Sandbox cold-start telemetry on queued_tasks.
--
-- SandboxProvisionMs is how long the task's sandbox container took to start,
-- measured independently of the task's own duration or outcome (see
-- sandbox.Session.ProvisionMs). SandboxImage is the docker image it ran;
-- its own prefix ("golang:", "node:", "python:", ...) doubles as ecosystem
-- attribution so nothing has to separately enumerate ecosystems.
ALTER TABLE queued_tasks ADD COLUMN IF NOT EXISTS sandbox_provision_ms BIGINT NOT NULL DEFAULT 0;
ALTER TABLE queued_tasks ADD COLUMN IF NOT EXISTS sandbox_image TEXT NOT NULL DEFAULT '';
