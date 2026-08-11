-- What a review comment on a Kiwi pull request does, per organisation:
-- off | mention | any.
--
-- Default mention rather than any: on "any", a comment saying "thanks, looks
-- good" spends a round of the org's allowance, and a new org should not
-- discover that by being billed for it.
ALTER TABLE organizations ADD COLUMN IF NOT EXISTS pr_comment_mode TEXT NOT NULL DEFAULT 'mention';
