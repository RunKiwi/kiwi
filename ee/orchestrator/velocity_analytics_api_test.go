// SPDX-License-Identifier: LicenseRef-Kiwi-BSL-1.1
// Copyright (c) 2026 RunKiwi. Licensed under the Business Source License 1.1.
// See ee/LICENSE. This is Control Plane code and is NOT Apache-2.0.

package orchestrator

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"github.com/ibreakthecloud/kiwi/ee/auth"
	"github.com/ibreakthecloud/kiwi/pkg/store"
	"github.com/ibreakthecloud/kiwi/pkg/ver"
)

func newVelocityTestServer(t *testing.T) *Server {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s_%d?mode=memory&cache=shared", t.Name(), time.Now().UnixNano())), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(
		&auth.Organization{},
		&store.ExecutionRecord{},
		&store.ExecutionRecordHead{},
		&store.QueuedTask{},
		&store.Job{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	st := store.NewPostgresStore(db)
	return &Server{db: db, storage: st}
}

func createExecRecord(t *testing.T, srv *Server, orgID, jobID string, finalOutcome string, rejections int, createdAt time.Time) {
	t.Helper()
	recBody := ver.Record{
		Ver:   ver.SchemaVersion,
		JobID: jobID,
		OrgID: orgID,
		Verification: ver.Verification{
			FinalOutcome: finalOutcome,
		},
		Execution: ver.Execution{
			Workers: []ver.WorkerAttestation{
				{
					WorkerID:         "w-" + jobID,
					CriticRejections: rejections,
				},
			},
		},
	}
	bodyBytes, err := json.Marshal(recBody)
	if err != nil {
		t.Fatalf("marshal ver record: %v", err)
	}

	rec := store.ExecutionRecord{
		RecordID:  "rec_" + jobID,
		OrgID:     orgID,
		JobID:     jobID,
		Ver:       ver.SchemaVersion,
		Body:      bodyBytes,
		CreatedAt: createdAt,
	}
	if err := srv.db.Create(&rec).Error; err != nil {
		t.Fatalf("create execution record: %v", err)
	}
}

type velocityResponse struct {
	TestPassMetrics struct {
		ZeroShotPct    float64 `json:"zero_shot_pct"`
		SelfHealedPct  float64 `json:"self_healed_pct"`
		HumanGuidedPct float64 `json:"human_guided_pct"`
	} `json:"test_pass_metrics"`
	JobsCounted int `json:"jobs_counted"`
}

func TestHandleVelocityAnalytics(t *testing.T) {
	t.Run("ClassificationAndExclusions", func(t *testing.T) {
		srv := newVelocityTestServer(t)
		now := time.Now()

		// 1. Zero-shot: CriticRejections == 0, FinalOutcome == "pass", no human continuation
		createExecRecord(t, srv, "org-1", "job-zs", "pass", 0, now.Add(-1*time.Hour))
		if err := srv.db.Create(&store.QueuedTask{
			ID:         "task-zs",
			OrgID:      "org-1",
			JobID:      "job-zs",
			RootTaskID: "task-zs",
			Origin:     store.OriginSubmit,
		}).Error; err != nil {
			t.Fatal(err)
		}

		// 2. Self-healed: CriticRejections > 0, FinalOutcome == "pass", no human continuation
		createExecRecord(t, srv, "org-1", "job-sh", "pass", 2, now.Add(-2*time.Hour))
		if err := srv.db.Create(&store.QueuedTask{
			ID:         "task-sh",
			OrgID:      "org-1",
			JobID:      "job-sh",
			RootTaskID: "task-sh",
			Origin:     store.OriginSubmit,
		}).Error; err != nil {
			t.Fatal(err)
		}

		// 3. Human-guided: Any task in thread has Origin == store.OriginPRComment, FinalOutcome == "pass"
		// (even with CriticRejections > 0 or 0)
		createExecRecord(t, srv, "org-1", "job-hg", "pass", 1, now.Add(-3*time.Hour))
		if err := srv.db.Create(&store.QueuedTask{
			ID:         "task-hg-root",
			OrgID:      "org-1",
			JobID:      "job-hg",
			RootTaskID: "task-hg-root",
			Origin:     store.OriginSubmit,
		}).Error; err != nil {
			t.Fatal(err)
		}
		parentID := "task-hg-root"
		if err := srv.db.Create(&store.QueuedTask{
			ID:           "task-hg-reply",
			OrgID:        "org-1",
			JobID:        "job-hg",
			RootTaskID:   "task-hg-root",
			ParentTaskID: &parentID,
			Origin:       store.OriginPRComment,
		}).Error; err != nil {
			t.Fatal(err)
		}

		// 4. Verification failed: Verification.FinalOutcome != "pass" -> excluded from pass metrics & jobs_counted
		createExecRecord(t, srv, "org-1", "job-failed", "fail", 0, now.Add(-4*time.Hour))
		if err := srv.db.Create(&store.QueuedTask{
			ID:         "task-failed",
			OrgID:      "org-1",
			JobID:      "job-failed",
			RootTaskID: "task-failed",
			Origin:     store.OriginSubmit,
		}).Error; err != nil {
			t.Fatal(err)
		}

		// 5. Plan-mode paused job with no execution record: excluded and does not cause errors
		if err := srv.db.Create(&store.QueuedTask{
			ID:         "task-plan-paused",
			OrgID:      "org-1",
			JobID:      "job-plan-paused",
			RootTaskID: "task-plan-paused",
			Origin:     store.OriginSubmit,
			Status:     store.TaskQueued,
		}).Error; err != nil {
			t.Fatal(err)
		}

		req := authed(http.MethodGet, "/api/v1/analytics/velocity?range=7d", "", "org-1")
		rr := httptest.NewRecorder()

		srv.handleVelocityAnalytics(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d. body: %s", rr.Code, rr.Body.String())
		}

		var resp velocityResponse
		if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
			t.Fatalf("decode response: %v", err)
		}

		// Total passed jobs should be 3: 1 zero-shot, 1 self-healed, 1 human-guided
		if resp.JobsCounted != 3 {
			t.Errorf("expected jobs_counted = 3, got %d", resp.JobsCounted)
		}

		wantZeroShotPct := 100.0 / 3.0
		wantSelfHealedPct := 100.0 / 3.0
		wantHumanGuidedPct := 100.0 / 3.0

		const epsilon = 0.001
		if diff := resp.TestPassMetrics.ZeroShotPct - wantZeroShotPct; diff > epsilon || diff < -epsilon {
			t.Errorf("zero_shot_pct: want %v, got %v", wantZeroShotPct, resp.TestPassMetrics.ZeroShotPct)
		}
		if diff := resp.TestPassMetrics.SelfHealedPct - wantSelfHealedPct; diff > epsilon || diff < -epsilon {
			t.Errorf("self_healed_pct: want %v, got %v", wantSelfHealedPct, resp.TestPassMetrics.SelfHealedPct)
		}
		if diff := resp.TestPassMetrics.HumanGuidedPct - wantHumanGuidedPct; diff > epsilon || diff < -epsilon {
			t.Errorf("human_guided_pct: want %v, got %v", wantHumanGuidedPct, resp.TestPassMetrics.HumanGuidedPct)
		}
	})

	t.Run("RangesAndParsing", func(t *testing.T) {
		srv := newVelocityTestServer(t)
		now := time.Now()

		// Record 1: 12 hours ago (within 24h, 7d, 30d)
		createExecRecord(t, srv, "org-1", "job-12h", "pass", 0, now.Add(-12*time.Hour))
		// Record 2: 3 days ago (within 7d, 30d; outside 24h)
		createExecRecord(t, srv, "org-1", "job-3d", "pass", 0, now.Add(-3*24*time.Hour))
		// Record 3: 15 days ago (within 30d; outside 24h, 7d)
		createExecRecord(t, srv, "org-1", "job-15d", "pass", 0, now.Add(-15*24*time.Hour))
		// Record 4: 45 days ago (outside all ranges)
		createExecRecord(t, srv, "org-1", "job-45d", "pass", 0, now.Add(-45*24*time.Hour))

		tests := []struct {
			rangeParam string
			wantCount  int
		}{
			{rangeParam: "24h", wantCount: 1},
			{rangeParam: "7d", wantCount: 2},
			{rangeParam: "30d", wantCount: 3},
			{rangeParam: "", wantCount: 2},        // default 7d
			{rangeParam: "invalid", wantCount: 2}, // fallback to 7d
		}

		for _, tc := range tests {
			t.Run("range="+tc.rangeParam, func(t *testing.T) {
				path := "/api/v1/analytics/velocity"
				if tc.rangeParam != "" {
					path += "?range=" + tc.rangeParam
				}
				req := authed(http.MethodGet, path, "", "org-1")
				rr := httptest.NewRecorder()

				srv.handleVelocityAnalytics(rr, req)
				if rr.Code != http.StatusOK {
					t.Fatalf("expected 200 OK, got %d body %s", rr.Code, rr.Body.String())
				}

				var resp velocityResponse
				if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
					t.Fatalf("decode response: %v", err)
				}

				if resp.JobsCounted != tc.wantCount {
					t.Errorf("range %q: expected %d jobs counted, got %d", tc.rangeParam, tc.wantCount, resp.JobsCounted)
				}
			})
		}
	})

	t.Run("MethodNotAllowed", func(t *testing.T) {
		srv := newVelocityTestServer(t)
		for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch} {
			req := authed(method, "/api/v1/analytics/velocity", "", "org-1")
			rr := httptest.NewRecorder()
			srv.handleVelocityAnalytics(rr, req)
			if rr.Code != http.StatusMethodNotAllowed {
				t.Errorf("expected 405 Method Not Allowed for %s, got %d", method, rr.Code)
			}
		}
	})

	t.Run("Unauthorized", func(t *testing.T) {
		srv := newVelocityTestServer(t)
		req := httptest.NewRequest(http.MethodGet, "/api/v1/analytics/velocity", nil)
		rr := httptest.NewRecorder()
		srv.handleVelocityAnalytics(rr, req)
		if rr.Code != http.StatusUnauthorized {
			t.Errorf("expected 401 Unauthorized for request with no claims, got %d", rr.Code)
		}
	})

	t.Run("OrgIsolation", func(t *testing.T) {
		srv := newVelocityTestServer(t)
		now := time.Now()

		// org-1 record
		createExecRecord(t, srv, "org-1", "job-org1", "pass", 0, now.Add(-1*time.Hour))
		// org-2 record
		createExecRecord(t, srv, "org-2", "job-org2", "pass", 1, now.Add(-1*time.Hour))

		// Query as org-2
		req := authed(http.MethodGet, "/api/v1/analytics/velocity?range=7d", "", "org-2")
		rr := httptest.NewRecorder()
		srv.handleVelocityAnalytics(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d body %s", rr.Code, rr.Body.String())
		}

		var resp velocityResponse
		if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
			t.Fatalf("decode response: %v", err)
		}

		if resp.JobsCounted != 1 {
			t.Fatalf("expected org-2 to count 1 job, got %d", resp.JobsCounted)
		}
		if resp.TestPassMetrics.SelfHealedPct != 100.0 {
			t.Errorf("expected 100%% self-healed for org-2, got %v", resp.TestPassMetrics.SelfHealedPct)
		}
	})

	t.Run("ZeroJobs", func(t *testing.T) {
		srv := newVelocityTestServer(t)
		req := authed(http.MethodGet, "/api/v1/analytics/velocity?range=7d", "", "org-empty")
		rr := httptest.NewRecorder()
		srv.handleVelocityAnalytics(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d body %s", rr.Code, rr.Body.String())
		}

		var resp velocityResponse
		if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
			t.Fatalf("decode response: %v", err)
		}

		if resp.JobsCounted != 0 {
			t.Errorf("expected 0 jobs counted, got %d", resp.JobsCounted)
		}
		if resp.TestPassMetrics.ZeroShotPct != 0 || resp.TestPassMetrics.SelfHealedPct != 0 || resp.TestPassMetrics.HumanGuidedPct != 0 {
			t.Errorf("expected all percentages to be 0 for empty jobs, got %+v", resp.TestPassMetrics)
		}
	})
}
