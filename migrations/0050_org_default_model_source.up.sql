-- Per-org preference for which payer defaultWorkerModelFor/architectModelFor
-- reach for when nothing else (an explicit submit, a Slack channel binding)
-- names a model: 'kiwi' (the existing behaviour — prefer a Kiwi-funded
-- catalog model) or 'byok' (skip the Kiwi-funded cascade, go straight to the
-- org's own key defaults). See ee/planner/architect_model.go.
ALTER TABLE organizations ADD COLUMN IF NOT EXISTS default_model_source TEXT NOT NULL DEFAULT 'kiwi';
