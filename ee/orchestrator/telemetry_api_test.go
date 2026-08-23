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

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/ibreakthecloud/kiwi/pkg/store"
)

func setupTelemetryAPITest(t *testing.T) (*Server, store.Store) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(
		&store.Organization{}, &store.OrgLimits{}, &store.Credential{},
		&store.Daemon{}, &store.DaemonJoinToken{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	st := store.NewPostgresStore(db)
	s := &Server{db: db, storage: st}
	return s, st
}

func TestHandleSandboxCacheStats(t *testing.T) {
	s, st := setupTelemetryAPITest(t)

	// Seed daemon 1 with telemetry for org-1
	err := st.DB().Create(&store.Daemon{
		ID:         "dmn-1",
		OrgID:      "org-1",
		SignPubKey: "sign-pub-1",
		EncPubKey:  "enc-pub-1",
		LastCacheStats: &store.CacheHeartbeatStats{
			TotalRepos:           18,
			TotalActiveWorktrees: 2,
			HitCount:             94,
			MissCount:            6,
		},
		ActiveContainers: 1,
		CreatedAt:        time.Now(),
	}).Error
	require.NoError(t, err)

	// Seed daemon 2 with nil LastCacheStats for org-1 (backward compatibility: should be omitted from calculations)
	err = st.DB().Create(&store.Daemon{
		ID:               "dmn-2",
		OrgID:            "org-1",
		SignPubKey:       "sign-pub-2",
		EncPubKey:        "enc-pub-2",
		LastCacheStats:   nil,
		ActiveContainers: 0,
		CreatedAt:        time.Now(),
	}).Error
	require.NoError(t, err)

	// Seed daemon 3 for org-2 (isolation: should not leak into org-1)
	err = st.DB().Create(&store.Daemon{
		ID:         "dmn-3",
		OrgID:      "org-2",
		SignPubKey: "sign-pub-3",
		EncPubKey:  "enc-pub-3",
		LastCacheStats: &store.CacheHeartbeatStats{
			TotalRepos:           50,
			TotalActiveWorktrees: 10,
			HitCount:             100,
			MissCount:            0,
		},
		ActiveContainers: 2,
		CreatedAt:        time.Now(),
	}).Error
	require.NoError(t, err)

	t.Run("successful aggregation for org-1", func(t *testing.T) {
		req := authed(http.MethodGet, "/api/v1/sandbox/cache/stats", "", "org-1")
		w := httptest.NewRecorder()
		s.handleSandboxCacheStats(w, req)

		require.Equal(t, http.StatusOK, w.Code)
		var resp struct {
			CacheHitRatePct      float64 `json:"cache_hit_rate_pct"`
			TotalCachedTrees     int     `json:"total_cached_trees"`
			TotalActiveWorktrees int     `json:"total_active_worktrees"`
			DaemonsReporting     int     `json:"daemons_reporting"`
			DaemonsTotal         int     `json:"daemons_total"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		require.InDelta(t, 94.0, resp.CacheHitRatePct, 0.1) // 94/(94+6)*100
		require.Equal(t, 18, resp.TotalCachedTrees)
		require.Equal(t, 2, resp.TotalActiveWorktrees)
		require.Equal(t, 1, resp.DaemonsReporting)
		require.Equal(t, 2, resp.DaemonsTotal)
	})

	t.Run("multiple reporting daemons aggregation", func(t *testing.T) {
		err := st.DB().Create(&store.Daemon{
			ID:         "dmn-4",
			OrgID:      "org-3",
			SignPubKey: "sign-pub-4",
			EncPubKey:  "enc-pub-4",
			LastCacheStats: &store.CacheHeartbeatStats{
				TotalRepos:           10,
				TotalActiveWorktrees: 3,
				HitCount:             30,
				MissCount:            10,
			},
			ActiveContainers: 0,
			CreatedAt:        time.Now(),
		}).Error
		require.NoError(t, err)

		err = st.DB().Create(&store.Daemon{
			ID:         "dmn-5",
			OrgID:      "org-3",
			SignPubKey: "sign-pub-5",
			EncPubKey:  "enc-pub-5",
			LastCacheStats: &store.CacheHeartbeatStats{
				TotalRepos:           20,
				TotalActiveWorktrees: 2,
				HitCount:             70,
				MissCount:            10,
			},
			ActiveContainers: 0,
			CreatedAt:        time.Now(),
		}).Error
		require.NoError(t, err)

		req := authed(http.MethodGet, "/api/v1/sandbox/cache/stats", "", "org-3")
		w := httptest.NewRecorder()
		s.handleSandboxCacheStats(w, req)

		require.Equal(t, http.StatusOK, w.Code)
		var resp struct {
			CacheHitRatePct      float64 `json:"cache_hit_rate_pct"`
			TotalCachedTrees     int     `json:"total_cached_trees"`
			TotalActiveWorktrees int     `json:"total_active_worktrees"`
			DaemonsReporting     int     `json:"daemons_reporting"`
			DaemonsTotal         int     `json:"daemons_total"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		// total hits = 100, total misses = 20 => 100/120 * 100 = 83.333%
		require.InDelta(t, 83.333, resp.CacheHitRatePct, 0.01)
		require.Equal(t, 30, resp.TotalCachedTrees)
		require.Equal(t, 5, resp.TotalActiveWorktrees)
		require.Equal(t, 2, resp.DaemonsReporting)
		require.Equal(t, 2, resp.DaemonsTotal)
	})

	t.Run("empty org with no daemons", func(t *testing.T) {
		req := authed(http.MethodGet, "/api/v1/sandbox/cache/stats", "", "org-empty")
		w := httptest.NewRecorder()
		s.handleSandboxCacheStats(w, req)

		require.Equal(t, http.StatusOK, w.Code)
		var resp struct {
			CacheHitRatePct      float64 `json:"cache_hit_rate_pct"`
			TotalCachedTrees     int     `json:"total_cached_trees"`
			TotalActiveWorktrees int     `json:"total_active_worktrees"`
			DaemonsReporting     int     `json:"daemons_reporting"`
			DaemonsTotal         int     `json:"daemons_total"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		require.Equal(t, 0.0, resp.CacheHitRatePct)
		require.Equal(t, 0, resp.TotalCachedTrees)
		require.Equal(t, 0, resp.TotalActiveWorktrees)
		require.Equal(t, 0, resp.DaemonsReporting)
		require.Equal(t, 0, resp.DaemonsTotal)
	})

	t.Run("unauthenticated request returns 401", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/sandbox/cache/stats", nil)
		w := httptest.NewRecorder()
		s.handleSandboxCacheStats(w, req)

		require.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("unsupported HTTP method returns 405", func(t *testing.T) {
		methods := []string{http.MethodPost, http.MethodPut, http.MethodDelete}
		for _, m := range methods {
			req := authed(m, "/api/v1/sandbox/cache/stats", "", "org-1")
			w := httptest.NewRecorder()
			s.handleSandboxCacheStats(w, req)
			require.Equal(t, http.StatusMethodNotAllowed, w.Code)
		}
	})
}
