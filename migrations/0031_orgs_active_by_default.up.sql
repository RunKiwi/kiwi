-- Organizations are active on creation, and existing "inactive" rows become active.
--
-- "inactive" described a gate that does not exist. Nothing in the run path has
-- ever consulted it: the submit handler gates on 'suspended' specifically (and
-- says so in a comment, because blocking non-active orgs would lock out every
-- free org), ensureFreeDaemon enqueues provisioning without looking, and the
-- provisioner claims any pending request regardless of the org's state. An
-- operator clicking "Activate" in the admin UI changed the label and nothing
-- else.
--
-- Leaving the rows alone is not an option, because the state was not merely
-- decorative. SuspendOrg treated 'inactive' as already-suspended and returned
-- early, so an abusive org that no operator had activated could not be
-- auto-suspended and no daemon reclaim was enqueued — disarming abuse control
-- for exactly the population most likely to need it, fresh signups nobody has
-- touched. Moving these rows to 'active' re-arms it for them.
--
-- 'suspended' is untouched: it is the state that does gate the run path.
UPDATE organizations SET activation_state = 'active' WHERE activation_state = 'inactive';

ALTER TABLE organizations ALTER COLUMN activation_state SET DEFAULT 'active';
