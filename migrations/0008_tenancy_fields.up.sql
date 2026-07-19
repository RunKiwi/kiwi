-- Tenancy fields for self-serve signup (RFC: Self-Serve Signup & Tenancy).
-- IF NOT EXISTS matches the repo convention (see 0006/0007) and keeps this
-- idempotent if the columns were already created by GORM AutoMigrate.
-- NOTE: existing orgs default to activation_state='inactive'. When the S4
-- activation gate lands, seed pre-existing orgs (incl. the `system` bootstrap
-- org used by `make local`/CLI) to 'active', or current usage will be blocked.
ALTER TABLE organizations ADD COLUMN IF NOT EXISTS type TEXT NOT NULL DEFAULT 'personal';
ALTER TABLE organizations ADD COLUMN IF NOT EXISTS primary_domain TEXT NOT NULL DEFAULT '';
ALTER TABLE organizations ADD COLUMN IF NOT EXISTS domain_join BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE organizations ADD COLUMN IF NOT EXISTS plan TEXT NOT NULL DEFAULT 'free';
ALTER TABLE organizations ADD COLUMN IF NOT EXISTS activation_state TEXT NOT NULL DEFAULT 'inactive';

ALTER TABLE users ADD COLUMN IF NOT EXISTS oauth_provider TEXT;
ALTER TABLE users ADD COLUMN IF NOT EXISTS oauth_subject TEXT;
CREATE UNIQUE INDEX IF NOT EXISTS idx_users_oauth ON users (oauth_provider, oauth_subject);
