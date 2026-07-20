package provisioner_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ibreakthecloud/kiwi/pkg/auth"
	"github.com/ibreakthecloud/kiwi/pkg/provisioner"
	"github.com/ibreakthecloud/kiwi/pkg/store"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func setupTestDB(t *testing.T) (*auth.Organization, *provisioner.Provisioner, *provisioner.StubLauncher, *gorm.DB) {
	t.Helper()

	// A distinct in-memory DB per test avoids cross-test interference on the
	// shared cache while still exercising real SQL (incl. the partial index).
	db, err := auth.OpenDB("file:" + t.Name() + "?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("failed to open in-memory db: %v", err)
	}
	if err := db.AutoMigrate(&store.DaemonJoinToken{}); err != nil {
		t.Fatalf("failed to auto-migrate store.DaemonJoinToken: %v", err)
	}

	s := store.NewPostgresStore(db)
	if err := provisioner.EnsureSchema(db); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}

	org := &auth.Organization{
		ID:              "org-" + t.Name(),
		Name:            "Test Org " + t.Name(),
		Plan:            "free",
		ActivationState: "active",
		CreatedAt:       time.Now(),
	}
	if err := db.Create(org).Error; err != nil {
		t.Fatalf("failed to create org: %v", err)
	}

	launcher := provisioner.NewStubLauncher()
	prov := provisioner.NewProvisioner(db, s, launcher, "http://localhost:8080")
	return org, prov, launcher, db
}

func mustCreateRequest(t *testing.T, db *gorm.DB, id, orgID, typ string) {
	t.Helper()
	req := auth.ProvisioningRequest{ID: id, OrgID: orgID, Type: typ, Status: "pending", CreatedAt: time.Now()}
	if err := db.Create(&req).Error; err != nil {
		t.Fatalf("create request: %v", err)
	}
}

func statusOf(t *testing.T, db *gorm.DB, id string) string {
	t.Helper()
	var r auth.ProvisioningRequest
	if err := db.First(&r, "id = ?", id).Error; err != nil {
		t.Fatalf("load request %s: %v", id, err)
	}
	return r.Status
}

func TestPoller_Provision(t *testing.T) {
	org, prov, launcher, db := setupTestDB(t)
	mustCreateRequest(t, db, "prov_1", org.ID, "provision")

	processed, err := prov.PollOnce(context.Background())
	if err != nil {
		t.Fatalf("PollOnce error: %v", err)
	}
	if !processed {
		t.Fatal("expected PollOnce to process a request")
	}

	if len(launcher.LaunchCalls) != 1 {
		t.Fatalf("expected 1 launch call, got %d", len(launcher.LaunchCalls))
	}
	call := launcher.LaunchCalls[0]
	if call.OrgID != org.ID {
		t.Errorf("expected org %s, got %s", org.ID, call.OrgID)
	}
	if call.FleetID != "shared-free" {
		t.Errorf("expected fleet shared-free, got %s", call.FleetID)
	}
	if call.JoinToken == "" {
		t.Error("expected a non-empty join token")
	}
	if s := statusOf(t, db, "prov_1"); s != "completed" {
		t.Errorf("expected status completed, got %s", s)
	}
}

func TestPoller_Reclaim(t *testing.T) {
	org, prov, launcher, db := setupTestDB(t)
	mustCreateRequest(t, db, "prov_2", org.ID, "reclaim")

	processed, err := prov.PollOnce(context.Background())
	if err != nil {
		t.Fatalf("PollOnce error: %v", err)
	}
	if !processed {
		t.Fatal("expected PollOnce to process a request")
	}
	if len(launcher.StopCalls) != 1 || launcher.StopCalls[0] != org.ID {
		t.Fatalf("expected 1 stop call for %s, got %v", org.ID, launcher.StopCalls)
	}
	if s := statusOf(t, db, "prov_2"); s != "completed" {
		t.Errorf("expected status completed, got %s", s)
	}
}

// TestPoller_FailedPersists is the regression test for the P1 bug: a launch
// failure must leave the request terminally FAILED (not rolled back to pending),
// and it must not be retried on the next poll.
func TestPoller_FailedPersists(t *testing.T) {
	org, prov, launcher, db := setupTestDB(t)
	launcher.LaunchErr = errors.New("boom: docker unavailable")
	mustCreateRequest(t, db, "prov_fail", org.ID, "provision")

	processed, err := prov.PollOnce(context.Background())
	if err != nil {
		t.Fatalf("PollOnce should not surface the side-effect error: %v", err)
	}
	if !processed {
		t.Fatal("expected the request to be claimed and processed")
	}
	if s := statusOf(t, db, "prov_fail"); s != "failed" {
		t.Fatalf("expected status failed to PERSIST, got %s (the rollback bug)", s)
	}

	// It must not be retried: a second poll finds no pending work and does not
	// launch again (the old bug re-launched every tick, leaking a token each time).
	processed2, err := prov.PollOnce(context.Background())
	if err != nil {
		t.Fatalf("second PollOnce error: %v", err)
	}
	if processed2 {
		t.Error("failed request must not be reprocessed")
	}
	if len(launcher.LaunchCalls) != 0 {
		t.Errorf("a failed launch records no successful call and never retries; got %d", len(launcher.LaunchCalls))
	}
}

// TestPoller_SkipsNonPending proves the claim guard: a request already in flight
// (not pending) is never claimed.
func TestPoller_SkipsNonPending(t *testing.T) {
	org, prov, launcher, db := setupTestDB(t)
	req := auth.ProvisioningRequest{ID: "prov_inflight", OrgID: org.ID, Type: "provision", Status: "in_progress", CreatedAt: time.Now()}
	if err := db.Create(&req).Error; err != nil {
		t.Fatalf("create: %v", err)
	}

	processed, err := prov.PollOnce(context.Background())
	if err != nil {
		t.Fatalf("PollOnce error: %v", err)
	}
	if processed {
		t.Error("must not claim a non-pending request")
	}
	if len(launcher.LaunchCalls) != 0 {
		t.Errorf("expected no launches, got %d", len(launcher.LaunchCalls))
	}
}

// TestPoller_ConcurrentSingleRow proves no double-claim: many pollers racing on
// one pending row result in exactly one launch. Transient SQLite lock errors are
// retried; the RowsAffected guard is what guarantees single-claim.
func TestPoller_ConcurrentSingleRow(t *testing.T) {
	org, prov, launcher, db := setupTestDB(t)
	mustCreateRequest(t, db, "prov_race", org.ID, "provision")

	const workers = 5
	var claimed int32
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for attempt := 0; attempt < 50; attempt++ {
				processed, err := prov.PollOnce(context.Background())
				if err != nil {
					continue // transient lock; retry
				}
				if processed {
					atomic.AddInt32(&claimed, 1)
				}
				return // processed it, or nothing left to do
			}
		}()
	}
	wg.Wait()

	if claimed != 1 {
		t.Errorf("expected exactly 1 poller to claim the row, got %d", claimed)
	}
	if len(launcher.LaunchCalls) != 1 {
		t.Errorf("expected exactly 1 launch, got %d", len(launcher.LaunchCalls))
	}
	if s := statusOf(t, db, "prov_race"); s != "completed" {
		t.Errorf("expected completed, got %s", s)
	}
}

// TestColdStart_DedupIndex proves the partial unique index backing free-tier
// cold-start: at most one PENDING provision per org, so a racing insert is a
// no-op under ON CONFLICT DO NOTHING (what handleTasks uses).
func TestColdStart_DedupIndex(t *testing.T) {
	org, _, _, db := setupTestDB(t)

	insert := func(id string) error {
		req := auth.ProvisioningRequest{ID: id, OrgID: org.ID, Type: "provision", Status: "pending", CreatedAt: time.Now()}
		return db.Clauses(clause.OnConflict{DoNothing: true}).Create(&req).Error
	}
	if err := insert("prov_a"); err != nil {
		t.Fatalf("first insert should succeed: %v", err)
	}
	if err := insert("prov_b"); err != nil {
		t.Fatalf("second insert should be a silent no-op, not an error: %v", err)
	}

	var count int64
	db.Model(&auth.ProvisioningRequest{}).
		Where("org_id = ? AND type = 'provision' AND status = 'pending'", org.ID).
		Count(&count)
	if count != 1 {
		t.Errorf("expected exactly 1 pending provision for the org, got %d", count)
	}
}
