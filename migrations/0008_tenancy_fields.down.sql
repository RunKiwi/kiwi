DROP INDEX IF EXISTS idx_users_oauth;
ALTER TABLE users DROP COLUMN IF EXISTS oauth_provider;
ALTER TABLE users DROP COLUMN IF EXISTS oauth_subject;
ALTER TABLE organizations DROP COLUMN IF EXISTS activation_state;
ALTER TABLE organizations DROP COLUMN IF EXISTS plan;
ALTER TABLE organizations DROP COLUMN IF EXISTS domain_join;
ALTER TABLE organizations DROP COLUMN IF EXISTS primary_domain;
ALTER TABLE organizations DROP COLUMN IF EXISTS type;
