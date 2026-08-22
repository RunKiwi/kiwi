package store

import "testing"

func TestUpsertSlackInstallationThenGetByTeamID(t *testing.T) {
	s := newTestStore(t)
	ctx := t.Context()
	err := s.UpsertSlackInstallation(ctx, &SlackInstallation{
		TeamID: "T123", OrgID: "org_1", TeamName: "Acme", InstalledByUserID: "user_1",
	})
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	got, err := s.GetSlackInstallationByTeamID(ctx, "T123")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.OrgID != "org_1" || got.TeamName != "Acme" {
		t.Fatalf("got %+v", got)
	}
}

func TestUpsertSlackInstallationRepointsExistingTeam(t *testing.T) {
	s := newTestStore(t)
	ctx := t.Context()
	_ = s.UpsertSlackInstallation(ctx, &SlackInstallation{TeamID: "T123", OrgID: "org_1"})
	_ = s.UpsertSlackInstallation(ctx, &SlackInstallation{TeamID: "T123", OrgID: "org_2"})
	got, err := s.GetSlackInstallationByTeamID(ctx, "T123")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.OrgID != "org_2" {
		t.Fatalf("expected re-pointed org_2, got %s", got.OrgID)
	}
}

// Regression test for the bug: a bot token used to live on the org-scoped
// Credential table, unique on (org_id, name) — so a second Slack workspace
// connected by the same org silently overwrote the first's token there, and
// every lookup for EITHER team would return whichever token was saved most
// recently. The token now lives on the team-scoped SlackInstallation row,
// so two workspaces under one org must keep two independent tokens.
func TestTwoWorkspacesUnderOneOrgKeepIndependentBotTokens(t *testing.T) {
	s := newTestStore(t)
	ctx := t.Context()

	if err := s.UpsertSlackInstallation(ctx, &SlackInstallation{TeamID: "T1", OrgID: "org_1"}); err != nil {
		t.Fatalf("upsert T1: %v", err)
	}
	if err := s.UpsertSlackInstallation(ctx, &SlackInstallation{TeamID: "T2", OrgID: "org_1"}); err != nil {
		t.Fatalf("upsert T2: %v", err)
	}
	if err := s.SetSlackBotToken(ctx, "T1", "xoxb-workspace-one"); err != nil {
		t.Fatalf("set T1 token: %v", err)
	}
	if err := s.SetSlackBotToken(ctx, "T2", "xoxb-workspace-two"); err != nil {
		t.Fatalf("set T2 token: %v", err)
	}

	inst1, err := s.GetSlackInstallationByTeamID(ctx, "T1")
	if err != nil {
		t.Fatalf("get T1: %v", err)
	}
	tok1, err := inst1.DecryptBotToken()
	if err != nil || tok1 != "xoxb-workspace-one" {
		t.Fatalf("T1 token = %q, err=%v, want xoxb-workspace-one", tok1, err)
	}

	inst2, err := s.GetSlackInstallationByTeamID(ctx, "T2")
	if err != nil {
		t.Fatalf("get T2: %v", err)
	}
	tok2, err := inst2.DecryptBotToken()
	if err != nil || tok2 != "xoxb-workspace-two" {
		t.Fatalf("T2 token = %q, err=%v, want xoxb-workspace-two", tok2, err)
	}
}

// RecordSlackEvent is what turns a Slack redelivery into a no-op instead of
// a second submission: the same event_id claimed twice must report fresh
// only the first time.
func TestRecordSlackEventClaimsOnceThenReportsDuplicate(t *testing.T) {
	s := newTestStore(t)
	ctx := t.Context()

	fresh, err := s.RecordSlackEvent(ctx, "Ev0PV52K21")
	if err != nil {
		t.Fatalf("first claim: %v", err)
	}
	if !fresh {
		t.Fatal("expected the first claim of a new event_id to be fresh")
	}

	fresh, err = s.RecordSlackEvent(ctx, "Ev0PV52K21")
	if err != nil {
		t.Fatalf("second claim: %v", err)
	}
	if fresh {
		t.Fatal("expected a redelivery of the same event_id to report NOT fresh")
	}
}

// A different event_id must never be blocked by an unrelated one already
// claimed — the ledger keys on event_id, not on "anything was recorded".
func TestRecordSlackEventDistinctIDsBothClaimFresh(t *testing.T) {
	s := newTestStore(t)
	ctx := t.Context()

	if fresh, err := s.RecordSlackEvent(ctx, "Ev1"); err != nil || !fresh {
		t.Fatalf("Ev1: fresh=%v err=%v", fresh, err)
	}
	if fresh, err := s.RecordSlackEvent(ctx, "Ev2"); err != nil || !fresh {
		t.Fatalf("Ev2: fresh=%v err=%v", fresh, err)
	}
}

func TestChannelBindingRoundTrip(t *testing.T) {
	s := newTestStore(t)
	ctx := t.Context()
	b := &SlackChannelBinding{OrgID: "org_1", TeamID: "T123", ChannelID: "C1", RepoURL: "https://github.com/acme/widget"}
	if err := s.CreateSlackChannelBinding(ctx, b); err != nil {
		t.Fatalf("create: %v", err)
	}
	got, err := s.GetSlackChannelBinding(ctx, "T123", "C1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.RepoURL != "https://github.com/acme/widget" {
		t.Fatalf("got %+v", got)
	}
}

func TestLatestSlackTriggeredTaskReturnsMostRecent(t *testing.T) {
	s := newTestStore(t)
	ctx := t.Context()
	older := &SlackTriggeredTask{OrgID: "org_1", TeamID: "T1", ChannelID: "C1", ThreadTS: "100.1", QueuedTaskID: "task_a"}
	newer := &SlackTriggeredTask{OrgID: "org_1", TeamID: "T1", ChannelID: "C1", ThreadTS: "100.1", QueuedTaskID: "task_b"}
	if err := s.CreateSlackTriggeredTask(ctx, older); err != nil {
		t.Fatalf("create older: %v", err)
	}
	if err := s.CreateSlackTriggeredTask(ctx, newer); err != nil {
		t.Fatalf("create newer: %v", err)
	}
	got, err := s.LatestSlackTriggeredTask(ctx, "T1", "C1", "100.1")
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	if got.QueuedTaskID != "task_b" {
		t.Fatalf("expected the newer row (task_b), got %s", got.QueuedTaskID)
	}
}
