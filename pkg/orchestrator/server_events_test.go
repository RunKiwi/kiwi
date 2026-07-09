package orchestrator

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ibreakthecloud/kiwi/pkg/auth"
)

func TestGetTaskEvents(t *testing.T) {
	db := newTestDB(t)
	s := &Server{db: db}

	// A task owned by test-org, with two events.
	db.Create(&TaskState{ID: "tk1", Status: "SUCCESS", OrgID: "test-org", UserID: "test-user"})
	db.Create(&TaskEvent{TaskID: "tk1", OrgID: "test-org", Step: 0, Phase: "initial_test", Outcome: "fail"})
	db.Create(&TaskEvent{TaskID: "tk1", OrgID: "test-org", Step: 1, Phase: "actor", Outcome: "proposed"})

	// Owner (same org) sees the events.
	req := httptest.NewRequest(http.MethodGet, "/tasks/tk1/events", nil).WithContext(testClaims())
	rw := httptest.NewRecorder()
	s.handleTaskStatus(rw, req)
	if rw.Code != http.StatusOK {
		t.Fatalf("owner status %d: %s", rw.Code, rw.Body.String())
	}
	var got []TaskEvent
	_ = json.Unmarshal(rw.Body.Bytes(), &got)
	if len(got) != 2 || got[0].Phase != "initial_test" || got[1].Phase != "actor" {
		t.Fatalf("events wrong: %+v", got)
	}

	// A different org is forbidden.
	otherClaims := auth.ContextWithClaims(context.Background(), &auth.UserClaims{
		UserID: "u2", OrgID: "other-org", Role: "member",
	})
	req2 := httptest.NewRequest(http.MethodGet, "/tasks/tk1/events", nil).WithContext(otherClaims)
	rw2 := httptest.NewRecorder()
	s.handleTaskStatus(rw2, req2)
	if rw2.Code != http.StatusForbidden {
		t.Errorf("cross-org events access: got %d want 403", rw2.Code)
	}
}
