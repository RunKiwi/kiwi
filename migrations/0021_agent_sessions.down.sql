-- Dropping these loses the only record of how a session reached its diff, and
-- any session running at the time becomes unresumable — it would restart from
-- the base commit on its next lease rather than from its last finished round.
--
-- Unlike 0020's columns, though, these tables carry data nothing else holds, so
-- this is a real down migration rather than a no-op: a rollback that leaves
-- orphaned tables behind is worse than one that removes what it added. Events
-- go first despite ON DELETE CASCADE, so the order is explicit rather than
-- inherited from a constraint.
DROP TABLE IF EXISTS agent_session_events;
DROP TABLE IF EXISTS agent_sessions;
