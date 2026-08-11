// SPDX-License-Identifier: LicenseRef-Kiwi-BSL-1.1
// Copyright (c) 2026 RunKiwi. Licensed under the Business Source License 1.1.
// See ee/LICENSE. This is Control Plane code and is NOT Apache-2.0.

package orchestrator

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/ibreakthecloud/kiwi/pkg/store"
	"github.com/ibreakthecloud/kiwi/pkg/ver"
)

func generateSignature(secret, body []byte) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func setupWebhookTest(t *testing.T) (*Server, *store.PostgresStore) {
	t.Helper()

	seed := bytes.Repeat([]byte("a"), 32)
	key, err := ver.ParseSigningKey(base64.StdEncoding.EncodeToString(seed), "cp_2026_07")
	if err != nil {
		t.Fatalf("parse signing key: %v", err)
	}

	db := newTestDB(t)
	if err := db.AutoMigrate(&store.Job{}, &store.QueuedTask{}, &store.ExecutionRecord{}, &store.ExecutionRecordHead{},
		&store.Organization{}, &store.AgentSession{}, &store.AgentSessionEvent{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	s := store.NewPostgresStore(db)
	srv := NewServer(s, &Config{})
	// Injected rather than set through the environment: CPSigningKey memoizes
	// process-wide, so an env-based key would leak between tests and depend on
	// which test ran first.
	srv.signingKeyFn = func() (*ver.SigningKey, error) { return key, nil }
	return srv, s
}

func TestGithubWebhook(t *testing.T) {
	os.Setenv("GITHUB_WEBHOOK_SECRET", "test-secret")
	defer os.Unsetenv("GITHUB_WEBHOOK_SECRET")

	srv, dbStore := setupWebhookTest(t)

	// Setup database with an initial execution record so we can append to it
	orgID := "org-123"
	jobID := "job-456"
	prURL := "https://github.com/foo/bar/pull/42"

	// Create QueuedTask with result_url
	dbStore.DB().Create(&store.Job{
		ID:     jobID,
		OrgID:  orgID,
		Status: "SUCCEEDED",
	})
	dbStore.DB().Create(&store.QueuedTask{
		ID:        "qt-1",
		JobID:     jobID,
		OrgID:     orgID,
		ResultURL: &prURL,
		Status:    "SUCCEEDED",
	})

	// Create original record
	origRec := &ver.Record{
		Ver:      ver.SchemaVersion,
		RecordID: "rec-1",
		OrgID:    orgID,
		JobID:    jobID,
	}
	bodyBytes, _ := ver.Canonicalize(origRec)
	hash, _ := ver.Hash(origRec)

	dbStore.DB().Create(&store.ExecutionRecord{
		RecordID:        "rec-1",
		OrgID:           orgID,
		JobID:           jobID,
		Ver:             ver.SchemaVersion,
		PrevRecordHash:  "",
		RecordHash:      hash,
		Body:            bodyBytes,
		SigningKeyID:    "cp_2026_07",
		RecordSignature: "sig1",
	})
	dbStore.DB().Create(&store.ExecutionRecordHead{
		OrgID:    orgID,
		HeadHash: hash,
	})

	validPayload := githubWebhookPayload{
		Action: "closed",
	}
	validPayload.PullRequest.HTMLURL = prURL
	validPayload.PullRequest.Merged = true
	validPayload.PullRequest.MergedAt = time.Now().Format(time.RFC3339)
	validPayload.PullRequest.MergeCommitSHA = "abcdef123"
	validPayload.PullRequest.MergedBy.Login = "someuser"

	bodyBytes, _ = json.Marshal(validPayload)

	t.Run("valid signature appends chained record", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/github", bytes.NewReader(bodyBytes))
		req.Header.Set("X-Hub-Signature-256", generateSignature([]byte("test-secret"), bodyBytes))
		req.Header.Set("X-GitHub-Event", "pull_request")
		rr := httptest.NewRecorder()

		srv.handleGithubWebhook(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("expected 200 OK, got %d", rr.Code)
		}

		// Verify merge record was appended
		var recs []store.ExecutionRecord
		dbStore.DB().Where("job_id = ?", jobID).Order("created_at ASC").Find(&recs)
		if len(recs) != 2 {
			t.Fatalf("expected 2 records, got %d", len(recs))
		}

		mergeRec := recs[1]
		if mergeRec.Ver != ver.MergeSchemaVersion {
			t.Errorf("expected merge version, got %s", mergeRec.Ver)
		}
		if mergeRec.PrevRecordHash != hash {
			t.Errorf("expected prev hash %s, got %s", hash, mergeRec.PrevRecordHash)
		}

		// Verify chain head
		head, _ := dbStore.GetExecutionRecordChainHead(context.Background(), orgID)
		if head != mergeRec.RecordHash {
			t.Errorf("expected chain head %s, got %s", mergeRec.RecordHash, head)
		}
	})

	t.Run("duplicate delivery is idempotent", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/github", bytes.NewReader(bodyBytes))
		req.Header.Set("X-Hub-Signature-256", generateSignature([]byte("test-secret"), bodyBytes))
		req.Header.Set("X-GitHub-Event", "pull_request")
		rr := httptest.NewRecorder()

		srv.handleGithubWebhook(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("expected 200 OK, got %d", rr.Code)
		}

		var count int64
		dbStore.DB().Model(&store.ExecutionRecord{}).Where("job_id = ?", jobID).Count(&count)
		if count != 2 {
			t.Errorf("expected exactly 2 records (no duplication), got %d", count)
		}
	})

	t.Run("unknown PR URL is ignored", func(t *testing.T) {
		payload2 := validPayload
		payload2.PullRequest.HTMLURL = "https://github.com/foo/bar/pull/999"
		body2, _ := json.Marshal(payload2)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/github", bytes.NewReader(body2))
		req.Header.Set("X-Hub-Signature-256", generateSignature([]byte("test-secret"), body2))
		req.Header.Set("X-GitHub-Event", "pull_request")
		rr := httptest.NewRecorder()

		srv.handleGithubWebhook(rr, req)
		if rr.Code != http.StatusOK {
			t.Errorf("expected 200 OK, got %d", rr.Code)
		}
	})

	t.Run("bad signature is rejected", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/github", bytes.NewReader(bodyBytes))
		req.Header.Set("X-Hub-Signature-256", generateSignature([]byte("wrong-secret"), bodyBytes))
		req.Header.Set("X-GitHub-Event", "pull_request")
		rr := httptest.NewRecorder()

		srv.handleGithubWebhook(rr, req)
		if rr.Code != http.StatusUnauthorized {
			t.Errorf("expected 401, got %d", rr.Code)
		}
	})

	t.Run("unconfigured secret rejects", func(t *testing.T) {
		os.Unsetenv("GITHUB_WEBHOOK_SECRET")
		defer os.Setenv("GITHUB_WEBHOOK_SECRET", "test-secret")

		req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/github", bytes.NewReader(bodyBytes))
		req.Header.Set("X-Hub-Signature-256", generateSignature([]byte("test-secret"), bodyBytes))
		req.Header.Set("X-GitHub-Event", "pull_request")
		rr := httptest.NewRecorder()

		srv.handleGithubWebhook(rr, req)
		if rr.Code != http.StatusServiceUnavailable {
			t.Errorf("expected 503, got %d", rr.Code)
		}
	})
}
