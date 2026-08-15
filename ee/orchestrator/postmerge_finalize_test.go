// SPDX-License-Identifier: LicenseRef-Kiwi-BSL-1.1
// Copyright (c) 2026 RunKiwi. Licensed under the Business Source License 1.1.
// See ee/LICENSE. This is Control Plane code and is NOT Apache-2.0.

package orchestrator

import (
	"context"
	"testing"
	"time"

	"github.com/ibreakthecloud/kiwi/pkg/store"
	"github.com/ibreakthecloud/kiwi/pkg/ver"
)

// seedMonitorWithRecord installs an Organization, a merged Job/QueuedTask, its
// original kiwi.ver/v1 execution record, and a MONITORING PostMergeMonitor —
// the state finalizeMonitor expects to find.
func seedMonitorWithRecord(t *testing.T, s *store.PostgresStore, orgID, jobID string, autoRemediate bool) *store.PostMergeMonitor {
	t.Helper()
	ctx := context.Background()

	if err := s.DB().Create(&store.Organization{ID: orgID, Name: "acme", AutoRemediate: autoRemediate}).Error; err != nil {
		t.Fatal(err)
	}
	prURL := "https://github.com/acme/widgets/pull/42"
	parent := &store.QueuedTask{
		ID: jobID + "-impl", OrgID: orgID, JobID: jobID, RootTaskID: jobID + "-impl",
		Status: store.TaskSucceeded, ResultURL: &prURL,
		Spec: map[string]interface{}{"task": "add a health endpoint", "repo_url": "https://github.com/acme/widgets"},
	}
	if err := s.EnqueueTask(ctx, parent); err != nil {
		t.Fatal(err)
	}
	if err := s.DB().Create(&store.Job{ID: jobID, OrgID: orgID, Status: "SUCCEEDED"}).Error; err != nil {
		t.Fatal(err)
	}

	origRec := &ver.Record{Ver: ver.SchemaVersion, RecordID: "rec-1", OrgID: orgID, JobID: jobID}
	body, _ := ver.Canonicalize(origRec)
	hash, _ := ver.Hash(origRec)
	if err := s.DB().Create(&store.ExecutionRecord{
		RecordID: "rec-1", OrgID: orgID, JobID: jobID, Ver: ver.SchemaVersion,
		RecordHash: hash, Body: body, SigningKeyID: "cp_2026_07", RecordSignature: "sig1",
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.DB().Create(&store.ExecutionRecordHead{OrgID: orgID, HeadHash: hash}).Error; err != nil {
		t.Fatal(err)
	}

	mon := &store.PostMergeMonitor{
		ID: "mon_1", OrgID: orgID, JobID: jobID, Repo: "acme/widgets", PRNumber: 42,
		MergeCommitSHA: "abc123", Status: store.MonitorStatusMonitoring,
		DeployedAt: time.Now(), WindowEndsAt: time.Now().Add(24 * time.Hour),
	}
	if err := s.CreateMonitor(ctx, mon); err != nil {
		t.Fatal(err)
	}
	return mon
}

func TestFinalizeMonitorVerifiedAppendsSignedRecord(t *testing.T) {
	srv, s := setupWebhookTest(t)
	mon := seedMonitorWithRecord(t, s, "org1", "job1", false)

	srv.finalizeMonitor(context.Background(), mon, store.MonitorStatusVerified, "24h window elapsed with no regression signal")

	var got store.PostMergeMonitor
	if err := s.DB().First(&got, "id = ?", "mon_1").Error; err != nil {
		t.Fatal(err)
	}
	if got.Status != store.MonitorStatusVerified {
		t.Errorf("status = %q, want VERIFIED", got.Status)
	}

	var recs []store.ExecutionRecord
	s.DB().Where("job_id = ? AND ver = ?", "job1", ver.PostMergeVerifySchemaVersion).Find(&recs)
	if len(recs) != 1 {
		t.Fatalf("expected 1 postmerge record, got %d", len(recs))
	}
}

func TestFinalizeMonitorRegressionWithAutoRemediateSubmitsContinuation(t *testing.T) {
	srv, s := setupWebhookTest(t)
	mon := seedMonitorWithRecord(t, s, "org1", "job1", true)

	srv.finalizeMonitor(context.Background(), mon, store.MonitorStatusRegression, "revert PR #43")

	// continuationsFor (pr_comment_webhook_test.go) filters to OriginPRComment;
	// the remediation continuation uses a different origin, so query directly.
	all, err := s.ThreadTasks(context.Background(), "org1", "job1-impl")
	if err != nil {
		t.Fatal(err)
	}
	var remediations []store.QueuedTask
	for _, task := range all {
		if task.Origin == store.OriginPostMergeRemediation {
			remediations = append(remediations, task)
		}
	}
	if len(remediations) != 1 {
		t.Fatalf("got %d remediation continuations, want 1", len(remediations))
	}

	var monRow store.PostMergeMonitor
	if err := s.DB().First(&monRow, "id = ?", "mon_1").Error; err != nil {
		t.Fatal(err)
	}
	if monRow.RemediationTaskID == nil || *monRow.RemediationTaskID != remediations[0].ID {
		t.Errorf("monitor.remediation_task_id = %v, want %q", monRow.RemediationTaskID, remediations[0].ID)
	}
}

func TestFinalizeMonitorRegressionWithoutAutoRemediateDoesNotSubmit(t *testing.T) {
	srv, s := setupWebhookTest(t)
	mon := seedMonitorWithRecord(t, s, "org1", "job1", false)

	srv.finalizeMonitor(context.Background(), mon, store.MonitorStatusRegression, "revert PR #43")

	all, err := s.ThreadTasks(context.Background(), "org1", "job1-impl")
	if err != nil {
		t.Fatal(err)
	}
	for _, task := range all {
		if task.Origin == store.OriginPostMergeRemediation {
			t.Errorf("found a remediation continuation with auto_remediate off: %s", task.ID)
		}
	}
}

func TestFinalizeMonitorIsNoOpIfAlreadyFinalized(t *testing.T) {
	srv, s := setupWebhookTest(t)
	mon := seedMonitorWithRecord(t, s, "org1", "job1", false)

	if won, err := s.FinalizeMonitor(context.Background(), mon.ID, store.MonitorStatusVerified, "already done"); err != nil || !won {
		t.Fatalf("setup: expected the direct FinalizeMonitor call to win, got won=%v err=%v", won, err)
	}

	// Now try to finalize again through the higher-level path — must be a no-op.
	srv.finalizeMonitor(context.Background(), mon, store.MonitorStatusRegression, "should not apply")

	var got store.PostMergeMonitor
	if err := s.DB().First(&got, "id = ?", "mon_1").Error; err != nil {
		t.Fatal(err)
	}
	if got.Status != store.MonitorStatusVerified {
		t.Errorf("status = %q, want VERIFIED (the winning call's verdict, unchanged)", got.Status)
	}
}
