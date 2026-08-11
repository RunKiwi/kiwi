-- Links a Kiwi org to a GitHub App installation.
--
-- Replaces the personal access token as the way Kiwi reaches a customer's
-- repositories. A PAT is a standing credential with whatever scope the user
-- happened to grant, usually org-wide write, sitting in this database until
-- somebody rotates it. An installation covers only the repositories the
-- customer ticked, buys tokens that expire within the hour, and can be revoked
-- from their own settings without telling us.
--
-- GIT_TOKEN is not dropped. Non-GitHub remotes still need it and existing orgs
-- keep working untouched; the App is preferred when an installation exists.
CREATE TABLE IF NOT EXISTS github_installations (
    -- GitHub's own id, and what the token-mint endpoint addresses.
    installation_id BIGINT PRIMARY KEY,

    org_id TEXT NOT NULL,

    -- The user or organisation the App is installed on, lower-cased. GitHub
    -- treats logins case-insensitively; a btree index does not, so folding at
    -- write time is what makes the lookup reliable.
    account_login TEXT NOT NULL,

    -- GitHub's "all" or "selected", for display only. Whether a token may touch
    -- a given repository is GitHub's decision, enforced when the token is
    -- minted; duplicating that answer here would go stale silently.
    repo_selection TEXT NOT NULL DEFAULT 'selected',

    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- The hot path: resolve a repository's owner to the installation that covers
-- it, for one org. org_id leads because it is the tenancy boundary, and every
-- lookup supplies it.
CREATE INDEX IF NOT EXISTS idx_github_installations_org_account
    ON github_installations (org_id, account_login);

-- One GitHub account links to one Kiwi org at a time. Without this, two orgs
-- could each claim the same account and a task in either would mint tokens
-- against the other's repositories.
CREATE UNIQUE INDEX IF NOT EXISTS idx_github_installations_account
    ON github_installations (account_login);
