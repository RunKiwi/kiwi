package orchestrator

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"github.com/ibreakthecloud/kiwi/pkg/auth"
	"github.com/ibreakthecloud/kiwi/pkg/store"
)

func newDashTestServer(t *testing.T) *Server {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&store.Fleet{}, &store.ModelEntry{}, &store.Credential{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return &Server{db: db, storage: store.NewPostgresStore(db)}
}

func authed(method, path, body, org string) *http.Request {
	var r *http.Request
	if body != "" {
		r = httptest.NewRequest(method, path, strings.NewReader(body))
	} else {
		r = httptest.NewRequest(method, path, nil)
	}
	return r.WithContext(auth.ContextWithClaims(r.Context(), &auth.UserClaims{OrgID: org}))
}

func TestHandleFleets(t *testing.T) {
	srv := newDashTestServer(t)

	// Unauthenticated -> 401.
	rr := httptest.NewRecorder()
	srv.handleFleets(rr, httptest.NewRequest(http.MethodGet, "/api/v1/fleets", nil))
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("no claims should be 401, got %d", rr.Code)
	}

	// Create for org-1.
	rr = httptest.NewRecorder()
	srv.handleFleets(rr, authed(http.MethodPost, "/api/v1/fleets", `{"name":"prod","type":"byoc"}`, "org-1"))
	if rr.Code != http.StatusCreated {
		t.Fatalf("create: got %d body %s", rr.Code, rr.Body.String())
	}

	// A different org must not see it.
	rr = httptest.NewRecorder()
	srv.handleFleets(rr, authed(http.MethodGet, "/api/v1/fleets", "", "org-2"))
	var other struct {
		Fleets []store.Fleet `json:"fleets"`
	}
	_ = json.NewDecoder(rr.Body).Decode(&other)
	if len(other.Fleets) != 0 {
		t.Errorf("org-2 must not see org-1's fleet, got %d", len(other.Fleets))
	}

	// Owner sees exactly one, of type byoc.
	rr = httptest.NewRecorder()
	srv.handleFleets(rr, authed(http.MethodGet, "/api/v1/fleets", "", "org-1"))
	var mine struct {
		Fleets []store.Fleet `json:"fleets"`
	}
	_ = json.NewDecoder(rr.Body).Decode(&mine)
	if len(mine.Fleets) != 1 || mine.Fleets[0].Type != store.FleetBYOC {
		t.Errorf("owner should see one byoc fleet, got %+v", mine.Fleets)
	}
}

func TestHandleModelsProviderInferred(t *testing.T) {
	srv := newDashTestServer(t)
	rr := httptest.NewRecorder()
	srv.handleModels(rr, authed(http.MethodPost, "/api/v1/models", `{"name":"gemini-2.5-flash"}`, "org-1"))
	if rr.Code != http.StatusCreated {
		t.Fatalf("create model: got %d body %s", rr.Code, rr.Body.String())
	}
	var m store.ModelEntry
	_ = json.NewDecoder(rr.Body).Decode(&m)
	if m.Provider != "gemini" {
		t.Errorf("provider should be inferred as gemini, got %q", m.Provider)
	}
}

func TestHandleIntegrationsReflectsCredentials(t *testing.T) {
	srv := newDashTestServer(t)
	// No creds -> nothing connected.
	rr := httptest.NewRecorder()
	srv.handleIntegrations(rr, authed(http.MethodGet, "/api/v1/integrations", "", "org-1"))
	var before struct {
		Integrations []struct {
			Key       string `json:"key"`
			Connected bool   `json:"connected"`
		} `json:"integrations"`
	}
	_ = json.NewDecoder(rr.Body).Decode(&before)
	for _, i := range before.Integrations {
		if i.Connected {
			t.Errorf("nothing should be connected yet, but %s is", i.Key)
		}
	}

	// Save a GitHub token -> github reports connected.
	if err := srv.storage.SaveCredential(authed(http.MethodGet, "/", "", "org-1").Context(), "org-1", "GITHUB_TOKEN", "github", "ghp_x"); err != nil {
		t.Fatalf("save cred: %v", err)
	}
	rr = httptest.NewRecorder()
	srv.handleIntegrations(rr, authed(http.MethodGet, "/api/v1/integrations", "", "org-1"))
	var after struct {
		Integrations []struct {
			Key       string `json:"key"`
			Connected bool   `json:"connected"`
		} `json:"integrations"`
	}
	_ = json.NewDecoder(rr.Body).Decode(&after)
	var githubConnected bool
	for _, i := range after.Integrations {
		if i.Key == "github" {
			githubConnected = i.Connected
		}
	}
	if !githubConnected {
		t.Error("github should report connected after saving GITHUB_TOKEN")
	}
}
