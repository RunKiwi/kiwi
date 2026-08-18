// SPDX-License-Identifier: LicenseRef-Kiwi-BSL-1.1
// Copyright (c) 2026 RunKiwi. Licensed under the Business Source License 1.1.
// See ee/LICENSE. This is Control Plane code and is NOT Apache-2.0.

package orchestrator

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ibreakthecloud/kiwi/pkg/store"
	"github.com/ibreakthecloud/kiwi/pkg/ver"
)

// testGitHubInstallationID is the installation id seedMonitorWithRecord links
// to whichever org it's seeding — checkForRevert and handleCheckRun both
// resolve org via this id (GitHub sends it on every App webhook delivery),
// not via a repo-name lookup, so a webhook payload built by the tests in this
// package must carry the same id for the org resolution to succeed.
const testGitHubInstallationID int64 = 555000001

// seedMonitorWithRecord installs an Organization, a GitHub App installation
// linking it to testGitHubInstallationID, a merged Job/QueuedTask, its
// original kiwi.ver/v1 execution record, and a MONITORING PostMergeMonitor —
// the state finalizeMonitor expects to find.
func seedMonitorWithRecord(t *testing.T, s *store.PostgresStore, orgID, jobID string, autoRemediate bool) *store.PostMergeMonitor {
	t.Helper()
	ctx := context.Background()

	if err := s.DB().Create(&store.Organization{ID: orgID, Name: "acme", AutoRemediate: autoRemediate}).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.DB().Create(&store.GitHubInstallation{
		InstallationID: testGitHubInstallationID, OrgID: orgID, AccountLogin: "acme",
	}).Error; err != nil {
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
		MergeCommitSHA: "582815c759063ccccca66c869360464f8dbcbc75", Status: store.MonitorStatusMonitoring,
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
	// setupWebhookTest injects a signing key (srv.signingKeyFn), so the
	// record this test's own name claims is "signed" must actually carry a
	// signature and the key id that produced it — not just exist.
	if recs[0].RecordSignature == "" {
		t.Error("postmerge record is unsigned, want a signature")
	}
	if recs[0].SigningKeyID != "cp_2026_07" {
		t.Errorf("signing_key_id = %q, want cp_2026_07", recs[0].SigningKeyID)
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

// TestSubmitRemediationPicksMostRecentTaskInThreadNotRoot proves the fix to
// submitRemediation's parent-task lookup: when a job's thread already has a
// PR-comment continuation (i.e. more than one task), remediation must attach
// to the most recent task in the thread, not the root task the old
// "parent_task_id IS NULL" query picked. Attaching to the root would fork a
// sibling branch off round one instead of extending the thread's actual
// latest state.
func TestSubmitRemediationPicksMostRecentTaskInThreadNotRoot(t *testing.T) {
	srv, s := setupWebhookTest(t)
	mon := seedMonitorWithRecord(t, s, "org1", "job1", true)
	ctx := context.Background()

	// A second, later task in the same thread — as a PR-comment continuation
	// would create — sharing the root's job id and root_task_id, with a
	// later CreatedAt than the root.
	rootID := "job1-impl"
	second := &store.QueuedTask{
		ID: "job1-c0001", OrgID: "org1", JobID: "job1", RootTaskID: rootID,
		ParentTaskID: &rootID, Origin: store.OriginPRComment,
		Status: store.TaskSucceeded,
		Spec:   map[string]interface{}{"task": "address review comment", "repo_url": "https://github.com/acme/widgets"},
	}
	if err := s.EnqueueTask(ctx, second); err != nil {
		t.Fatal(err)
	}
	// EnqueueTask/BeforeCreate does not control CreatedAt — force it later
	// than the root's so the "most recent" ordering is unambiguous rather
	// than depending on two rows landing on the same clock tick.
	if err := s.DB().Model(&store.QueuedTask{}).Where("id = ?", second.ID).
		Update("created_at", time.Now().Add(1*time.Hour)).Error; err != nil {
		t.Fatal(err)
	}

	// A session on the second task, to prove submitRemediation also carries
	// the session of the task it actually picks, not the root's (which has
	// none here).
	if err := s.DB().Create(&store.AgentSession{
		ID: "sess-second", OrgID: "org1", JobID: "job1", TaskID: second.ID, Phase: "done",
	}).Error; err != nil {
		t.Fatal(err)
	}

	srv.finalizeMonitor(ctx, mon, store.MonitorStatusRegression, "check run failed")

	all, err := s.ThreadTasks(ctx, "org1", rootID)
	if err != nil {
		t.Fatal(err)
	}
	var remediation *store.QueuedTask
	for i := range all {
		if all[i].Origin == store.OriginPostMergeRemediation {
			remediation = &all[i]
		}
	}
	if remediation == nil {
		t.Fatalf("no remediation continuation found among %d thread tasks", len(all))
	}
	if remediation.ParentTaskID == nil || *remediation.ParentTaskID != second.ID {
		t.Errorf("remediation.parent_task_id = %v, want %q (the most recent task, not the root)", remediation.ParentTaskID, second.ID)
	}

	// The session moved onto the remediation task rather than being dropped —
	// ReattachSessionIn (called inside SubmitContinuation) reassigns task_id.
	var sess store.AgentSession
	if err := s.DB().First(&sess, "id = ?", "sess-second").Error; err != nil {
		t.Fatal(err)
	}
	if sess.TaskID != remediation.ID {
		t.Errorf("session.task_id = %q, want %q (reattached onto the remediation task)", sess.TaskID, remediation.ID)
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

func TestFinalizePastWindowMonitorsMarksCleanOnesVerified(t *testing.T) {
	srv, s := setupWebhookTest(t)
	if err := s.DB().Create(&store.Organization{ID: "org1", Name: "acme"}).Error; err != nil {
		t.Fatal(err)
	}
	mon := &store.PostMergeMonitor{
		ID: "mon_past", OrgID: "org1", JobID: "job1", Repo: "acme/widgets", PRNumber: 42,
		MergeCommitSHA: "abc123", Status: store.MonitorStatusMonitoring,
		DeployedAt:   time.Now().Add(-25 * time.Hour),
		WindowEndsAt: time.Now().Add(-1 * time.Hour), // already elapsed
	}
	if err := s.CreateMonitor(context.Background(), mon); err != nil {
		t.Fatal(err)
	}

	srv.FinalizePastWindowMonitors(context.Background())

	var got store.PostMergeMonitor
	if err := s.DB().First(&got, "id = ?", "mon_past").Error; err != nil {
		t.Fatal(err)
	}
	if got.Status != store.MonitorStatusVerified {
		t.Errorf("status = %q, want VERIFIED", got.Status)
	}
}

func TestFinalizePastWindowMonitorsLeavesFutureOnesAlone(t *testing.T) {
	srv, s := setupWebhookTest(t)
	if err := s.DB().Create(&store.Organization{ID: "org1", Name: "acme"}).Error; err != nil {
		t.Fatal(err)
	}
	mon := &store.PostMergeMonitor{
		ID: "mon_future", OrgID: "org1", JobID: "job1", Repo: "acme/widgets", PRNumber: 42,
		MergeCommitSHA: "abc123", Status: store.MonitorStatusMonitoring,
		DeployedAt:   time.Now(),
		WindowEndsAt: time.Now().Add(23 * time.Hour), // not yet elapsed
	}
	if err := s.CreateMonitor(context.Background(), mon); err != nil {
		t.Fatal(err)
	}

	srv.FinalizePastWindowMonitors(context.Background())

	var got store.PostMergeMonitor
	if err := s.DB().First(&got, "id = ?", "mon_future").Error; err != nil {
		t.Fatal(err)
	}
	if got.Status != store.MonitorStatusMonitoring {
		t.Errorf("status = %q, want unchanged MONITORING", got.Status)
	}
}

func TestFinalizeMonitorPostsToSlackWhenConfigured(t *testing.T) {
	var gotBody string
	slackSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(http.StatusOK)
	}))
	defer slackSrv.Close()

	srv, s := setupWebhookTest(t)
	mon := seedMonitorWithRecord(t, s, "org1", "job1", false)
	if err := s.SaveCredential(context.Background(), "org1", "SLACK_WEBHOOK_URL", store.CredentialWebhook, slackSrv.URL); err != nil {
		t.Fatal(err)
	}

	srv.finalizeMonitor(context.Background(), mon, store.MonitorStatusRegression, "test evidence")

	if !strings.Contains(gotBody, "REGRESSION") {
		t.Errorf("slack body = %s, want it to mention REGRESSION", gotBody)
	}
}

func TestFinalizeMonitorSkipsSlackWhenNotConfigured(t *testing.T) {
	srv, s := setupWebhookTest(t)
	mon := seedMonitorWithRecord(t, s, "org1", "job1", false)
	// No SLACK_WEBHOOK_URL credential saved — must not panic or error.
	srv.finalizeMonitor(context.Background(), mon, store.MonitorStatusRegression, "test evidence")
}
