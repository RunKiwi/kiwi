package orchestrator

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"github.com/ibreakthecloud/kiwi/pkg/auth"
	"github.com/ibreakthecloud/kiwi/pkg/store"
)

func TestHandleJobStatus(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(
		&store.Organization{}, &store.OrgLimits{}, &store.QueuedTask{},
		&store.Credential{}, &store.Daemon{}, &store.DaemonJoinToken{},
		&store.Job{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	st := store.NewPostgresStore(db)
	srv := &Server{db: db, storage: st}

	task1 := store.QueuedTask{
		ID:     "j1-t1",
		OrgID:  "org-1",
		JobID:  "j1",
		Status: store.TaskSucceeded,
	}
	if err := st.DB().Create(&task1).Error; err != nil {
		t.Fatal(err)
	}
	if err := st.DB().Model(&store.QueuedTask{}).Where("id = ?", task1.ID).Update("result_url", "https://pr").Error; err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/jobs/j1", nil)
	req = req.WithContext(auth.ContextWithClaims(req.Context(), &auth.UserClaims{OrgID: "org-1"}))
	rr := httptest.NewRecorder()

	srv.handleJobStatus(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d. body: %s", rr.Code, rr.Body.String())
	}

	var resp JobStatusResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.JobID != "j1" {
		t.Errorf("job_id: got %v, want j1", resp.JobID)
	}
	if len(resp.Tasks) != 1 {
		t.Fatalf("tasks len: got %d, want 1", len(resp.Tasks))
	}
	if resp.Tasks[0].ID != "j1-t1" || resp.Tasks[0].Status != store.TaskSucceeded || *resp.Tasks[0].ResultURL != "https://pr" {
		t.Errorf("unexpected task payload: %+v", resp.Tasks[0])
	}

	req2 := httptest.NewRequest(http.MethodGet, "/api/v1/jobs/j1", nil)
	req2 = req2.WithContext(auth.ContextWithClaims(req2.Context(), &auth.UserClaims{OrgID: "org-2"}))
	rr2 := httptest.NewRecorder()
	srv.handleJobStatus(rr2, req2)
	if rr2.Code != http.StatusNotFound {
		t.Errorf("expected 404 for wrong org, got %d", rr2.Code)
	}
}

func TestHandleJobsList(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(
		&store.Organization{}, &store.OrgLimits{}, &store.QueuedTask{},
		&store.Credential{}, &store.Daemon{}, &store.DaemonJoinToken{},
		&store.Job{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	st := store.NewPostgresStore(db)
	srv := &Server{db: db, storage: st}

	now := time.Now()
	// Job 1: FAILED
	db.Create(&store.QueuedTask{ID: "j1-t1", OrgID: "org-1", JobID: "j1", Status: store.TaskFailed, CreatedAt: now.Add(-time.Hour)})
	// Job 2: SUCCEEDED
	db.Create(&store.QueuedTask{ID: "j2-t1", OrgID: "org-1", JobID: "j2", Status: store.TaskSucceeded, CreatedAt: now.Add(-30 * time.Minute)})
	// Job 3: RUNNING
	db.Create(&store.QueuedTask{ID: "j3-t1", OrgID: "org-1", JobID: "j3", Status: store.TaskLeased, CreatedAt: now.Add(-15 * time.Minute)})
	// Job 4: QUEUED
	db.Create(&store.QueuedTask{ID: "j4-t1", OrgID: "org-1", JobID: "j4", Status: store.TaskQueued, CreatedAt: now.Add(-5 * time.Minute)})
	// Another org
	db.Create(&store.QueuedTask{ID: "j5-t1", OrgID: "org-2", JobID: "j5", Status: store.TaskQueued, CreatedAt: now})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/jobs", nil)
	req = req.WithContext(auth.ContextWithClaims(req.Context(), &auth.UserClaims{OrgID: "org-1"}))
	rr := httptest.NewRecorder()

	srv.handleJobsList(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d. body: %s", rr.Code, rr.Body.String())
	}

	var resp JobsListResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Jobs) != 4 {
		t.Fatalf("jobs len: got %d, want 4", len(resp.Jobs))
	}

	if resp.Jobs[0].JobID != "j4" || resp.Jobs[0].Status != "QUEUED" {
		t.Errorf("expected j4 (QUEUED), got %s (%s)", resp.Jobs[0].JobID, resp.Jobs[0].Status)
	}
	if resp.Jobs[1].JobID != "j3" || resp.Jobs[1].Status != "RUNNING" {
		t.Errorf("expected j3 (RUNNING), got %s (%s)", resp.Jobs[1].JobID, resp.Jobs[1].Status)
	}
	if resp.Jobs[2].JobID != "j2" || resp.Jobs[2].Status != "SUCCEEDED" {
		t.Errorf("expected j2 (SUCCEEDED), got %s (%s)", resp.Jobs[2].JobID, resp.Jobs[2].Status)
	}
	if resp.Jobs[3].JobID != "j1" || resp.Jobs[3].Status != "FAILED" {
		t.Errorf("expected j1 (FAILED), got %s (%s)", resp.Jobs[3].JobID, resp.Jobs[3].Status)
	}
}
