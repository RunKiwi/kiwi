-- Backfill origin='slack' for tasks that predate the Slack origin-stamping
-- fix (ee/orchestrator/slack_trigger.go, slack_thread_reply.go). Before that
-- fix, every Slack-triggered SubmitPlan/SubmitContinuation call left Origin
-- empty, so BeforeCreate (pkg/store/lineage.go) defaulted it to 'submit' —
-- indistinguishable from a task submitted directly, so the frontend's Slack
-- badges/filters/icons never render for them even though slack_triggered_tasks
-- already proves they came from Slack.
--
-- Restricted to origin='submit' so a fork of a Slack-originated task (which
-- also gets a slack_triggered_tasks row, but intentionally keeps
-- origin='fork' — see the OriginSlack doc comment) is left untouched.
UPDATE queued_tasks
SET origin = 'slack'
WHERE origin = 'submit'
  AND id IN (SELECT queued_task_id FROM slack_triggered_tasks WHERE queued_task_id != '');
