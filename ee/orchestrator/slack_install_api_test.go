// ee/orchestrator/slack_install_api_test.go
package orchestrator

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/ibreakthecloud/kiwi/ee/auth"
)

func containsAll(s string, substrs ...string) bool {
	for _, sub := range substrs {
		if !strings.Contains(s, sub) {
			return false
		}
	}
	return true
}

func TestHandleSlackInstallReturnsJSONURLWhenAcceptHeaderAsks(t *testing.T) {
	os.Setenv("KIWI_SLACK_CLIENT_ID", "client-123")
	defer os.Unsetenv("KIWI_SLACK_CLIENT_ID")

	s := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/integrations/slack/install", nil)
	req.Header.Set("Accept", "application/json")
	req = req.WithContext(auth.ContextWithClaims(req.Context(), &auth.UserClaims{OrgID: "org_1", UserID: "user_1"}))
	w := httptest.NewRecorder()

	s.handleSlackInstall(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	if !containsAll(w.Body.String(), "slack.com/oauth/v2/authorize", "client-123", "state=") {
		t.Fatalf("unexpected body: %s", w.Body.String())
	}
}

func TestHandleSlackInstallRejectsUnauthenticated(t *testing.T) {
	os.Setenv("KIWI_SLACK_CLIENT_ID", "client-123")
	defer os.Unsetenv("KIWI_SLACK_CLIENT_ID")

	s := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/integrations/slack/install", nil)
	w := httptest.NewRecorder()
	s.handleSlackInstall(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}
