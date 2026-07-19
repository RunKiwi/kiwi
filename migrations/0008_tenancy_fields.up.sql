ALTER TABLE organizations ADD COLUMN type TEXT NOT NULL DEFAULT 'personal';
ALTER TABLE organizations ADD COLUMN primary_domain TEXT NOT NULL DEFAULT '';
ALTER TABLE organizations ADD COLUMN domain_join BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE organizations ADD COLUMN plan TEXT NOT NULL DEFAULT 'free';
ALTER TABLE organizations ADD COLUMN activation_state TEXT NOT NULL DEFAULT 'inactive';

ALTER TABLE users ADD COLUMN oauth_provider TEXT;
ALTER TABLE users ADD COLUMN oauth_subject TEXT;
CREATE UNIQUE INDEX idx_users_oauth ON users (oauth_provider, oauth_subject);
