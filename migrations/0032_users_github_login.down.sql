DROP INDEX IF EXISTS idx_users_github_login;
ALTER TABLE users DROP COLUMN IF EXISTS github_login;
