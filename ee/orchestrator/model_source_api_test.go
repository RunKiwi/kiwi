// SPDX-License-Identifier: LicenseRef-Kiwi-BSL-1.1
// Copyright (c) 2026 RunKiwi. Licensed under the Business Source License 1.1.
// See ee/LICENSE. This is Control Plane code and is NOT Apache-2.0.

package orchestrator

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ibreakthecloud/kiwi/ee/auth"
)

func TestHandleModelSourceGetDefaultsToKiwiWithNoOrgRow(t *testing.T) {
	s := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/org/model-source", nil)
	req = req.WithContext(auth.ContextWithClaims(req.Context(), &auth.UserClaims{OrgID: "org_1", UserID: "user_1"}))
	w := httptest.NewRecorder()

	s.handleModelSource(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	var out map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out["default_model_source"] != "kiwi" {
		t.Errorf("default_model_source = %q, want the default \"kiwi\"", out["default_model_source"])
	}
}

func TestHandleModelSourcePatchPersistsAndRoundTrips(t *testing.T) {
	s := newTestServer(t)
	if err := s.db.Create(&auth.Organization{ID: "org_1", Plan: "pro"}).Error; err != nil {
		t.Fatal(err)
	}

	body, _ := json.Marshal(map[string]string{"default_model_source": "byok"})
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/org/model-source", bytes.NewReader(body))
	req = req.WithContext(auth.ContextWithClaims(req.Context(), &auth.UserClaims{OrgID: "org_1", UserID: "user_1"}))
	w := httptest.NewRecorder()

	s.handleModelSource(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	got, err := s.storage.ModelSource(req.Context(), "org_1")
	if err != nil || got != "byok" {
		t.Errorf("ModelSource = %q, err=%v; want \"byok\"", got, err)
	}
}

func TestHandleModelSourcePatchRejectsAnUnknownValue(t *testing.T) {
	s := newTestServer(t)
	if err := s.db.Create(&auth.Organization{ID: "org_1", Plan: "pro"}).Error; err != nil {
		t.Fatal(err)
	}

	body, _ := json.Marshal(map[string]string{"default_model_source": "anthropic-only-please"})
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/org/model-source", bytes.NewReader(body))
	req = req.WithContext(auth.ContextWithClaims(req.Context(), &auth.UserClaims{OrgID: "org_1", UserID: "user_1"}))
	w := httptest.NewRecorder()

	s.handleModelSource(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status %d, want 400: %s", w.Code, w.Body.String())
	}
}
