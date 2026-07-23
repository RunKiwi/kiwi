package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestAdminJoinRequests(t *testing.T) {
	db := setupTestDB(t)

	// Create org and some join requests
	org := Organization{ID: "org_1", Name: "Acme", Type: "team", DomainJoin: false}
	db.Create(&org)

	req1 := OrgJoinRequest{ID: "req_1", OrgID: "org_1", UserEmail: "alice@acme.com", Status: "pending", CreatedAt: time.Now()}
	req2 := OrgJoinRequest{ID: "req_2", OrgID: "org_1", UserEmail: "bob@acme.com", Status: "pending", CreatedAt: time.Now()}
	db.Create(&req1)
	db.Create(&req2)

	// Create alice in a personal org (waiting for approval)
	aliceOrg := Organization{ID: "org_alice", Name: "Alice Personal", Type: "personal"}
	db.Create(&aliceOrg)
	alice := User{ID: "usr_alice", Email: "alice@acme.com", OrgID: "org_alice", Role: "admin"}
	db.Create(&alice)

	mux := http.NewServeMux()
	AdminRouter(db, mux)

	t.Run("list join requests", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/admin/orgs/org_1/join_requests", nil)
		req = req.WithContext(context.WithValue(req.Context(), claimsKey, &UserClaims{UserID: "system"}))
		rr := httptest.NewRecorder()

		mux.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d: %s", rr.Code, rr.Body.String())
		}

		var reqs []OrgJoinRequest
		if err := json.Unmarshal(rr.Body.Bytes(), &reqs); err != nil {
			t.Fatal(err)
		}
		if len(reqs) != 2 {
			t.Errorf("expected 2 requests, got %d", len(reqs))
		}
	})

	t.Run("approve join request", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/admin/orgs/org_1/join_requests/req_1/approve", nil)
		req = req.WithContext(context.WithValue(req.Context(), claimsKey, &UserClaims{UserID: "system"}))
		rr := httptest.NewRecorder()

		mux.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d: %s", rr.Code, rr.Body.String())
		}

		var updatedReq OrgJoinRequest
		db.First(&updatedReq, "id = ?", "req_1")
		if updatedReq.Status != "approved" {
			t.Errorf("expected status 'approved', got %s", updatedReq.Status)
		}

		var updatedUser User
		db.First(&updatedUser, "id = ?", "usr_alice")
		if updatedUser.OrgID != "org_1" || updatedUser.Role != "member" {
			t.Errorf("expected user org_1/member, got %s/%s", updatedUser.OrgID, updatedUser.Role)
		}
	})

	t.Run("deny join request", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/admin/orgs/org_1/join_requests/req_2/deny", nil)
		req = req.WithContext(context.WithValue(req.Context(), claimsKey, &UserClaims{UserID: "system"}))
		rr := httptest.NewRecorder()

		mux.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d: %s", rr.Code, rr.Body.String())
		}

		var updatedReq OrgJoinRequest
		db.First(&updatedReq, "id = ?", "req_2")
		if updatedReq.Status != "denied" {
			t.Errorf("expected status 'denied', got %s", updatedReq.Status)
		}
	})

	t.Run("toggle domain join", func(t *testing.T) {
		body := []byte(`{"domain_join": true}`)
		req := httptest.NewRequest(http.MethodPut, "/admin/orgs/org_1/domain_join", bytes.NewBuffer(body))
		req = req.WithContext(context.WithValue(req.Context(), claimsKey, &UserClaims{UserID: "system"}))
		rr := httptest.NewRecorder()

		mux.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d: %s", rr.Code, rr.Body.String())
		}

		var updatedOrg Organization
		db.First(&updatedOrg, "id = ?", "org_1")
		if !updatedOrg.DomainJoin {
			t.Errorf("expected domain_join to be true")
		}
	})
}
