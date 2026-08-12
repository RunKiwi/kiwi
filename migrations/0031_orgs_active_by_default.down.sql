-- Restore the previous default.
--
-- The UPDATE is deliberately not reversed. Which orgs were 'inactive' before it
-- ran is not recoverable from this table, and guessing would be worse than
-- leaving them active: 'inactive' gated nothing, so an org wrongly left active
-- behaves exactly as it did, while an org wrongly moved back to 'inactive'
-- silently loses the ability to be auto-suspended for abuse.
ALTER TABLE organizations ALTER COLUMN activation_state SET DEFAULT 'inactive';
