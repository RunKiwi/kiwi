// SPDX-License-Identifier: LicenseRef-Kiwi-BSL-1.1
// Copyright (c) 2026 RunKiwi. Licensed under the Business Source License 1.1.
// See ee/LICENSE. This is Control Plane code and is NOT Apache-2.0.

package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/ibreakthecloud/kiwi/pkg/store"
)

func setupFleetTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := setupTestDB(t)
	require.NoError(t, db.AutoMigrate(&store.Daemon{}))
	return db
}

func TestHandleAdminMetricsFleetRequiresSuperAdmin(t *testing.T) {
	db := setupFleetTestDB(t)
	mux := http.NewServeMux()
	AdminRouter(db, mux)

	t.Run("no auth returns 403", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/admin/metrics/fleet", nil)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		require.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("org admin returns 403", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/admin/metrics/fleet", nil)
		req = req.WithContext(ContextWithClaims(req.Context(), &UserClaims{
			Role:  "admin",
			Email: "org-admin@example.com",
			OrgID: "org-1",
		}))
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		require.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("wrong bearer token returns 403", func(t *testing.T) {
		t.Setenv("KIWI_SERVER_TOKEN", "super-secret")
		req := httptest.NewRequest(http.MethodGet, "/admin/metrics/fleet", nil)
		req.Header.Set("Authorization", "Bearer wrong-token")
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		require.Equal(t, http.StatusForbidden, w.Code)
	})
}

func TestHandleAdminMetricsFleetAggregates(t *testing.T) {
	db := setupFleetTestDB(t)

	// Seed daemons directly via GORM
	err := db.Create(&store.Daemon{
		ID:               "dmn-1",
		OrgID:            "org-1",
		SignPubKey:       "sign-pub-1",
		EncPubKey:        "enc-pub-1",
		ActiveContainers: 4,
		CreatedAt:        time.Now(),
	}).Error
	require.NoError(t, err)

	err = db.Create(&store.Daemon{
		ID:               "dmn-2",
		OrgID:            "org-2",
		SignPubKey:       "sign-pub-2",
		EncPubKey:        "enc-pub-2",
		ActiveContainers: 8,
		CreatedAt:        time.Now(),
	}).Error
	require.NoError(t, err)

	mux := http.NewServeMux()
	AdminRouter(db, mux)

	t.Run("authenticated with system user claims", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/admin/metrics/fleet", nil)
		req = req.WithContext(ContextWithClaims(context.Background(), &UserClaims{UserID: "system"}))
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)

		require.Equal(t, http.StatusOK, w.Code)
		var resp struct {
			TotalDaemons     int `json:"total_daemons"`
			ActiveContainers int `json:"active_containers"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		require.Equal(t, 2, resp.TotalDaemons)
		require.Equal(t, 12, resp.ActiveContainers)
	})

	t.Run("authenticated with server token header", func(t *testing.T) {
		t.Setenv("KIWI_SERVER_TOKEN", "valid-server-token")
		req := httptest.NewRequest(http.MethodGet, "/admin/metrics/fleet", nil)
		req.Header.Set("Authorization", "Bearer valid-server-token")
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)

		require.Equal(t, http.StatusOK, w.Code)
		var resp struct {
			TotalDaemons     int `json:"total_daemons"`
			ActiveContainers int `json:"active_containers"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		require.Equal(t, 2, resp.TotalDaemons)
		require.Equal(t, 12, resp.ActiveContainers)
	})

	t.Run("method not allowed", func(t *testing.T) {
		t.Setenv("KIWI_SERVER_TOKEN", "valid-server-token")
		req := httptest.NewRequest(http.MethodPost, "/admin/metrics/fleet", nil)
		req.Header.Set("Authorization", "Bearer valid-server-token")
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		require.Equal(t, http.StatusMethodNotAllowed, w.Code)
	})
}
