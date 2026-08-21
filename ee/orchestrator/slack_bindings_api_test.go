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
	"github.com/ibreakthecloud/kiwi/pkg/store"
)

func TestHandleCreateSlackBindingPersistsAndReturns201(t *testing.T) {
	s := newTestServer(t)
	_ = s.storage.UpsertSlackInstallation(t.Context(), &store.SlackInstallation{TeamID: "T1", OrgID: "org_1"})

	body, _ := json.Marshal(map[string]string{
		"team_id": "T1", "channel_id": "C1", "repo_url": "https://github.com/acme/widget",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/integrations/slack/bindings", bytes.NewReader(body))
	req = req.WithContext(auth.ContextWithClaims(req.Context(), &auth.UserClaims{OrgID: "org_1", UserID: "user_1"}))
	w := httptest.NewRecorder()

	s.handleSlackBindings(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}

	list, err := s.storage.ListSlackChannelBindings(req.Context(), "org_1")
	if err != nil || len(list) != 1 || list[0].ChannelID != "C1" {
		t.Fatalf("list=%v err=%v", list, err)
	}
}

// The workspace being bound must actually belong to the requesting org —
// otherwise any authenticated user on any org could bind a channel on a
// workspace connected to a DIFFERENT org, redirecting that workspace's
// future triggers at a repo of the attacker's choosing.
func TestHandleCreateSlackBindingRejectsAWorkspaceOwnedByAnotherOrg(t *testing.T) {
	s := newTestServer(t)
	_ = s.storage.UpsertSlackInstallation(t.Context(), &store.SlackInstallation{TeamID: "T-other-org", OrgID: "org_other"})

	body, _ := json.Marshal(map[string]string{
		"team_id": "T-other-org", "channel_id": "C1", "repo_url": "https://github.com/attacker/repo",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/integrations/slack/bindings", bytes.NewReader(body))
	req = req.WithContext(auth.ContextWithClaims(req.Context(), &auth.UserClaims{OrgID: "org_1", UserID: "user_1"}))
	w := httptest.NewRecorder()

	s.handleSlackBindings(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for a workspace owned by another org, got %d: %s", w.Code, w.Body.String())
	}
	list, err := s.storage.ListSlackChannelBindings(req.Context(), "org_1")
	if err != nil || len(list) != 0 {
		t.Fatalf("expected no binding created, got %v err=%v", list, err)
	}
}

// A team_id with no connected installation at all must be rejected the same
// way — there's nothing to check ownership against, so it can't be trusted.
func TestHandleCreateSlackBindingRejectsAnUninstalledTeamID(t *testing.T) {
	s := newTestServer(t)
	body, _ := json.Marshal(map[string]string{
		"team_id": "T-never-installed", "channel_id": "C1", "repo_url": "https://github.com/acme/widget",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/integrations/slack/bindings", bytes.NewReader(body))
	req = req.WithContext(auth.ContextWithClaims(req.Context(), &auth.UserClaims{OrgID: "org_1", UserID: "user_1"}))
	w := httptest.NewRecorder()

	s.handleSlackBindings(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for an uninstalled team_id, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleListSlackBindingsIsOrgScoped(t *testing.T) {
	s := newTestServer(t)
	_ = s.storage.CreateSlackChannelBinding(t.Context(), &store.SlackChannelBinding{OrgID: "org_other", TeamID: "T2", ChannelID: "C2", RepoURL: "https://github.com/acme/other"})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/integrations/slack/bindings", nil)
	req = req.WithContext(auth.ContextWithClaims(req.Context(), &auth.UserClaims{OrgID: "org_1", UserID: "user_1"}))
	w := httptest.NewRecorder()

	s.handleSlackBindings(w, req)

	var out struct {
		Bindings []map[string]any `json:"bindings"`
	}
	json.Unmarshal(w.Body.Bytes(), &out)
	if len(out.Bindings) != 0 {
		t.Fatalf("expected no bindings visible to org_1, got %v", out.Bindings)
	}
}
