package provisioner_test

import (
	"context"
	"testing"
	"time"

	"github.com/ibreakthecloud/kiwi/pkg/auth"
	"github.com/ibreakthecloud/kiwi/pkg/provisioner"
	"github.com/ibreakthecloud/kiwi/pkg/store"
	"gorm.io/gorm"
)

func setupSweepDB(t *testing.T) (*provisioner.Provisioner, *provisioner.StubLauncher, *gorm.DB) {
	t.Helper()
	db, err := auth.OpenDB("file:" + t.Name() + "?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&store.Daemon{}, &store.QueuedTask{}, &store.DaemonJoinToken{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if err := provisioner.EnsureSchema(db); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}
	launcher := provisioner.NewStubLauncher()
	prov := provisioner.NewProvisioner(db, store.NewPostgresStore(db), launcher, "http://localhost:8080")
	return prov, launcher, db
}

func createFreeDaemon(t *testing.T, db *gorm.DB, id, orgID string, createdAt time.Time) {
	t.Helper()
	d := store.Daemon{ID: id, OrgID: orgID, FleetID: auth.SharedFreeFleet, SignPubKey: "sign-" + id, EncPubKey: "enc-" + id, CreatedAt: createdAt}
	if err := db.Create(&d).Error; err != nil {
		t.Fatalf("create daemon: %v", err)
	}
}

func createTask(t *testing.T, db *gorm.DB, id, orgID, status string, updatedAt time.Time) {
	t.Helper()
	tk := store.QueuedTask{ID: id, OrgID: orgID, Status: status, Spec: map[string]interface{}{"task": "x"}}
	if err := db.Create(&tk).Error; err != nil {
		t.Fatalf("create task: %v", err)
	}
	if err := db.Model(&store.QueuedTask{}).Where("id = ?", id).UpdateColumn("updated_at", updatedAt).Error; err != nil {
		t.Fatalf("backdate task: %v", err)
	}
}

func TestReclaimStaleInProgress(t *testing.T) {
	prov, _, db := setupSweepDB(t)
	ctx := context.Background()

	// A stale in_progress request (crashed mid-claim) and a fresh one.
	db.Create(&auth.ProvisioningRequest{ID: "stale", OrgID: "o1", Type: "provision", Status: "in_progress"})
	db.Model(&auth.ProvisioningRequest{}).Where("id = ?", "stale").UpdateColumn("updated_at", time.Now().Add(-10*time.Minute))
	db.Create(&auth.ProvisioningRequest{ID: "fresh", OrgID: "o2", Type: "provision", Status: "in_progress"})

	n, err := prov.ReclaimStaleInProgress(ctx, 5*time.Minute)
	if err != nil {
		t.Fatalf("ReclaimStaleInProgress: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected 1 requeued, got %d", n)
	}

	var stale, fresh auth.ProvisioningRequest
	db.First(&stale, "id = ?", "stale")
	db.First(&fresh, "id = ?", "fresh")
	if stale.Status != "pending" {
		t.Errorf("stale should be requeued to pending, got %s", stale.Status)
	}
	if fresh.Status != "in_progress" {
		t.Errorf("fresh should stay in_progress, got %s", fresh.Status)
	}
}

func TestReclaimIdle_EnqueuesForIdleOrg(t *testing.T) {
	prov, _, db := setupSweepDB(t)
	ctx := context.Background()

	// Free daemon whose org has never run a task and predates the idle window.
	createFreeDaemon(t, db, "d1", "o1", time.Now().Add(-1*time.Hour))

	n, err := prov.ReclaimIdle(ctx, 15*time.Minute)
	if err != nil {
		t.Fatalf("ReclaimIdle: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected 1 reclaim enqueued, got %d", n)
	}

	var reclaims int64
	db.Model(&auth.ProvisioningRequest{}).
		Where("org_id = ? AND type = 'reclaim' AND status = 'pending'", "o1").Count(&reclaims)
	if reclaims != 1 {
		t.Errorf("expected 1 pending reclaim for o1, got %d", reclaims)
	}
}

func TestReclaimIdle_SkipsActiveOrg(t *testing.T) {
	prov, _, db := setupSweepDB(t)
	ctx := context.Background()

	createFreeDaemon(t, db, "d1", "o1", time.Now().Add(-1*time.Hour))
	createTask(t, db, "t1", "o1", store.TaskQueued, time.Now()) // active work

	n, err := prov.ReclaimIdle(ctx, 15*time.Minute)
	if err != nil {
		t.Fatalf("ReclaimIdle: %v", err)
	}
	if n != 0 {
		t.Errorf("an org with active tasks must not be reclaimed, got %d", n)
	}
}

func TestReclaimIdle_SkipsRecentlyActiveOrg(t *testing.T) {
	prov, _, db := setupSweepDB(t)
	ctx := context.Background()

	createFreeDaemon(t, db, "d1", "o1", time.Now().Add(-1*time.Hour))
	createTask(t, db, "t1", "o1", store.TaskSucceeded, time.Now()) // finished just now

	n, err := prov.ReclaimIdle(ctx, 15*time.Minute)
	if err != nil {
		t.Fatalf("ReclaimIdle: %v", err)
	}
	if n != 0 {
		t.Errorf("an org active within the window must not be reclaimed, got %d", n)
	}
}

func TestReclaimIdle_DedupsOpenReclaim(t *testing.T) {
	prov, _, db := setupSweepDB(t)
	ctx := context.Background()

	createFreeDaemon(t, db, "d1", "o1", time.Now().Add(-1*time.Hour))
	db.Create(&auth.ProvisioningRequest{ID: "r1", OrgID: "o1", Type: "reclaim", Status: "pending"})

	n, err := prov.ReclaimIdle(ctx, 15*time.Minute)
	if err != nil {
		t.Fatalf("ReclaimIdle: %v", err)
	}
	if n != 0 {
		t.Errorf("must not enqueue a second reclaim when one is open, got %d", n)
	}
}

func TestReclaim_DeregistersDaemon(t *testing.T) {
	prov, launcher, db := setupSweepDB(t)
	ctx := context.Background()

	createFreeDaemon(t, db, "d1", "o1", time.Now())
	db.Create(&auth.ProvisioningRequest{ID: "r1", OrgID: "o1", Type: "reclaim", Status: "pending"})

	processed, err := prov.PollOnce(ctx)
	if err != nil || !processed {
		t.Fatalf("PollOnce: processed=%v err=%v", processed, err)
	}

	if len(launcher.StopCalls) != 1 || launcher.StopCalls[0] != "o1" {
		t.Fatalf("expected Stop for o1, got %v", launcher.StopCalls)
	}
	var daemons int64
	db.Model(&store.Daemon{}).Where("org_id = ?", "o1").Count(&daemons)
	if daemons != 0 {
		t.Errorf("reclaim should deregister the org's free daemon, %d left", daemons)
	}
}
