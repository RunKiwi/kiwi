// SPDX-License-Identifier: LicenseRef-Kiwi-BSL-1.1
// Copyright (c) 2026 RunKiwi. Licensed under the Business Source License 1.1.
// See ee/LICENSE. This is Control Plane code and is NOT Apache-2.0.

package orchestrator

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ibreakthecloud/kiwi/pkg/store"
)

func newTestCachingServer(t *testing.T) (*Server, *http.ServeMux) {
	t.Helper()
	s := newTestServer(t)
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/analytics/caching", s.handleCachingAnalytics)
	return s, mux
}

func TestHandleCachingAnalytics(t *testing.T) {
	s, mux := newTestCachingServer(t)
	now := time.Now()

	// 1. Task for org-1 with 800 cached tokens, 200 raw tokens
	taskOrg1Recent := &store.QueuedTask{
		ID:                 "task-org1-recent",
		OrgID:              "org-1",
		JobID:              "job-1",
		Status:             store.TaskSucceeded,
		CachedPromptTokens: 800,
		RawPromptTokens:    200,
		Spec:               map[string]interface{}{"model": "claude-opus-4-8"},
		CreatedAt:          now.Add(-1 * time.Hour),
	}
	require.NoError(t, s.db.Create(taskOrg1Recent).Error)

	// 2. Task for org-2 to verify tenancy isolation
	taskOrg2 := &store.QueuedTask{
		ID:                 "task-org2",
		OrgID:              "org-2",
		JobID:              "job-2",
		Status:             store.TaskSucceeded,
		CachedPromptTokens: 500,
		RawPromptTokens:    500,
		Spec:               map[string]interface{}{"model": "claude-opus-4-8"},
		CreatedAt:          now.Add(-1 * time.Hour),
	}
	require.NoError(t, s.db.Create(taskOrg2).Error)

	// 3. Old task for org-1 (> 35 days ago) to verify 30-day filter
	taskOrg1Old := &store.QueuedTask{
		ID:                 "task-org1-old",
		OrgID:              "org-1",
		JobID:              "job-old",
		Status:             store.TaskSucceeded,
		CachedPromptTokens: 1000,
		RawPromptTokens:    1000,
		Spec:               map[string]interface{}{"model": "claude-opus-4-8"},
		CreatedAt:          now.Add(-36 * 24 * time.Hour),
	}
	require.NoError(t, s.db.Create(taskOrg1Old).Error)

	// Perform authed GET request for org-1
	req := authedRequest(t, http.MethodGet, "/api/v1/analytics/caching", nil, "org-1")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var resp struct {
		CachedPromptTokens    int64   `json:"cached_prompt_tokens"`
		RawPromptTokens       int64   `json:"raw_prompt_tokens"`
		CacheDiscountRate     float64 `json:"cache_discount_rate"`
		TotalDollarSavingsUSD float64 `json:"total_dollar_savings_usd"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))

	require.Equal(t, int64(800), resp.CachedPromptTokens)
	require.Equal(t, int64(200), resp.RawPromptTokens)
	require.InDelta(t, 0.8, resp.CacheDiscountRate, 1e-6)
	require.Greater(t, resp.TotalDollarSavingsUSD, 0.0)
}

func TestHandleCachingAnalytics_Unauthorized(t *testing.T) {
	_, mux := newTestCachingServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/analytics/caching", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	require.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestHandleCachingAnalytics_MethodNotAllowed(t *testing.T) {
	_, mux := newTestCachingServer(t)

	req := authedRequest(t, http.MethodPost, "/api/v1/analytics/caching", nil, "org-1")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	require.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

func TestHandleCachingAnalytics_Empty(t *testing.T) {
	_, mux := newTestCachingServer(t)

	req := authedRequest(t, http.MethodGet, "/api/v1/analytics/caching", nil, "org-empty")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var resp struct {
		CachedPromptTokens    int64   `json:"cached_prompt_tokens"`
		RawPromptTokens       int64   `json:"raw_prompt_tokens"`
		CacheDiscountRate     float64 `json:"cache_discount_rate"`
		TotalDollarSavingsUSD float64 `json:"total_dollar_savings_usd"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))

	require.Equal(t, int64(0), resp.CachedPromptTokens)
	require.Equal(t, int64(0), resp.RawPromptTokens)
	require.Equal(t, 0.0, resp.CacheDiscountRate)
	require.Equal(t, 0.0, resp.TotalDollarSavingsUSD)
}
