// ee/orchestrator/slack_bindings_api_test.go
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
