package agentapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/ibreakthecloud/kiwi/pkg/checkpoint"
	"github.com/ibreakthecloud/kiwi/pkg/store"
	"gorm.io/gorm"
)

type fakeSecrets struct{}

func (fakeSecrets) Resolve(ctx context.Context, jobID, key string) (string, error) {
	return "secret-for-" + key, nil
}

func testServer(t *testing.T) (*Server, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if sqlDB, err := db.DB(); err == nil {
		sqlDB.SetMaxOpenConns(1)
	}
	if err := db.AutoMigrate(&JobToken{}, &store.Job{}, &store.Event{}, &store.Checkpoint{}, &store.SideEffect{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	st := store.NewPostgresStore(db)
	deps := Deps{
		Store:   st,
		Events:  checkpoint.NewService(st, checkpoint.NewLocalSnapshotter(t.TempDir())),
		Secrets: fakeSecrets{},
	}
	return NewServer(deps), db
}

func do(t *testing.T, h http.Handler, path, token string, body interface{}) *httptest.ResponseRecorder {
	t.Helper()
	var r io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = bytes.NewReader(b)
	}
	req := httptest.NewRequest(http.MethodPost, path, r)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestValidateJobToken(t *testing.T) {
	_, db := testServer(t)

	tok, err := MintJobToken(db, "jobA", "org1", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	claims, err := ValidateJobToken(db, tok)
	if err != nil || claims.JobID != "jobA" || claims.OrgID != "org1" {
		t.Fatalf("validate = %+v err=%v", claims, err)
	}

	if _, err := ValidateJobToken(db, "kiwijob_garbage"); err != ErrInvalidToken {
		t.Errorf("garbage token err = %v, want ErrInvalidToken", err)
	}

	expired, _ := MintJobToken(db, "jobA", "org1", -time.Minute)
	if _, err := ValidateJobToken(db, expired); err != ErrExpiredToken {
		t.Errorf("expired token err = %v, want ErrExpiredToken", err)
	}
}

// The core #34 exit criterion: a token can only act on its own job.
func TestAgentAPIScopedToJob(t *testing.T) {
	srv, db := testServer(t)
	h := srv.Handler()
	tokA, _ := MintJobToken(db, "jobA", "org1", time.Hour)

	// Valid: token for jobA writing jobA's event log.
	rec := do(t, h, "/agent/jobA/events", tokA, appendEventReq{Phase: "actor", Payload: map[string]interface{}{"k": "v"}})
	if rec.Code != http.StatusOK {
		t.Fatalf("append event: got %d, body=%s", rec.Code, rec.Body.String())
	}
	var evResp struct {
		Seq int64 `json:"seq"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &evResp)
	if evResp.Seq != 1 {
		t.Errorf("first event seq = %d, want 1", evResp.Seq)
	}
	var count int64
	db.Model(&store.Event{}).Where("job_id = ?", "jobA").Count(&count)
	if count != 1 {
		t.Errorf("stored events for jobA = %d, want 1", count)
	}

	// Cross-job: token for jobA must NOT write jobB.
	rec = do(t, h, "/agent/jobB/events", tokA, appendEventReq{Phase: "actor"})
	if rec.Code != http.StatusForbidden {
		t.Errorf("cross-job write: got %d, want 403", rec.Code)
	}

	// Invalid token → 401.
	rec = do(t, h, "/agent/jobA/events", "kiwijob_nope", appendEventReq{Phase: "actor"})
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("invalid token: got %d, want 401", rec.Code)
	}

	// Expired token → 401.
	expired, _ := MintJobToken(db, "jobA", "org1", -time.Minute)
	rec = do(t, h, "/agent/jobA/events", expired, appendEventReq{Phase: "actor"})
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expired token: got %d, want 401", rec.Code)
	}

	// Missing bearer → 401.
	rec = do(t, h, "/agent/jobA/events", "", appendEventReq{Phase: "actor"})
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("no token: got %d, want 401", rec.Code)
	}
}

func TestAgentAPIFetchSecretAndResult(t *testing.T) {
	srv, db := testServer(t)
	h := srv.Handler()
	if err := db.Create(&store.Job{
		ID: "jobA", OrgID: "org1", UserID: "u1", Status: "RUNNING",
		Inputs: map[string]interface{}{},
	}).Error; err != nil {
		t.Fatal(err)
	}
	tokA, _ := MintJobToken(db, "jobA", "org1", time.Hour)

	// FetchSecret bridges to the resolver.
	rec := do(t, h, "/agent/jobA/secrets", tokA, fetchSecretReq{Key: "GITHUB_TOKEN"})
	if rec.Code != http.StatusOK {
		t.Fatalf("fetch secret: %d %s", rec.Code, rec.Body.String())
	}
	var sec struct {
		Value string `json:"value"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &sec)
	if sec.Value != "secret-for-GITHUB_TOKEN" {
		t.Errorf("secret value = %q", sec.Value)
	}

	// ReportResult updates the job's terminal status.
	rec = do(t, h, "/agent/jobA/result", tokA, reportResultReq{Status: "FAILED", Error: "boom"})
	if rec.Code != http.StatusOK {
		t.Fatalf("report result: %d %s", rec.Code, rec.Body.String())
	}
	var job store.Job
	db.First(&job, "id = ?", "jobA")
	if job.Status != "FAILED" {
		t.Errorf("job status = %q, want FAILED", job.Status)
	}
}
