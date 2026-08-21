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
