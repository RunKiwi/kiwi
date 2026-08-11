package daemon

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ibreakthecloud/kiwi/pkg/session"
)

// A continuation is a new task carrying a session that already exists, so the
// id the daemon would derive from its own task id is not the session's id.
//
// The Control Plane resolves sessions by task and answers with the real one.
// Ignoring that answer and saving under the derived id inserts a second row —
// which collides with the unique index on task_id, so the checkpoint is lost
// and the round's history with it.
func TestSessionStoreAdoptsTheResolvedSessionID(t *testing.T) {
	var savedID string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req SessionCheckpointReq
		_ = json.NewDecoder(r.Body).Decode(&req)

		switch r.URL.Path {
		case "/api/v1/daemon/session/load":
			// The thread's session, named after the task that started it.
			_ = json.NewEncoder(w).Encode(SessionStateRes{
				Found:     true,
				SessionID: "sess_task_root",
				Round:     3,
				Status:    "RUNNING",
				State:     json.RawMessage(`{"round":3,"base_sha":"base","head_sha":"head"}`),
			})
		case "/api/v1/daemon/session":
			savedID = req.SessionID
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	store := &cpSessionStore{
		client:  NewClient(srv.URL),
		taskID:  "task_continuation",
		leaseID: "lease_1",
	}

	cp, err := store.Load(context.Background(), sessionIDFor("task_continuation"))
	if err != nil {
		t.Fatal(err)
	}
	if cp == nil {
		t.Fatal("expected a checkpoint to resume from")
	}
	if cp.Round != 3 {
		t.Errorf("round = %d, want 3", cp.Round)
	}

	// The runner passes its own derived id; the store must ignore it in favour
	// of the one the Control Plane resolved.
	if err := store.Save(context.Background(), sessionIDFor("task_continuation"), *cp, nil); err != nil {
		t.Fatal(err)
	}
	if savedID != "sess_task_root" {
		t.Errorf("saved under %q, want sess_task_root — a second row would collide and lose the round", savedID)
	}
}

// A task's first lease has no session, so nothing is adopted and the derived
// id stands.
func TestSessionStoreKeepsTheDerivedIDWhenThereIsNoSession(t *testing.T) {
	var savedID string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/daemon/session/load":
			w.WriteHeader(http.StatusNoContent)
		case "/api/v1/daemon/session":
			var req SessionCheckpointReq
			_ = json.NewDecoder(r.Body).Decode(&req)
			savedID = req.SessionID
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	defer srv.Close()

	store := &cpSessionStore{client: NewClient(srv.URL), taskID: "task_fresh", leaseID: "lease_1"}

	cp, err := store.Load(context.Background(), sessionIDFor("task_fresh"))
	if err != nil {
		t.Fatal(err)
	}
	if cp != nil {
		t.Fatalf("got a checkpoint %+v, want none for a first lease", cp)
	}

	if err := store.Save(context.Background(), sessionIDFor("task_fresh"), session.Checkpoint{}, nil); err != nil {
		t.Fatal(err)
	}
	if savedID != "sess_task_fresh" {
		t.Errorf("saved under %q, want the derived sess_task_fresh", savedID)
	}
}

// A concluded session is not resumed by an ordinary re-lease. Adoption must
// not quietly undo that: the id is only taken over when there is something to
// resume.
func TestSessionStoreDoesNotAdoptAConcludedSession(t *testing.T) {
	var savedID string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/daemon/session/load":
			_ = json.NewEncoder(w).Encode(SessionStateRes{
				Found: true, SessionID: "sess_old", Round: 2, Status: "SUCCEEDED",
				State: json.RawMessage(`{"round":2}`),
			})
		case "/api/v1/daemon/session":
			var req SessionCheckpointReq
			_ = json.NewDecoder(r.Body).Decode(&req)
			savedID = req.SessionID
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	defer srv.Close()

	store := &cpSessionStore{client: NewClient(srv.URL), taskID: "task_retry", leaseID: "lease_1"}

	cp, err := store.Load(context.Background(), sessionIDFor("task_retry"))
	if err != nil {
		t.Fatal(err)
	}
	if cp != nil {
		t.Fatal("a concluded session must not be resumed")
	}

	if err := store.Save(context.Background(), sessionIDFor("task_retry"), session.Checkpoint{}, nil); err != nil {
		t.Fatal(err)
	}
	if savedID != "sess_task_retry" {
		t.Errorf("saved under %q, want the derived id — nothing was resumed", savedID)
	}
}
